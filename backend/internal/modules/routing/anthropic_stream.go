package routing

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// chatStream turns a Claude SSE stream into chat completion chunks, one event at a time.
type chatStream struct {
	src          io.ReadCloser
	br           *bufio.Reader
	created      int64
	includeUsage bool

	pending []byte
	done    bool
	id      string
	model   string
	usage   claudeUsage
}

func newChatStream(src io.ReadCloser, created int64, includeUsage bool) *chatStream {
	return &chatStream{src: src, br: bufio.NewReader(src), created: created, includeUsage: includeUsage}
}

func (s *chatStream) Close() error { return s.src.Close() }

func (s *chatStream) Read(p []byte) (int, error) {
	for len(s.pending) == 0 {
		if s.done {
			return 0, io.EOF
		}
		data, err := s.readEvent()
		if err != nil {
			return 0, err
		}
		s.handle(data)
	}
	n := copy(p, s.pending)
	s.pending = s.pending[n:]
	return n, nil
}

// readEvent returns the data of the next SSE event; a stream cut before message_stop is an unexpected EOF.
func (s *chatStream) readEvent() ([]byte, error) {
	var data []byte
	for {
		line, err := s.br.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		if err != nil {
			return nil, err
		}
		line = bytes.TrimRight(line, "\r\n")
		if len(line) == 0 && len(data) > 0 {
			return data, nil
		}
		if value, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			if len(data) > 0 {
				data = append(data, '\n')
			}
			data = append(data, bytes.TrimPrefix(value, []byte(" "))...)
		}
	}
}

func (s *chatStream) handle(data []byte) {
	var ev struct {
		Type    string `json:"type"`
		Message struct {
			ID    string      `json:"id"`
			Model string      `json:"model"`
			Usage claudeUsage `json:"usage"`
		} `json:"message"`
		Delta struct {
			Type       string `json:"type"`
			Text       string `json:"text"`
			StopReason string `json:"stop_reason"`
		} `json:"delta"`
		Usage claudeUsage `json:"usage"`
		Error claudeError
	}
	if json.Unmarshal(data, &ev) != nil {
		return
	}
	switch ev.Type {
	case "message_start":
		s.id, s.model, s.usage = ev.Message.ID, ev.Message.Model, ev.Message.Usage
		empty := ""
		s.chunk(chatDelta{Role: "assistant", Content: &empty}, nil)
	case "content_block_delta":
		if ev.Delta.Type == "text_delta" {
			s.chunk(chatDelta{Content: &ev.Delta.Text}, nil)
		}
	case "message_delta":
		s.usage.OutputTokens = ev.Usage.OutputTokens
		finish := finishReason(ev.Delta.StopReason)
		s.chunk(chatDelta{}, &finish)
	case "message_stop":
		if s.includeUsage {
			s.emit(chatCompletion{ID: s.id, Object: "chat.completion.chunk", Created: s.created, Model: s.model, Choices: []chatChoice{}, Usage: s.usage.chat()})
		}
		s.pending = append(s.pending, "data: [DONE]\n\n"...)
		s.done = true
	case "error":
		s.pending = append(s.pending, "data: "...)
		s.pending = append(s.pending, chatError("upstream_error", ev.Error)...)
		s.pending = append(s.pending, "\n\n"...)
		s.done = true
	}
}

func (s *chatStream) chunk(delta chatDelta, finish *string) {
	s.emit(chatCompletion{
		ID: s.id, Object: "chat.completion.chunk", Created: s.created, Model: s.model,
		Choices: []chatChoice{{Delta: &delta, FinishReason: finish}},
	})
}

func (s *chatStream) emit(c chatCompletion) {
	b, _ := json.Marshal(c)
	s.pending = append(append(append(s.pending, "data: "...), b...), "\n\n"...)
}

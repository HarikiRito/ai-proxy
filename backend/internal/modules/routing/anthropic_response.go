package routing

import (
	"encoding/json"
	"net/http"
	"strings"
)

type (
	chatCompletion struct {
		ID      string       `json:"id,omitempty"`
		Object  string       `json:"object"`
		Created int64        `json:"created"`
		Model   string       `json:"model,omitempty"`
		Choices []chatChoice `json:"choices"`
		Usage   *chatUsage   `json:"usage,omitempty"`
	}
	chatChoice struct {
		Index        int          `json:"index"`
		Message      *chatMessage `json:"message,omitempty"`
		Delta        *chatDelta   `json:"delta,omitempty"`
		FinishReason *string      `json:"finish_reason"`
	}
	chatMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	chatDelta struct {
		Role    string  `json:"role,omitempty"`
		Content *string `json:"content,omitempty"`
	}
	chatUsage struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		TotalTokens         int `json:"total_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	}
)

type claudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

func (u claudeUsage) chat() *chatUsage {
	prompt := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
	out := &chatUsage{PromptTokens: prompt, CompletionTokens: u.OutputTokens, TotalTokens: prompt + u.OutputTokens}
	out.PromptTokensDetails.CachedTokens = u.CacheReadInputTokens
	return out
}

func finishReason(stop string) string {
	switch stop {
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "refusal":
		return "content_filter"
	}
	return "stop"
}

// chatReply turns a buffered Claude message into a chat completion.
func chatReply(data []byte, created int64) ([]byte, bool) {
	var in struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage claudeUsage `json:"usage"`
	}
	if json.Unmarshal(data, &in) != nil {
		return nil, false
	}
	var text strings.Builder
	for _, b := range in.Content {
		if b.Type == "text" {
			text.WriteString(b.Text)
		}
	}
	finish := finishReason(in.StopReason)
	out, _ := json.Marshal(chatCompletion{
		ID: in.ID, Object: "chat.completion", Created: created, Model: in.Model, Usage: in.Usage.chat(),
		Choices: []chatChoice{{Message: &chatMessage{Role: "assistant", Content: text.String()}, FinishReason: &finish}},
	})
	return out, true
}

type chatErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

func chatErrorType(status int) string {
	switch {
	case status == http.StatusUnauthorized:
		return "authentication_error"
	case status == http.StatusForbidden:
		return "permission_error"
	case status == http.StatusNotFound:
		return "not_found_error"
	case status == http.StatusTooManyRequests:
		return "rate_limit_error"
	case status >= http.StatusInternalServerError:
		return "api_error"
	}
	return "invalid_request_error"
}

type claudeError struct{ Type, Message string }

// chatError wraps a Claude error (body or mid-stream event) in the OpenAI envelope; the code is Claude's error type.
func chatError(typ string, claude claudeError) []byte {
	var b chatErrorBody
	b.Error.Message, b.Error.Type, b.Error.Code = claude.Message, typ, claude.Type
	if b.Error.Message == "" {
		b.Error.Message = "upstream error"
	}
	if b.Error.Code == "" {
		b.Error.Code = "upstream_error"
	}
	out, _ := json.Marshal(b)
	return out
}

func chatErrorFromBody(status int, data []byte) []byte {
	var in struct{ Error claudeError }
	_ = json.Unmarshal(data, &in)
	return chatError(chatErrorType(status), in.Error)
}

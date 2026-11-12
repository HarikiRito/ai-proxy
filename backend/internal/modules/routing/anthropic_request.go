package routing

import (
	"encoding/json"
	"strconv"
	"strings"
)

const defaultMaxTokens = 8192

// Chat fields with no Claude equivalent here; sending any of them keeps Claude targets out of the chain.
var claudeUnsupported = []string{"tools", "tool_choice", "functions", "function_call", "parallel_tool_calls", "audio", "modalities", "top_logprobs", "web_search_options"}

type claudeRequest struct {
	Model         string          `json:"model"`
	System        string          `json:"system,omitempty"`
	Messages      []claudeTurn    `json:"messages"`
	MaxTokens     int             `json:"max_tokens"`
	Temperature   json.RawMessage `json:"temperature,omitempty"`
	TopP          json.RawMessage `json:"top_p,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	Metadata      *claudeMetadata `json:"metadata,omitempty"`
	Stream        bool            `json:"stream,omitempty"`
	CacheControl  json.RawMessage `json:"cache_control"`
}

type (
	claudeMetadata struct {
		UserID string `json:"user_id"`
	}
	claudeTurn struct {
		Role    string        `json:"role"`
		Content []claudeBlock `json:"content"`
	}
	claudeBlock struct {
		Type   string        `json:"type"`
		Text   *string       `json:"text,omitempty"`
		Source *claudeSource `json:"source,omitempty"`
	}
	claudeSource struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type,omitempty"`
		Data      string `json:"data,omitempty"`
		URL       string `json:"url,omitempty"`
	}
)

// chatTranslation is a chat completions request rewritten for Claude targets.
type chatTranslation struct {
	req          claudeRequest
	includeUsage bool
}

type chatTurn struct {
	Role         string          `json:"role"`
	Content      json.RawMessage `json:"content"`
	ToolCalls    json.RawMessage `json:"tool_calls"`
	FunctionCall json.RawMessage `json:"function_call"`
}

type chatIn struct {
	Messages            []chatTurn            `json:"messages"`
	N                   int                   `json:"n"`
	ResponseFormat      struct{ Type string } `json:"response_format"`
	MaxTokens           int                   `json:"max_tokens"`
	MaxCompletionTokens int                   `json:"max_completion_tokens"`
	Stop                json.RawMessage       `json:"stop"`
	User                string                `json:"user"`
	Stream              bool                  `json:"stream"`
	StreamOptions       struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
}

func present(raw json.RawMessage) bool { return len(raw) > 0 && string(raw) != "null" }

// translateChat returns nil when the request cannot be expressed as a Claude messages request.
func translateChat(body map[string]json.RawMessage) *chatTranslation {
	for _, k := range claudeUnsupported {
		if present(body[k]) {
			return nil
		}
	}
	if string(body["logprobs"]) == "true" {
		return nil
	}
	raw, _ := json.Marshal(body)
	var in chatIn
	if json.Unmarshal(raw, &in) != nil || len(in.Messages) == 0 || in.N > 1 || (in.ResponseFormat.Type != "" && in.ResponseFormat.Type != "text") {
		return nil
	}
	req := claudeRequest{MaxTokens: cmpOr(in.MaxCompletionTokens, in.MaxTokens, defaultMaxTokens), Stream: in.Stream, CacheControl: body["cache_control"]}
	if !present(req.CacheControl) {
		req.CacheControl = json.RawMessage(`{"type":"ephemeral"}`)
	}
	if present(body["temperature"]) {
		if temp, err := strconv.ParseFloat(string(body["temperature"]), 64); err != nil || temp > 1 {
			return nil // Claude accepts 0 to 1 only
		}
		req.Temperature = body["temperature"]
	}
	if present(body["top_p"]) {
		req.TopP = body["top_p"]
	}
	if in.User != "" {
		req.Metadata = &claudeMetadata{UserID: in.User}
	}
	var ok bool
	if req.StopSequences, ok = stopSequences(in.Stop); !ok {
		return nil
	}
	if req.System, req.Messages, ok = in.turns(); !ok || len(req.Messages) == 0 {
		return nil
	}
	return &chatTranslation{req: req, includeUsage: in.StreamOptions.IncludeUsage}
}

func cmpOr(vals ...int) int {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}

func stopSequences(raw json.RawMessage) ([]string, bool) {
	if !present(raw) {
		return nil, true
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}, true
	}
	var many []string
	return many, json.Unmarshal(raw, &many) == nil
}

// turns splits the messages into the top-level system prompt and user/assistant turns.
func (in chatIn) turns() (system string, turns []claudeTurn, ok bool) {
	var sys []string
	for _, m := range in.Messages {
		if present(m.ToolCalls) || present(m.FunctionCall) {
			return "", nil, false
		}
		blocks, ok := m.blocks()
		if !ok {
			return "", nil, false
		}
		switch m.Role {
		case "system", "developer":
			for _, b := range blocks {
				if b.Text == nil {
					return "", nil, false
				}
				sys = append(sys, *b.Text)
			}
		case "user", "assistant":
			turns = append(turns, claudeTurn{Role: m.Role, Content: blocks})
		default:
			return "", nil, false
		}
	}
	return strings.Join(sys, "\n\n"), turns, true
}

func (m chatTurn) blocks() ([]claudeBlock, bool) {
	if !present(m.Content) {
		return []claudeBlock{}, true
	}
	var text string
	if json.Unmarshal(m.Content, &text) == nil {
		return []claudeBlock{{Type: "text", Text: &text}}, true
	}
	var parts []struct {
		Type     string  `json:"type"`
		Text     *string `json:"text"`
		ImageURL struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if json.Unmarshal(m.Content, &parts) != nil {
		return nil, false
	}
	blocks := make([]claudeBlock, 0, len(parts))
	for _, p := range parts {
		switch {
		case p.Type == "text" && p.Text != nil:
			blocks = append(blocks, claudeBlock{Type: "text", Text: p.Text})
		case p.Type == "image_url":
			src, ok := imageSource(p.ImageURL.URL)
			if !ok {
				return nil, false
			}
			blocks = append(blocks, claudeBlock{Type: "image", Source: &src})
		default:
			return nil, false
		}
	}
	return blocks, true
}

// imageSource accepts base64 data URIs and http(s) URLs.
func imageSource(url string) (claudeSource, bool) {
	if rest, found := strings.CutPrefix(url, "data:"); found {
		meta, data, hasData := strings.Cut(rest, ",")
		mediaType, isBase64 := strings.CutSuffix(meta, ";base64")
		return claudeSource{Type: "base64", MediaType: mediaType, Data: data}, hasData && isBase64 && mediaType != ""
	}
	return claudeSource{Type: "url", URL: url}, strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://")
}

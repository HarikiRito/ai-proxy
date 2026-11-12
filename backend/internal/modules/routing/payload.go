package routing

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/textproto"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/providers"
	"github.com/HarikiRito/ai-proxy/backend/internal/modules/virtualmodels"
)

// Part is one buffered multipart form field with its original headers.
type Part struct {
	Name   string
	Header textproto.MIMEHeader
	Data   []byte
}

// payload describes one endpoint: the model kind it serves, the provider kinds it can be sent to,
// its upstream path, and how to encode the body for a real model name.
type payload struct {
	kind     virtualmodels.Kind
	accepts  []providers.Kind
	path     string
	build    func(model string, kind providers.Kind) (body []byte, contentType string)
	body     map[string]json.RawMessage // the client body, kept for cache hints
	affinity bool                       // may be pinned to a target and given cache hints
	shape    affinityShape              // how affinity reads this body; set when affinity is
	chat     *chatTranslation           // set when Claude targets can serve this chat request; their answers are translated back
}

const claudePath = "/messages"

var (
	openAIOnly     = []providers.Kind{providers.KindOpenAICompatible}
	openAIOrClaude = []providers.Kind{providers.KindOpenAICompatible, providers.KindAnthropic}
)

// chatPayload also reaches Claude targets when the body can be translated.
func chatPayload(body map[string]json.RawMessage) payload {
	pl := payload{kind: virtualmodels.KindChat, accepts: openAIOnly, path: "/chat/completions", chat: translateChat(body), body: body, affinity: true, shape: chatShape}
	pl.build = func(model string, kind providers.Kind) ([]byte, string) {
		if kind == providers.KindAnthropic {
			req := pl.chat.req
			req.Model = model
			out, _ := json.Marshal(req)
			return out, "application/json"
		}
		return withModel(body, model), "application/json"
	}
	if pl.chat != nil {
		pl.accepts = openAIOrClaude
	}
	return pl
}

// messagesPayload is the native Anthropic messages body, passed through with only the model rewritten.
func messagesPayload(body map[string]json.RawMessage) payload {
	return payload{kind: virtualmodels.KindChat, accepts: []providers.Kind{providers.KindAnthropic}, path: claudePath, body: body, affinity: true, shape: messagesShape, build: func(model string, _ providers.Kind) ([]byte, string) {
		return withModel(body, model), "application/json"
	}}
}

// formPayload re-emits the parts under a fresh boundary; only the "model" field value changes.
func formPayload(parts []Part) payload {
	return payload{kind: virtualmodels.KindTranscription, accepts: openAIOnly, path: "/audio/transcriptions", build: func(model string, _ providers.Kind) ([]byte, string) {
		size := 1024
		for _, p := range parts {
			size += len(p.Data) + 256
		}
		b := bytes.NewBuffer(make([]byte, 0, size))
		w := multipart.NewWriter(b)
		for _, p := range parts {
			data := p.Data
			if p.Name == "model" {
				data = []byte(model)
			}
			pw, _ := w.CreatePart(p.Header)
			_, _ = pw.Write(data)
		}
		_ = w.Close()
		return b.Bytes(), w.FormDataContentType()
	}}
}

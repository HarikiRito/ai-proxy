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
	kind    virtualmodels.Kind
	accepts []providers.Kind
	path    string
	build   func(model string) (body []byte, contentType string)
}

var openAIOnly = []providers.Kind{providers.KindOpenAICompatible}

func chatPayload(body map[string]json.RawMessage) payload {
	return payload{virtualmodels.KindChat, openAIOnly, "/chat/completions", func(model string) ([]byte, string) {
		return withModel(body, model), "application/json"
	}}
}

// messagesPayload is the native Anthropic messages body, passed through with only the model rewritten.
func messagesPayload(body map[string]json.RawMessage) payload {
	return payload{virtualmodels.KindChat, []providers.Kind{providers.KindAnthropic}, "/messages", func(model string) ([]byte, string) {
		return withModel(body, model), "application/json"
	}}
}

// formPayload re-emits the parts under a fresh boundary; only the "model" field value changes.
func formPayload(parts []Part) payload {
	return payload{virtualmodels.KindTranscription, openAIOnly, "/audio/transcriptions", func(model string) ([]byte, string) {
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

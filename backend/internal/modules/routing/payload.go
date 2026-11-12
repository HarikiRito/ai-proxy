package routing

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/textproto"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/virtualmodels"
)

// Part is one buffered multipart form field with its original headers.
type Part struct {
	Name   string
	Header textproto.MIMEHeader
	Data   []byte
}

// payload describes one endpoint: the model kind it serves, its upstream path, and how to encode the body for a real model name.
type payload struct {
	kind  virtualmodels.Kind
	path  string
	build func(model string) (body []byte, contentType string)
}

func chatPayload(body map[string]json.RawMessage) payload {
	return payload{virtualmodels.KindChat, "/chat/completions", func(model string) ([]byte, string) {
		return withModel(body, model), "application/json"
	}}
}

// formPayload re-emits the parts under a fresh boundary; only the "model" field value changes.
func formPayload(parts []Part) payload {
	return payload{virtualmodels.KindTranscription, "/audio/transcriptions", func(model string) ([]byte, string) {
		var b bytes.Buffer
		w := multipart.NewWriter(&b)
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

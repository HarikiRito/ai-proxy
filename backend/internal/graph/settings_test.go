package graph

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/settings"
	"github.com/HarikiRito/ai-proxy/backend/internal/platform/db"
)

func settingsHandler(t *testing.T) http.Handler {
	t.Helper()
	client, _, err := db.Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "app.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return NewHandler(&Resolver{Settings: settings.New(settings.Deps{DB: client})}, false)
}

func TestSettingsRoundTrip(t *testing.T) {
	h := settingsHandler(t)
	const fields = `maxBodyBytes requestTimeoutSeconds queueWaitSeconds defaultCooldownSeconds`
	out := gql(t, h, `{ settings { `+fields+` } }`, nil)
	if want := `{"data":{"settings":{"maxBodyBytes":26214400,"requestTimeoutSeconds":600,"queueWaitSeconds":30,"defaultCooldownSeconds":60}}}`; out != want {
		t.Fatalf("defaults: %s", out)
	}
	out = gql(t, h, `mutation { updateSettings(input: {queueWaitSeconds: 9}) { `+fields+` } }`, nil)
	if !strings.Contains(out, `"queueWaitSeconds":9`) || !strings.Contains(out, `"requestTimeoutSeconds":600`) {
		t.Fatalf("update: %s", out)
	}
	if out = gql(t, h, `{ settings { queueWaitSeconds } }`, nil); !strings.Contains(out, `"queueWaitSeconds":9`) {
		t.Fatalf("read back: %s", out)
	}
	out = gql(t, h, `mutation { updateSettings(input: {maxBodyBytes: 0}) { maxBodyBytes } }`, nil)
	if !strings.Contains(out, "max body bytes must be between 1 and") {
		t.Fatalf("validation hidden: %s", out)
	}
}

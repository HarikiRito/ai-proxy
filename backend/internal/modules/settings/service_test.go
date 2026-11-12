package settings

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/HarikiRito/ai-proxy/backend/internal/ent"
	"github.com/HarikiRito/ai-proxy/backend/internal/platform/db"
)

func newClient(t *testing.T) *ent.Client {
	t.Helper()
	client, _, err := db.Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "app.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func ptr(n int) *int { return &n }

func TestGetReturnsDefaultsWithoutRow(t *testing.T) {
	got, err := New(Deps{DB: newClient(t)}).Get(context.Background())
	if err != nil || got != defaults() {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got.MaxBodyBytes != 26214400 || got.RequestTimeoutSeconds != 600 || got.QueueWaitSeconds != 30 || got.DefaultCooldownSeconds != 60 {
		t.Fatalf("defaults drifted: %+v", got)
	}
}

func TestUpdatePersistsAndKeepsUnsetFields(t *testing.T) {
	client := newClient(t)
	s := New(Deps{DB: client})
	ctx := context.Background()
	if _, err := s.Update(ctx, Input{QueueWaitSeconds: ptr(5)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(ctx, Input{DefaultCooldownSeconds: ptr(120)}); err != nil {
		t.Fatal(err)
	}
	want := defaults()
	want.QueueWaitSeconds, want.DefaultCooldownSeconds = 5, 120
	fresh, err := New(Deps{DB: client}).Get(ctx)
	if err != nil || fresh != want {
		t.Fatalf("got %+v, %v; want %+v", fresh, err, want)
	}
}

func TestUpdateRejectsOutOfRangeAndKeepsState(t *testing.T) {
	s := New(Deps{DB: newClient(t)})
	ctx := context.Background()
	cases := map[string]Input{
		"body zero":       {MaxBodyBytes: ptr(0)},
		"body over 1GiB":  {MaxBodyBytes: ptr(1<<30 + 1)},
		"timeout zero":    {RequestTimeoutSeconds: ptr(0)},
		"timeout over":    {RequestTimeoutSeconds: ptr(86401)},
		"wait negative":   {QueueWaitSeconds: ptr(-1)},
		"wait over":       {QueueWaitSeconds: ptr(601)},
		"cooldown zero":   {DefaultCooldownSeconds: ptr(0)},
		"cooldown over":   {DefaultCooldownSeconds: ptr(86401)},
		"one bad of many": {QueueWaitSeconds: ptr(10), RequestTimeoutSeconds: ptr(0)},
	}
	for name, in := range cases {
		var ue *UserError
		if _, err := s.Update(ctx, in); !errors.As(err, &ue) {
			t.Errorf("%s: want UserError, got %v", name, err)
		}
	}
	if got, _ := s.Get(ctx); got != defaults() {
		t.Fatalf("rejected update leaked: %+v", got)
	}
	max := Input{MaxBodyBytes: ptr(1 << 30), RequestTimeoutSeconds: ptr(86400), QueueWaitSeconds: ptr(600), DefaultCooldownSeconds: ptr(1)}
	if _, err := s.Update(ctx, max); err != nil {
		t.Fatalf("bounds are inclusive: %v", err)
	}
}

func TestGetCacheIsInvalidatedByUpdate(t *testing.T) {
	client := newClient(t)
	s := New(Deps{DB: client})
	ctx := context.Background()
	if _, err := s.Get(ctx); err != nil {
		t.Fatal(err)
	}
	// a row written behind the service's back stays hidden: Get is served from memory
	if err := client.Setting.Create().SetMaxBodyBytes(1).SetRequestTimeoutSeconds(1).SetQueueWaitSeconds(1).SetDefaultCooldownSeconds(1).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx); got != defaults() {
		t.Fatalf("Get hit the db: %+v", got)
	}
	got, err := s.Update(ctx, Input{QueueWaitSeconds: ptr(7)})
	if err != nil || got.QueueWaitSeconds != 7 {
		t.Fatalf("update: %+v, %v", got, err)
	}
	if again, _ := s.Get(ctx); again != got {
		t.Fatalf("Get after Update: %+v want %+v", again, got)
	}
}

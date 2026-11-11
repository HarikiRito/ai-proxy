package events

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type evt string

func (e evt) Name() string { return string(e) }

type ctxKey struct{}

func TestPublishRunsMatchingHandlersInOrderWithCallerCtx(t *testing.T) {
	bus := New()
	var calls []string
	record := func(label string) Handler {
		return func(ctx context.Context, _ Event) error {
			calls = append(calls, label+":"+ctx.Value(ctxKey{}).(string))
			return nil
		}
	}
	bus.Subscribe("a", record("first"))
	bus.Subscribe("a", record("second"))
	bus.Subscribe("b", record("other"))

	ctx := context.WithValue(context.Background(), ctxKey{}, "tx")
	if err := bus.Publish(ctx, evt("a")); err != nil {
		t.Fatal(err)
	}
	if want := []string{"first:tx", "second:tx"}; !slices.Equal(calls, want) {
		t.Fatalf("got %v want %v", calls, want)
	}
}

func TestPublishStopsAtFirstError(t *testing.T) {
	bus := New()
	boom := errors.New("boom")
	ran := false
	bus.Subscribe("a", func(context.Context, Event) error { return boom })
	bus.Subscribe("a", func(context.Context, Event) error { ran = true; return nil })

	if err := bus.Publish(context.Background(), evt("a")); !errors.Is(err, boom) || ran {
		t.Fatalf("err=%v secondRan=%v", err, ran)
	}
}

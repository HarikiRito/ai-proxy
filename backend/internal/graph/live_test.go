package graph

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/modules/routing"
)

type fakeLive []routing.KeyStatus

func (f fakeLive) Snapshot(context.Context) ([]routing.KeyStatus, error) { return f, nil }

func TestLiveStatusRoundTrip(t *testing.T) {
	until := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	h := NewHandler(&Resolver{Live: fakeLive{
		{ProviderID: "p1", ProviderName: "P", ProviderEnabled: true, KeyID: "k1", KeyName: "main", KeyEnabled: true, MaxInFlight: 3, InFlight: 1, Free: 2},
		{ProviderID: "p1", ProviderName: "P", ProviderEnabled: false, KeyID: "k2", KeyName: "cool", KeyEnabled: true, MaxInFlight: 1, Free: 1, CooldownUntil: &until, Flag: "needs_login"},
	}}, false)
	out := gql(t, h, `{ liveStatus { providerId providerName keyId keyName enabled maxInFlight inFlight free cooldownUntil flag } }`, nil)
	var got struct {
		Data struct {
			LiveStatus []struct {
				KeyID         string
				Enabled       bool
				InFlight      int
				Free          int
				CooldownUntil *time.Time
				Flag          string
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(out)
	}
	k := got.Data.LiveStatus
	if len(k) != 2 || k[0].KeyID != "k1" || !k[0].Enabled || k[0].InFlight != 1 || k[0].Free != 2 || k[0].CooldownUntil != nil {
		t.Fatal(out)
	}
	if k[0].Flag != "" || k[1].Flag != "needs_login" {
		t.Fatal(out)
	}
	if k[1].Enabled || k[1].CooldownUntil == nil || !k[1].CooldownUntil.Equal(until) {
		t.Fatal(out)
	}
}

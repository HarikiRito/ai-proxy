package virtualmodels

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/HarikiRito/ai-proxy/backend/internal/platform/db"
)

type fakeDirectory map[string]ProviderInfo

func (f fakeDirectory) ProviderInfo(_ context.Context, ids []string) (map[string]ProviderInfo, error) {
	out := map[string]ProviderInfo{}
	for _, id := range ids {
		if info, ok := f[id]; ok {
			out[id] = info
		}
	}
	return out, nil
}

func (f fakeDirectory) add(name, kind string, enabled bool) string {
	id := uuid.NewString()
	f[id] = ProviderInfo{Name: name, Kind: kind, Enabled: enabled}
	return id
}

func newService(t *testing.T) (*Service, fakeDirectory) {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "app.db") + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate"
	client, _, err := db.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	dir := fakeDirectory{}
	return New(Deps{DB: client, Providers: dir}), dir
}

func tier(entries ...EntryInput) TierInput { return TierInput{Entries: entries} }

func entry(provider, model string, weight int) EntryInput {
	return EntryInput{ProviderID: provider, Model: model, Weight: weight}
}

func chatInput(name string, tiers ...TierInput) CreateInput {
	return CreateInput{Name: name, Kind: KindChat, Strategy: StrategyRoundRobin, CacheAffinity: true, Tiers: tiers}
}

func updateOf(m CreateInput) UpdateInput {
	return UpdateInput{Kind: m.Kind, Strategy: m.Strategy, CacheAffinity: m.CacheAffinity, Tiers: m.Tiers}
}

func mustCreate(t *testing.T, s *Service, in CreateInput) Model {
	t.Helper()
	m, err := s.Create(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func wantUserError(t *testing.T, err error, contains string) {
	t.Helper()
	var ue *UserError
	if err == nil || !errors.As(err, &ue) || !strings.Contains(ue.Msg, contains) {
		t.Fatalf("want user error containing %q, got %v", contains, err)
	}
}

func shape(m Model) string {
	var b strings.Builder
	b.WriteString("[")
	for _, tr := range m.Tiers {
		var models []string
		for _, e := range tr.Entries {
			models = append(models, e.Model)
		}
		b.WriteString("[" + strings.Join(models, ",") + "]")
	}
	return b.String() + "]"
}

func TestCreateKeepsTierOrderWeightsAndSettings(t *testing.T) {
	s, dir := newService(t)
	a, b := dir.add("openai", "openai_compatible", true), dir.add("groq", "openai_compatible", true)
	in := chatInput("smart", tier(entry(a, "gpt-5", 3), entry(b, "llama", 1)), tier(entry(b, "fallback", 2)))
	in.Strategy, in.CacheAffinity = StrategyWeightedRoundRobin, false

	m := mustCreate(t, s, in)

	if m.Name != "smart" || m.Kind != KindChat || m.Strategy != StrategyWeightedRoundRobin || m.CacheAffinity {
		t.Fatalf("settings: %+v", m)
	}
	if len(m.Tiers) != 2 || m.Tiers[0].Priority != 0 || m.Tiers[1].Priority != 1 {
		t.Fatalf("tiers: %+v", m.Tiers)
	}
	first := m.Tiers[0].Entries
	if len(first) != 2 || first[0].Model != "llama" || first[0].Weight != 1 || first[0].ProviderName != "groq" || first[0].Status != StatusOK ||
		first[1].Model != "gpt-5" || first[1].Weight != 3 || first[1].ProviderName != "openai" {
		t.Fatalf("entries sorted by provider name then model with weights: %+v", first)
	}
	if got := m.Tiers[1].Entries[0]; got.Model != "fallback" || got.Weight != 2 {
		t.Fatalf("tier 1: %+v", got)
	}
	list, err := s.List(context.Background())
	if err != nil || len(list) != 1 || list[0].ID != m.ID || len(list[0].Tiers) != 2 {
		t.Fatalf("list: %+v %v", list, err)
	}
}

func TestSameProviderModelAllowedInDifferentTiers(t *testing.T) {
	s, dir := newService(t)
	a := dir.add("openai", "openai_compatible", true)
	m := mustCreate(t, s, chatInput("m", tier(entry(a, "x", 1)), tier(entry(a, "x", 1))))
	if len(m.Tiers) != 2 {
		t.Fatalf("%+v", m)
	}
}

func TestNameRules(t *testing.T) {
	s, dir := newService(t)
	a := dir.add("openai", "openai_compatible", true)
	for _, ok := range []string{"gpt-4", "a", "claude.3:opus_x", "A1", "  padded  ", strings.Repeat("a", 64)} {
		if _, err := s.Create(context.Background(), chatInput(ok, tier(entry(a, "m", 1)))); err != nil {
			t.Errorf("%q should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "   ", "-x", ".x", "has space", "slash/name", "ünï", strings.Repeat("a", 65)} {
		_, err := s.Create(context.Background(), chatInput(bad, tier(entry(a, "m", 1))))
		wantUserError(t, err, "name")
	}
}

func TestDuplicateNameRefused(t *testing.T) {
	s, dir := newService(t)
	a := dir.add("openai", "openai_compatible", true)
	mustCreate(t, s, chatInput("dup", tier(entry(a, "m", 1))))
	_, err := s.Create(context.Background(), chatInput("dup", tier(entry(a, "m", 1))))
	wantUserError(t, err, "name already exists")
}

func TestStructureValidation(t *testing.T) {
	s, dir := newService(t)
	a := dir.add("openai", "openai_compatible", true)
	cases := map[string]struct {
		in   CreateInput
		want string
	}{
		"no tiers":        {chatInput("m"), "at least one tier"},
		"empty tier":      {chatInput("m", tier()), "at least one entry"},
		"zero weight":     {chatInput("m", tier(entry(a, "x", 0))), "weight must be at least 1"},
		"negative weight": {chatInput("m", tier(entry(a, "x", -2))), "weight must be at least 1"},
		"blank model":     {chatInput("m", tier(entry(a, "  ", 1))), "model"},
		"duplicate entry": {chatInput("m", tier(entry(a, "x", 1), entry(a, " x ", 2))), "duplicate"},
		"bad kind":        {CreateInput{Name: "m", Kind: "video", Strategy: StrategyRoundRobin, Tiers: []TierInput{tier(entry(a, "x", 1))}}, "unknown kind"},
		"bad strategy":    {CreateInput{Name: "m", Kind: KindChat, Strategy: "random", Tiers: []TierInput{tier(entry(a, "x", 1))}}, "unknown strategy"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := s.Create(context.Background(), c.in)
			wantUserError(t, err, c.want)
		})
	}
	if list, _ := s.List(context.Background()); len(list) != 0 {
		t.Fatalf("failed creates must leave nothing: %+v", list)
	}
}

func TestUnknownAndDisabledProvidersRejectedOnSave(t *testing.T) {
	s, dir := newService(t)
	off := dir.add("sleepy", "openai_compatible", false)
	_, err := s.Create(context.Background(), chatInput("m", tier(entry(uuid.NewString(), "x", 1))))
	wantUserError(t, err, "provider not found")
	_, err = s.Create(context.Background(), chatInput("m", tier(entry(off, "x", 1))))
	wantUserError(t, err, "sleepy")
	wantUserError(t, err, "disabled")
}

func TestTranscriptionAcceptsOnlyOpenAICompatibleProviders(t *testing.T) {
	s, dir := newService(t)
	oa := dir.add("openai", "openai_compatible", true)
	an := dir.add("claude-api", "anthropic", true)
	cs := dir.add("claude-sub", "claude_subscription", true)
	in := func(name string, p string) CreateInput {
		c := chatInput(name, tier(entry(p, "whisper-1", 1)))
		c.Kind = KindTranscription
		return c
	}
	mustCreate(t, s, in("stt", oa))
	_, err := s.Create(context.Background(), in("stt-a", an))
	wantUserError(t, err, "claude-api")
	wantUserError(t, err, "audio transcription needs an OpenAI-compatible provider")
	_, err = s.Create(context.Background(), in("stt-c", cs))
	wantUserError(t, err, "claude-sub")

	mustCreate(t, s, chatInput("chat-all", tier(entry(oa, "a", 1), entry(an, "b", 1), entry(cs, "c", 1))))
}

func TestUpdateRevalidatesEntriesWhenKindChanges(t *testing.T) {
	s, dir := newService(t)
	an := dir.add("claude-api", "anthropic", true)
	m := mustCreate(t, s, chatInput("m", tier(entry(an, "x", 1))))
	up := updateOf(chatInput("m", tier(entry(an, "x", 1))))
	up.Kind = KindTranscription
	_, err := s.Update(context.Background(), m.ID, up)
	wantUserError(t, err, "OpenAI-compatible")
}

func TestUpdateReplacesTreeKeepsNameAndBumpsUpdatedAt(t *testing.T) {
	s, dir := newService(t)
	a, b := dir.add("a", "openai_compatible", true), dir.add("b", "openai_compatible", true)
	m := mustCreate(t, s, chatInput("keep", tier(entry(a, "one", 1)), tier(entry(b, "two", 1))))

	up := UpdateInput{Kind: KindChat, Strategy: StrategyLeastInFlight, CacheAffinity: false, Tiers: []TierInput{tier(entry(b, "three", 5))}}
	got, err := s.Update(context.Background(), m.ID, up)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != m.ID || got.Name != "keep" || got.Strategy != StrategyLeastInFlight || got.CacheAffinity {
		t.Fatalf("%+v", got)
	}
	if len(got.Tiers) != 1 || got.Tiers[0].Entries[0].Model != "three" || got.Tiers[0].Entries[0].Weight != 5 {
		t.Fatalf("tree not replaced: %+v", got.Tiers)
	}
	if got.CreatedAt != m.CreatedAt || !got.UpdatedAt.After(m.UpdatedAt) {
		t.Fatalf("timestamps: %v %v vs %v", got.CreatedAt, got.UpdatedAt, m.UpdatedAt)
	}
}

func TestFailedUpdateLeavesPreviousTreeIntact(t *testing.T) {
	s, dir := newService(t)
	a := dir.add("a", "openai_compatible", true)
	m := mustCreate(t, s, chatInput("m", tier(entry(a, "old", 1))))

	_, err := s.Update(context.Background(), m.ID, updateOf(chatInput("m", tier(entry(a, "new", 0)))))
	wantUserError(t, err, "weight")

	// bypass validation so the failure happens inside the transaction, after the old tree is deleted
	bad := updateOf(chatInput("m", tier(entry(a, "x", 1)), tier(entry(a, "dup", 1), entry(a, "dup", 1))))
	if _, err := s.store.update(context.Background(), m.ID, bad); err == nil {
		t.Fatal("constraint violation expected")
	}
	list, _ := s.List(context.Background())
	if len(list) != 1 || shape(list[0]) != "[[old]]" {
		t.Fatalf("old tree lost: %+v", list)
	}
}

func TestReorderAndTierRemovalKeepPrioritiesContiguous(t *testing.T) {
	s, dir := newService(t)
	a := dir.add("a", "openai_compatible", true)
	t0, t1, t2 := tier(entry(a, "zero", 1)), tier(entry(a, "one", 1)), tier(entry(a, "two", 1))
	m := mustCreate(t, s, chatInput("m", t0, t1, t2))

	re, err := s.Update(context.Background(), m.ID, updateOf(chatInput("m", t2, t0, t1)))
	if err != nil {
		t.Fatal(err)
	}
	if shape(re) != "[[two][zero][one]]" || re.Tiers[2].Priority != 2 {
		t.Fatalf("reorder: %v", shape(re))
	}

	del, err := s.Update(context.Background(), m.ID, updateOf(chatInput("m", t2, t1)))
	if err != nil {
		t.Fatal(err)
	}
	if shape(del) != "[[two][one]]" || del.Tiers[0].Priority != 0 || del.Tiers[1].Priority != 1 {
		t.Fatalf("omission: %+v", del.Tiers)
	}
}

func TestStatusFlagsAreComputedOnRead(t *testing.T) {
	s, dir := newService(t)
	gone, off := dir.add("gone", "openai_compatible", true), dir.add("off", "openai_compatible", true)
	swap, fine := dir.add("swap", "openai_compatible", true), dir.add("fine", "openai_compatible", true)
	in := chatInput("stt", tier(entry(gone, "g", 1), entry(off, "o", 1), entry(swap, "s", 1), entry(fine, "f", 1)))
	in.Kind = KindTranscription
	mustCreate(t, s, in)

	delete(dir, gone)
	dir[off] = ProviderInfo{Name: "off", Kind: "openai_compatible", Enabled: false}
	dir[swap] = ProviderInfo{Name: "swap", Kind: "anthropic", Enabled: true}

	list, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]Status{}
	for _, e := range list[0].Tiers[0].Entries {
		status[e.Model] = e.Status
	}
	want := map[string]Status{"g": StatusProviderMissing, "o": StatusProviderDisabled, "s": StatusProviderIncompatible, "f": StatusOK}
	for k, v := range want {
		if status[k] != v {
			t.Errorf("%s: got %q want %q", k, status[k], v)
		}
	}
}

func TestDeleteCascadesAndUnknownIDsAreNotFound(t *testing.T) {
	s, dir := newService(t)
	a := dir.add("a", "openai_compatible", true)
	m := mustCreate(t, s, chatInput("m", tier(entry(a, "x", 1)), tier(entry(a, "y", 1))))
	if err := s.Delete(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if n, _ := s.store.db.VirtualModelTier.Query().Count(ctx); n != 0 {
		t.Fatalf("%d tiers left", n)
	}
	if n, _ := s.store.db.VirtualModelEntry.Query().Count(ctx); n != 0 {
		t.Fatalf("%d entries left", n)
	}
	wantUserError(t, s.Delete(ctx, m.ID), "not found")
	wantUserError(t, s.Delete(ctx, "nope"), "not found")
	_, err := s.Update(ctx, uuid.NewString(), updateOf(chatInput("m", tier(entry(a, "x", 1)))))
	wantUserError(t, err, "not found")
}

func TestByName(t *testing.T) {
	s, dir := newService(t)
	p := dir.add("alpha", "openai_compatible", true)
	created := mustCreate(t, s, chatInput("fast", tier(entry(p, "gpt", 2))))
	mustCreate(t, s, chatInput("other", tier(entry(p, "gpt", 1))))

	got, err := s.ByName(context.Background(), "fast")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID || shape(got) != shape(created) || got.Tiers[0].Entries[0].Status != StatusOK {
		t.Fatalf("got %+v", got)
	}
	if _, err := s.ByName(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

package usage

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/HarikiRito/ai-proxy/backend/internal/ent"
	"github.com/HarikiRito/ai-proxy/backend/internal/platform/db"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func newClient(t *testing.T) *ent.Client {
	t.Helper()
	client, _, err := db.Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "app.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func count(t *testing.T, st *store) int {
	t.Helper()
	n, err := st.db.UsageRecord.Query().Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func rec(model string, status int) Record {
	return Record{ProxyKeyID: "pk", ProxyKeyName: "ci", VirtualModel: model, Status: status, Steps: 1}
}

func TestWriterFlushesOnBatchSizeInterval(t *testing.T) {
	st := &store{db: newClient(t)}
	w := startWriter(st, quiet, time.Now, time.Hour, 3)
	t.Cleanup(w.close)
	for range 3 {
		w.enqueue(rec("m", 200))
	}
	eventually(t, func() bool { return count(t, st) == 3 })

	w.enqueue(rec("m", 200))
	time.Sleep(50 * time.Millisecond)
	if n := count(t, st); n != 3 {
		t.Fatalf("below batch size and before interval nothing is written, got %d", n)
	}

	st2 := &store{db: newClient(t)}
	w2 := startWriter(st2, quiet, time.Now, 10*time.Millisecond, 100)
	t.Cleanup(w2.close)
	w2.enqueue(rec("m", 200))
	eventually(t, func() bool { return count(t, st2) == 1 })
}

func TestWriterCloseDrainsPending(t *testing.T) {
	st := &store{db: newClient(t)}
	w := startWriter(st, quiet, time.Now, time.Hour, 100)
	for range 7 {
		w.enqueue(rec("m", 200))
	}
	w.close()
	w.close()
	if n := count(t, st); n != 7 {
		t.Fatalf("want 7 drained, got %d", n)
	}
}

func TestWriterDropsWhenQueueFullWithoutBlocking(t *testing.T) {
	w := &writer{log: quiet, now: time.Now, queue: make(chan Record, 2)}
	finished := make(chan struct{})
	go func() {
		for range 10 {
			w.enqueue(rec("m", 200))
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("enqueue blocked")
	}
	if got := w.dropped.Load(); got != 8 {
		t.Fatalf("want 8 dropped, got %d", got)
	}
}

func TestRetentionDeletesOldRowsInChunksOnStartAndTick(t *testing.T) {
	client := newClient(t)
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	s := New(Deps{DB: client, Log: quiet, Now: func() time.Time { return now }})
	t.Cleanup(s.Close)
	seed := func(age time.Duration, n int) {
		for range n {
			r := rec("m", 200)
			r.CreatedAt = now.Add(-age)
			s.Record(r)
		}
	}
	seed(31*24*time.Hour, 25)
	seed(29*24*time.Hour, 3)
	s.writer.close()

	tick := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.retain(ctx, tick); close(done) }()
	eventually(t, func() bool { return count(t, s.store) == 3 })

	if err := s.store.insert(ctx, []Record{{ProxyKeyID: "pk", VirtualModel: "m", CreatedAt: now.Add(-40 * 24 * time.Hour), Steps: 1}}); err != nil {
		t.Fatal(err)
	}
	tick <- now
	eventually(t, func() bool { return count(t, s.store) == 3 })
	cancel()
	<-done
}

func TestDeleteBeforeWorksAcrossChunks(t *testing.T) {
	st := &store{db: newClient(t)}
	old := time.Now().Add(-48 * time.Hour)
	var recs []Record
	for range 25 {
		r := rec("m", 200)
		r.CreatedAt = old
		recs = append(recs, r)
	}
	recs = append(recs, Record{ProxyKeyID: "pk", VirtualModel: "m", CreatedAt: time.Now(), Steps: 1})
	if err := st.insert(context.Background(), recs); err != nil {
		t.Fatal(err)
	}
	if err := st.deleteBefore(context.Background(), time.Now().Add(-24*time.Hour), 10); err != nil {
		t.Fatal(err)
	}
	if n := count(t, st); n != 1 {
		t.Fatalf("want 1 kept, got %d", n)
	}
}

func TestTableColumnsAreAnAllowlist(t *testing.T) {
	client, pool, err := db.Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "app.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.QueryContext(context.Background(), `SELECT name FROM pragma_table_info('usage_records')`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
	}
	want := []string{
		"id", "created_at", "proxy_key_id", "proxy_key_name", "virtual_model", "provider_id", "provider_name", "key_id", "key_name", "model",
		"status", "duration_ms", "input_tokens", "output_tokens", "cache_read_tokens", "cache_creation_tokens", "steps", "affinity",
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("columns changed (no content columns allowed):\n got %v\nwant %v", got, want)
	}
}

func ptr[T any](v T) *T { return &v }

func seedService(t *testing.T) *Service {
	t.Helper()
	s := New(Deps{DB: newClient(t), Log: quiet})
	t.Cleanup(s.Close)
	day := func(d, h int) time.Time { return time.Date(2026, 6, d, h, 0, 0, 0, time.UTC) }
	recs := []Record{
		{ProxyKeyID: "a", VirtualModel: "fast", ProviderName: ptr("p1"), Status: 200, InputTokens: ptr(10), OutputTokens: ptr(1), CacheReadTokens: ptr(4), CreatedAt: day(1, 23)},
		{ProxyKeyID: "a", VirtualModel: "fast", ProviderName: ptr("p2"), Status: 502, CreatedAt: day(2, 1)},
		{ProxyKeyID: "b", VirtualModel: "slow", ProviderName: ptr("p1"), Status: 429, InputTokens: ptr(5), OutputTokens: ptr(2), CacheCreationTokens: ptr(3), CreatedAt: day(2, 2)},
		{ProxyKeyID: "b", VirtualModel: "fast", Status: 200, InputTokens: ptr(1), OutputTokens: ptr(1), CreatedAt: day(3, 0)},
	}
	for i := range recs {
		recs[i].ProxyKeyName, recs[i].Steps = "k", 1
	}
	if err := s.store.insert(context.Background(), recs); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRecordsFilterPageAndValidate(t *testing.T) {
	s := seedService(t)
	ctx := context.Background()
	page, err := s.Records(ctx, Filter{}, 0, 0)
	if err != nil || page.Total != 4 || len(page.Items) != 4 || page.Items[0].CreatedAt.Day() != 3 {
		t.Fatalf("default page newest first: %+v %v", page, err)
	}
	page, _ = s.Records(ctx, Filter{ProxyKeyID: ptr("a"), VirtualModel: ptr("fast")}, 1, 1)
	if page.Total != 2 || len(page.Items) != 1 || page.Items[0].CreatedAt.Day() != 1 {
		t.Fatalf("filtered second page: %+v", page)
	}
	from, to := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 2, 23, 0, 0, 0, time.UTC)
	if page, _ = s.Records(ctx, Filter{From: &from, To: &to}, 10, 0); page.Total != 2 {
		t.Fatalf("time range: %+v", page)
	}
	for name, call := range map[string]func() error{
		"limit too big": func() error { _, err := s.Records(ctx, Filter{}, 201, 0); return err },
		"negative":      func() error { _, err := s.Records(ctx, Filter{}, -1, 0); return err },
		"offset":        func() error { _, err := s.Records(ctx, Filter{}, 1, -1); return err },
		"from after to": func() error { _, err := s.Totals(ctx, Filter{From: &to, To: &from}, GroupDay); return err },
	} {
		var ue *UserError
		if err := call(); !errors.As(err, &ue) {
			t.Errorf("%s: want UserError, got %v", name, err)
		}
	}
}

func TestTotalsPerGroup(t *testing.T) {
	s := seedService(t)
	ctx := context.Background()
	day, err := s.Totals(ctx, Filter{}, GroupDay)
	want := []Total{
		{Label: "2026-06-01", Requests: 1, InputTokens: 10, OutputTokens: 1, CacheReadTokens: 4},
		{Label: "2026-06-02", Requests: 2, InputTokens: 5, OutputTokens: 2, CacheCreationTokens: 3, Errors: 2},
		{Label: "2026-06-03", Requests: 1, InputTokens: 1, OutputTokens: 1},
	}
	if err != nil || !slices.Equal(day, want) {
		t.Fatalf("day: %+v %v", day, err)
	}
	model, _ := s.Totals(ctx, Filter{}, GroupModel)
	if len(model) != 2 || model[0].Label != "fast" || model[0].Requests != 3 || model[0].Errors != 1 || model[1].Label != "slow" {
		t.Fatalf("model: %+v", model)
	}
	prov, _ := s.Totals(ctx, Filter{ProxyKeyID: ptr("b")}, GroupProvider)
	if len(prov) != 2 || prov[0].Label != "p1" && prov[0].Label != "unknown" {
		t.Fatalf("provider: %+v", prov)
	}
}

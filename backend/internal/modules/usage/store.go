package usage

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"

	"github.com/HarikiRito/ai-proxy/backend/internal/ent"
	"github.com/HarikiRito/ai-proxy/backend/internal/ent/predicate"
	entrec "github.com/HarikiRito/ai-proxy/backend/internal/ent/usagerecord"
)

type store struct{ db *ent.Client }

// insert writes the batch as one statement, so the write lock is taken once.
func (st *store) insert(ctx context.Context, recs []Record) error {
	creates := make([]*ent.UsageRecordCreate, len(recs))
	for i, r := range recs {
		creates[i] = st.db.UsageRecord.Create().
			SetCreatedAt(r.CreatedAt.UTC()).
			SetProxyKeyID(r.ProxyKeyID).SetProxyKeyName(r.ProxyKeyName).SetVirtualModel(r.VirtualModel).
			SetNillableProviderID(r.ProviderID).SetNillableProviderName(r.ProviderName).
			SetNillableKeyID(r.KeyID).SetNillableKeyName(r.KeyName).SetNillableModel(r.Model).
			SetStatus(r.Status).SetDurationMs(r.DurationMs).SetSteps(r.Steps).
			SetNillableInputTokens(r.InputTokens).SetNillableOutputTokens(r.OutputTokens).
			SetNillableCacheReadTokens(r.CacheReadTokens).SetNillableCacheCreationTokens(r.CacheCreationTokens).
			SetNillableAffinity(r.Affinity)
	}
	return st.db.UsageRecord.CreateBulk(creates...).Exec(ctx)
}

// deleteBefore removes records older than cutoff, chunk ids per statement, so each write lock is short.
func (st *store) deleteBefore(ctx context.Context, cutoff time.Time, chunk int) error {
	for {
		ids, err := st.db.UsageRecord.Query().Where(entrec.CreatedAtLT(cutoff.UTC())).Limit(chunk).IDs(ctx)
		if err != nil || len(ids) == 0 {
			return err
		}
		if _, err := st.db.UsageRecord.Delete().Where(entrec.IDIn(ids...)).Exec(ctx); err != nil {
			return err
		}
	}
}

func where(f Filter) []predicate.UsageRecord {
	var ps []predicate.UsageRecord
	if f.ProxyKeyID != nil {
		ps = append(ps, entrec.ProxyKeyID(*f.ProxyKeyID))
	}
	if f.VirtualModel != nil {
		ps = append(ps, entrec.VirtualModel(*f.VirtualModel))
	}
	if f.From != nil {
		ps = append(ps, entrec.CreatedAtGTE(f.From.UTC()))
	}
	if f.To != nil {
		ps = append(ps, entrec.CreatedAtLTE(f.To.UTC()))
	}
	return ps
}

func (st *store) page(ctx context.Context, f Filter, limit, offset int) (Page, error) {
	q := st.db.UsageRecord.Query().Where(where(f)...)
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return Page{}, err
	}
	rows, err := q.Order(ent.Desc(entrec.FieldCreatedAt), ent.Asc(entrec.FieldID)).Limit(limit).Offset(offset).All(ctx)
	if err != nil {
		return Page{}, err
	}
	items := make([]Record, len(rows))
	for i, row := range rows {
		items[i] = toRecord(row)
	}
	return Page{Items: items, Total: total}, nil
}

// groupLabel is the SQL expression a group is keyed by; created_at is stored in UTC, so its first 10 chars are the day.
func groupLabel(by Group) string {
	switch by {
	case GroupModel:
		return "virtual_model"
	case GroupProvider:
		return "COALESCE(provider_name, 'unknown')"
	}
	return "substr(created_at, 1, 10)"
}

func (st *store) totals(ctx context.Context, f Filter, by Group) ([]Total, error) {
	label := groupLabel(by)
	var rows []struct {
		Label         string `sql:"label"`
		Requests      int    `sql:"requests"`
		In            int    `sql:"input_tokens"`
		Out           int    `sql:"output_tokens"`
		CacheRead     int    `sql:"cache_read_tokens"`
		CacheCreation int    `sql:"cache_creation_tokens"`
		Errors        int    `sql:"errors"`
	}
	order := "requests DESC, label"
	if by == GroupDay {
		order = "label"
	}
	err := st.db.UsageRecord.Query().Where(where(f)...).Modify(func(s *sql.Selector) {
		sum := func(col, as string) string { return sql.As(fmt.Sprintf("COALESCE(SUM(%s), 0)", s.C(col)), as) }
		s.Select(
			sql.As(label, "label"), sql.As(sql.Count("*"), "requests"),
			sum(entrec.FieldInputTokens, "input_tokens"), sum(entrec.FieldOutputTokens, "output_tokens"),
			sum(entrec.FieldCacheReadTokens, "cache_read_tokens"), sum(entrec.FieldCacheCreationTokens, "cache_creation_tokens"),
			sql.As(fmt.Sprintf("COALESCE(SUM(%s >= 400), 0)", s.C(entrec.FieldStatus)), "errors"),
		).GroupBy(label).OrderExpr(sql.Expr(order))
	}).Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := make([]Total, len(rows))
	for i, r := range rows {
		out[i] = Total{Label: r.Label, Requests: r.Requests, InputTokens: r.In, OutputTokens: r.Out, CacheReadTokens: r.CacheRead, CacheCreationTokens: r.CacheCreation, Errors: r.Errors}
	}
	return out, nil
}

func toRecord(row *ent.UsageRecord) Record {
	return Record{
		ID: row.ID.String(), CreatedAt: row.CreatedAt, ProxyKeyID: row.ProxyKeyID, ProxyKeyName: row.ProxyKeyName, VirtualModel: row.VirtualModel,
		ProviderID: row.ProviderID, ProviderName: row.ProviderName, KeyID: row.KeyID, KeyName: row.KeyName, Model: row.Model,
		Status: row.Status, DurationMs: row.DurationMs, InputTokens: row.InputTokens, OutputTokens: row.OutputTokens,
		CacheReadTokens: row.CacheReadTokens, CacheCreationTokens: row.CacheCreationTokens, Steps: row.Steps, Affinity: row.Affinity,
	}
}

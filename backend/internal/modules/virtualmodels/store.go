package virtualmodels

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/HarikiRito/ai-proxy/backend/internal/ent"
	entmodel "github.com/HarikiRito/ai-proxy/backend/internal/ent/virtualmodel"
	enttier "github.com/HarikiRito/ai-proxy/backend/internal/ent/virtualmodeltier"
)

type store struct{ db *ent.Client }

var errNotFound = userError("not found")

func parseID(id string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, errNotFound
	}
	return parsed, nil
}

// mapErr turns ent not-found and unique-constraint failures into user errors.
func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case ent.IsNotFound(err):
		return errNotFound
	case ent.IsConstraintError(err):
		return userError("name already exists")
	}
	return err
}

// inTx runs fn in one transaction; any error rolls everything back.
func (st *store) inTx(ctx context.Context, fn func(tx *ent.Tx) error) error {
	tx, err := st.db.Tx(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

func (st *store) withTree() func(*ent.VirtualModelQuery) {
	return func(q *ent.VirtualModelQuery) {
		q.WithTiers(func(tq *ent.VirtualModelTierQuery) {
			tq.Order(ent.Asc(enttier.FieldPriority)).WithEntries()
		})
	}
}

func (st *store) list(ctx context.Context) ([]Model, error) {
	q := st.db.VirtualModel.Query().Order(ent.Asc(entmodel.FieldName))
	st.withTree()(q)
	rows, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Model, len(rows))
	for i, row := range rows {
		out[i] = toModel(row)
	}
	return out, nil
}

func (st *store) get(ctx context.Context, id uuid.UUID) (Model, error) {
	q := st.db.VirtualModel.Query().Where(entmodel.ID(id))
	st.withTree()(q)
	row, err := q.Only(ctx)
	if err != nil {
		return Model{}, mapErr(err)
	}
	return toModel(row), nil
}

func (st *store) byName(ctx context.Context, name string) (Model, error) {
	q := st.db.VirtualModel.Query().Where(entmodel.Name(name))
	st.withTree()(q)
	row, err := q.Only(ctx)
	if ent.IsNotFound(err) {
		return Model{}, ErrNotFound
	}
	if err != nil {
		return Model{}, err
	}
	return toModel(row), nil
}

func (st *store) create(ctx context.Context, in CreateInput) (Model, error) {
	var id uuid.UUID
	err := st.inTx(ctx, func(tx *ent.Tx) error {
		row, err := tx.VirtualModel.Create().
			SetName(in.Name).
			SetKind(entmodel.Kind(in.Kind)).
			SetStrategy(entmodel.Strategy(in.Strategy)).
			SetCacheAffinity(in.CacheAffinity).
			Save(ctx)
		if err != nil {
			return err
		}
		id = row.ID
		return createTiers(ctx, tx, id, in.Tiers)
	})
	if err != nil {
		return Model{}, mapErr(err)
	}
	return st.get(ctx, id)
}

// update swaps the whole tier tree in one transaction; a failure keeps the previous tree.
func (st *store) update(ctx context.Context, id string, in UpdateInput) (Model, error) {
	uid, err := parseID(id)
	if err != nil {
		return Model{}, err
	}
	err = st.inTx(ctx, func(tx *ent.Tx) error {
		if err := tx.VirtualModel.UpdateOneID(uid).
			SetKind(entmodel.Kind(in.Kind)).
			SetStrategy(entmodel.Strategy(in.Strategy)).
			SetCacheAffinity(in.CacheAffinity).
			Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.VirtualModelTier.Delete().Where(enttier.HasModelWith(entmodel.ID(uid))).Exec(ctx); err != nil {
			return err
		}
		return createTiers(ctx, tx, uid, in.Tiers)
	})
	if err != nil {
		return Model{}, mapErr(err)
	}
	return st.get(ctx, uid)
}

// createTiers writes tiers in array order as priorities 0..n-1.
func createTiers(ctx context.Context, tx *ent.Tx, modelID uuid.UUID, tiers []TierInput) error {
	for priority, t := range tiers {
		row, err := tx.VirtualModelTier.Create().SetModelID(modelID).SetPriority(priority).Save(ctx)
		if err != nil {
			return err
		}
		for _, e := range t.Entries {
			providerID, err := uuid.Parse(e.ProviderID)
			if err != nil {
				return errNotFound
			}
			if err := tx.VirtualModelEntry.Create().
				SetTierID(row.ID).SetProviderID(providerID).SetModel(e.Model).SetWeight(e.Weight).
				Exec(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

func (st *store) delete(ctx context.Context, id string) error {
	uid, err := parseID(id)
	if err != nil {
		return err
	}
	return mapErr(st.db.VirtualModel.DeleteOneID(uid).Exec(ctx))
}

func toModel(row *ent.VirtualModel) Model {
	m := Model{
		ID: row.ID.String(), Name: row.Name, Kind: Kind(row.Kind), Strategy: Strategy(row.Strategy),
		CacheAffinity: row.CacheAffinity, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		Tiers: make([]Tier, len(row.Edges.Tiers)),
	}
	for i, t := range row.Edges.Tiers {
		tier := Tier{Priority: t.Priority, Entries: make([]Entry, len(t.Edges.Entries))}
		for j, e := range t.Edges.Entries {
			tier.Entries[j] = Entry{ID: e.ID.String(), ProviderID: e.ProviderID.String(), Model: e.Model, Weight: e.Weight}
		}
		m.Tiers[i] = tier
	}
	return m
}

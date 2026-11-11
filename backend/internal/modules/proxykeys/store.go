package proxykeys

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/HarikiRito/ai-proxy/backend/internal/ent"
	entkey "github.com/HarikiRito/ai-proxy/backend/internal/ent/proxykey"
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

func (st *store) create(ctx context.Context, name, hash string, models []string) (Key, error) {
	row, err := st.db.ProxyKey.Create().SetName(name).SetKeyHash(hash).SetAllowedModels(models).Save(ctx)
	if err != nil {
		return Key{}, mapErr(err)
	}
	return toKey(row), nil
}

func (st *store) list(ctx context.Context) ([]Key, error) {
	rows, err := st.db.ProxyKey.Query().Order(ent.Desc(entkey.FieldCreatedAt)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Key, len(rows))
	for i, row := range rows {
		out[i] = toKey(row)
	}
	return out, nil
}

// revoke only writes while revoked_at is null, so the first revocation time survives repeats.
func (st *store) revoke(ctx context.Context, id string) (Key, error) {
	uid, err := parseID(id)
	if err != nil {
		return Key{}, err
	}
	_, err = st.db.ProxyKey.Update().
		Where(entkey.ID(uid), entkey.RevokedAtIsNil()).
		SetRevokedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return Key{}, err
	}
	return st.get(ctx, uid)
}

func (st *store) setAllowedModels(ctx context.Context, id string, models []string) (Key, error) {
	uid, err := parseID(id)
	if err != nil {
		return Key{}, err
	}
	row, err := st.db.ProxyKey.UpdateOneID(uid).SetAllowedModels(models).Save(ctx)
	if err != nil {
		return Key{}, mapErr(err)
	}
	return toKey(row), nil
}

// byHash returns ErrInvalidKey when no key has the hash; it also returns the stored hash for comparison.
func (st *store) byHash(ctx context.Context, hash string) (Key, string, error) {
	row, err := st.db.ProxyKey.Query().Where(entkey.KeyHash(hash)).Only(ctx)
	if ent.IsNotFound(err) {
		return Key{}, "", ErrInvalidKey
	}
	if err != nil {
		return Key{}, "", err
	}
	return toKey(row), row.KeyHash, nil
}

func (st *store) touch(ctx context.Context, id string) error {
	uid, err := parseID(id)
	if err != nil {
		return err
	}
	return mapErr(st.db.ProxyKey.UpdateOneID(uid).SetLastUsedAt(time.Now()).Exec(ctx))
}

func (st *store) get(ctx context.Context, id uuid.UUID) (Key, error) {
	row, err := st.db.ProxyKey.Get(ctx, id)
	if err != nil {
		return Key{}, mapErr(err)
	}
	return toKey(row), nil
}

func toKey(row *ent.ProxyKey) Key {
	status := StatusActive
	if row.RevokedAt != nil {
		status = StatusRevoked
	}
	return Key{
		ID:            row.ID.String(),
		Name:          row.Name,
		AllowedModels: row.AllowedModels,
		CreatedAt:     row.CreatedAt,
		LastUsedAt:    row.LastUsedAt,
		RevokedAt:     row.RevokedAt,
		Status:        status,
	}
}

package providers

import (
	"context"

	"github.com/google/uuid"

	"github.com/HarikiRito/ai-proxy/backend/internal/ent"
	entprovider "github.com/HarikiRito/ai-proxy/backend/internal/ent/provider"
	"github.com/HarikiRito/ai-proxy/backend/internal/ent/providerkey"
)

type store struct{ db *ent.Client }

type sealedSecret struct {
	ciphertext []byte
	last4      string
}

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

func (st *store) list(ctx context.Context) ([]Provider, error) {
	rows, err := st.db.Provider.Query().
		WithKeys(func(q *ent.ProviderKeyQuery) { q.Order(ent.Asc(providerkey.FieldCreatedAt)) }).
		Order(ent.Asc(entprovider.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Provider, len(rows))
	for i, row := range rows {
		out[i] = toProvider(row)
	}
	return out, nil
}

func (st *store) create(ctx context.Context, in ProviderInput) (Provider, error) {
	c := st.db.Provider.Create().
		SetName(*in.Name).
		SetKind(entprovider.Kind(*in.Kind)).
		SetBaseURL(*in.BaseURL)
	if in.Enabled != nil {
		c.SetEnabled(*in.Enabled)
	}
	if in.Rotation != nil {
		c.SetRotation(entprovider.Rotation(*in.Rotation))
	}
	if in.CacheAffinitySeconds != nil {
		c.SetCacheAffinitySeconds(*in.CacheAffinitySeconds)
	}
	row, err := c.Save(ctx)
	if err != nil {
		return Provider{}, mapErr(err)
	}
	return toProvider(row), nil
}

func (st *store) update(ctx context.Context, id string, in ProviderInput) (Provider, error) {
	uid, err := parseID(id)
	if err != nil {
		return Provider{}, err
	}
	if err := st.rejectSubscriptionWithKeys(ctx, uid, in.Kind); err != nil {
		return Provider{}, err
	}
	u := st.db.Provider.UpdateOneID(uid)
	if in.Name != nil {
		u.SetName(*in.Name)
	}
	if in.Kind != nil {
		u.SetKind(entprovider.Kind(*in.Kind))
	}
	if in.BaseURL != nil {
		u.SetBaseURL(*in.BaseURL)
	}
	if in.Enabled != nil {
		u.SetEnabled(*in.Enabled)
	}
	if in.Rotation != nil {
		u.SetRotation(entprovider.Rotation(*in.Rotation))
	}
	if in.CacheAffinitySeconds != nil {
		u.SetCacheAffinitySeconds(*in.CacheAffinitySeconds)
	}
	if _, err := u.Save(ctx); err != nil {
		return Provider{}, mapErr(err)
	}
	row, err := st.db.Provider.Query().Where(entprovider.ID(uid)).WithKeys().Only(ctx)
	if err != nil {
		return Provider{}, mapErr(err)
	}
	return toProvider(row), nil
}

func (st *store) rejectSubscriptionWithKeys(ctx context.Context, id uuid.UUID, kind *Kind) error {
	if kind == nil || *kind != KindClaudeSubscription {
		return nil
	}
	n, err := st.db.ProviderKey.Query().Where(providerkey.HasProviderWith(entprovider.ID(id))).Count(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return userError("claude subscription providers take no API keys: remove its keys first")
	}
	return nil
}

func (st *store) delete(ctx context.Context, id string) error {
	uid, err := parseID(id)
	if err != nil {
		return err
	}
	return mapErr(st.db.Provider.DeleteOneID(uid).Exec(ctx))
}

func (st *store) addKey(ctx context.Context, providerID string, in KeyInput, sealed *sealedSecret) (Key, error) {
	uid, err := parseID(providerID)
	if err != nil {
		return Key{}, err
	}
	p, err := st.db.Provider.Get(ctx, uid)
	if err != nil {
		return Key{}, mapErr(err)
	}
	if Kind(p.Kind) == KindClaudeSubscription {
		return Key{}, userError("claude subscription providers do not take API keys; accounts are managed in a later release")
	}
	c := st.db.ProviderKey.Create().
		SetProviderID(uid).
		SetName(*in.Name).
		SetCiphertext(sealed.ciphertext).
		SetLast4(sealed.last4).
		SetMaxInFlight(*in.MaxInFlight)
	if in.Enabled != nil {
		c.SetEnabled(*in.Enabled)
	}
	if in.Weight != nil {
		c.SetWeight(*in.Weight)
	}
	if in.Priority != nil {
		c.SetPriority(*in.Priority)
	}
	row, err := c.Save(ctx)
	if err != nil {
		return Key{}, mapErr(err)
	}
	return toKey(row), nil
}

func (st *store) updateKey(ctx context.Context, id string, in KeyInput, sealed *sealedSecret) (Key, error) {
	uid, err := parseID(id)
	if err != nil {
		return Key{}, err
	}
	u := st.db.ProviderKey.UpdateOneID(uid)
	if in.Name != nil {
		u.SetName(*in.Name)
	}
	if sealed != nil {
		u.SetCiphertext(sealed.ciphertext).SetLast4(sealed.last4)
	}
	if in.Enabled != nil {
		u.SetEnabled(*in.Enabled)
	}
	if in.MaxInFlight != nil {
		u.SetMaxInFlight(*in.MaxInFlight)
	}
	if in.Weight != nil {
		u.SetWeight(*in.Weight)
	}
	if in.Priority != nil {
		u.SetPriority(*in.Priority)
	}
	row, err := u.Save(ctx)
	if err != nil {
		return Key{}, mapErr(err)
	}
	return toKey(row), nil
}

func (st *store) deleteKey(ctx context.Context, id string) error {
	uid, err := parseID(id)
	if err != nil {
		return err
	}
	return mapErr(st.db.ProviderKey.DeleteOneID(uid).Exec(ctx))
}

func (st *store) ciphertext(ctx context.Context, keyID string) ([]byte, error) {
	uid, err := parseID(keyID)
	if err != nil {
		return nil, err
	}
	row, err := st.db.ProviderKey.Get(ctx, uid)
	if err != nil {
		return nil, mapErr(err)
	}
	return row.Ciphertext, nil
}

func toProvider(row *ent.Provider) Provider {
	p := Provider{
		ID:                   row.ID.String(),
		Name:                 row.Name,
		Kind:                 Kind(row.Kind),
		BaseURL:              row.BaseURL,
		Enabled:              row.Enabled,
		Rotation:             Rotation(row.Rotation),
		CacheAffinitySeconds: row.CacheAffinitySeconds,
		Keys:                 make([]Key, len(row.Edges.Keys)),
	}
	for i, k := range row.Edges.Keys {
		p.Keys[i] = toKey(k)
		if k.Enabled {
			p.Capacity += k.MaxInFlight
		}
	}
	return p
}

func toKey(row *ent.ProviderKey) Key {
	return Key{
		ID:          row.ID.String(),
		Name:        row.Name,
		Enabled:     row.Enabled,
		MaxInFlight: row.MaxInFlight,
		Weight:      row.Weight,
		Priority:    row.Priority,
		MaskedKey:   maskedKey(row.Last4),
	}
}

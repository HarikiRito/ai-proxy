package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

// ProxyKey is a client credential for the proxy; owned by the proxykeys module. Only the sha256 hash is stored.
type ProxyKey struct {
	ent.Schema
}

func (ProxyKey) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "proxykeys_keys"}}
}

func (ProxyKey) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.String("name").NotEmpty().Unique(),
		field.String("key_hash").NotEmpty().Unique().Sensitive(),
		field.JSON("allowed_models", []string{}).Default(func() []string { return []string{} }),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("last_used_at").Optional().Nillable(),
		field.Time("revoked_at").Optional().Nillable(),
	}
}

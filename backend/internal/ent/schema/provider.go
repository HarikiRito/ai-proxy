package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Provider is an upstream AI provider; owned by the providers module.
type Provider struct {
	ent.Schema
}

func (Provider) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "providers_providers"}}
}

func (Provider) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.String("name").NotEmpty().Unique(),
		field.Enum("kind").Values("openai_compatible", "anthropic", "claude_subscription"),
		field.String("base_url").NotEmpty(),
		field.Bool("enabled").Default(true),
		field.Enum("rotation").
			Values("round_robin", "weighted_round_robin", "priority_overflow", "least_in_flight").
			Default("round_robin"),
		field.Int("cache_affinity_seconds").Default(600).NonNegative(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Provider) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("keys", ProviderKey.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

// ProviderKey is an encrypted API key of a provider; ciphertext never leaves the providers module.
type ProviderKey struct {
	ent.Schema
}

func (ProviderKey) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "providers_keys"}}
}

func (ProviderKey) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.String("name").NotEmpty(),
		field.Bytes("ciphertext").Sensitive(),
		field.String("last4").Default(""),
		field.Bool("enabled").Default(true),
		field.Int("max_in_flight").Positive(),
		field.Int("weight").Default(1).Positive(),
		field.Int("priority").Default(0),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (ProviderKey) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("provider", Provider.Type).Ref("keys").Unique().Required(),
	}
}

func (ProviderKey) Indexes() []ent.Index {
	return []ent.Index{index.Fields("name").Edges("provider").Unique()}
}

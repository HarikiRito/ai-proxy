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

// VirtualModel is a client-facing model name routed to provider entries; owned by the virtualmodels module.
type VirtualModel struct {
	ent.Schema
}

func (VirtualModel) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "virtualmodels_models"}}
}

func (VirtualModel) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.String("name").NotEmpty().Unique(),
		field.Enum("kind").Values("chat", "transcription"),
		field.Enum("strategy").
			Values("round_robin", "weighted_round_robin", "priority_overflow", "least_in_flight").
			Default("round_robin"),
		field.Bool("cache_affinity").Default(true),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (VirtualModel) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("tiers", VirtualModelTier.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

// VirtualModelTier is one priority level of a virtual model; lower priority is tried first.
type VirtualModelTier struct {
	ent.Schema
}

func (VirtualModelTier) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "virtualmodels_tiers"}}
}

func (VirtualModelTier) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.Int("priority").NonNegative(),
	}
}

func (VirtualModelTier) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("model", VirtualModel.Type).Ref("tiers").Unique().Required(),
		edge.To("entries", VirtualModelEntry.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (VirtualModelTier) Indexes() []ent.Index {
	return []ent.Index{index.Fields("priority").Edges("model").Unique()}
}

// VirtualModelEntry routes a tier to an upstream model; provider_id is a plain column (providers owns the table).
type VirtualModelEntry struct {
	ent.Schema
}

func (VirtualModelEntry) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "virtualmodels_entries"}}
}

func (VirtualModelEntry) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.UUID("provider_id", uuid.UUID{}),
		field.String("model").NotEmpty(),
		field.Int("weight").Default(1).Positive(),
	}
}

func (VirtualModelEntry) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tier", VirtualModelTier.Type).Ref("entries").Unique().Required(),
	}
}

func (VirtualModelEntry) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider_id"),
		index.Fields("provider_id", "model").Edges("tier").Unique(),
	}
}

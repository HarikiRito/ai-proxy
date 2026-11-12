package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// UsageRecord is one proxied request; owned by the usage module. Metadata only, never request or response content.
type UsageRecord struct {
	ent.Schema
}

func (UsageRecord) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "usage_records"}}
}

// Provider, key and entry refs are plain ids plus name snapshots: those rows can be deleted.
// input_tokens is the whole prompt (cache reads and creation included); cache columns are the parts of it.
func (UsageRecord) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.String("proxy_key_id"),
		field.String("proxy_key_name"),
		field.String("virtual_model"),
		field.String("provider_id").Optional().Nillable(),
		field.String("provider_name").Optional().Nillable(),
		field.String("key_id").Optional().Nillable(),
		field.String("key_name").Optional().Nillable(),
		field.String("model").Optional().Nillable(),
		field.Int("status"),
		field.Int("duration_ms"),
		field.Int("input_tokens").Optional().Nillable(),
		field.Int("output_tokens").Optional().Nillable(),
		field.Int("cache_read_tokens").Optional().Nillable(),
		field.Int("cache_creation_tokens").Optional().Nillable(),
		field.Int("steps"),
		field.String("affinity").Optional().Nillable(),
	}
}

func (UsageRecord) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("created_at"),
		index.Fields("proxy_key_id", "created_at"),
		index.Fields("virtual_model", "created_at"),
		index.Fields("provider_id", "created_at"),
	}
}

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// Setting is the singleton runtime-settings row (id 1); owned by the settings module. Defaults live in the module.
type Setting struct {
	ent.Schema
}

func (Setting) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "settings_settings"}}
}

func (Setting) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id").Default(1).Immutable(),
		field.Int("max_body_bytes"),
		field.Int("request_timeout_seconds"),
		field.Int("queue_wait_seconds"),
		field.Int("default_cooldown_seconds"),
	}
}

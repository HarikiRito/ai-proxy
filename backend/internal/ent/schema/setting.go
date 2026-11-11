package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Setting is a placeholder entity: ent cannot generate without one. The first real entity replaces it.
type Setting struct {
	ent.Schema
}

func (Setting) Fields() []ent.Field {
	return []ent.Field{
		field.String("key").NotEmpty().Unique(),
		field.String("value"),
	}
}

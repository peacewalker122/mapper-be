package mapper

import (
	"errors"
	"testing"
)

func TestMemorySchemaRegistryValidatesAndCopies(t *testing.T) {
	registry := NewMemorySchemaRegistry()
	schema := Schema{
		ID:   2,
		Name: "subscriber",
		Fields: []Field{
			{ID: 1, Name: "email", Type: TypeString, Required: true},
		},
	}
	if err := registry.Register(schema); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	schema.Fields[0].Name = "mutated"

	got, err := registry.Get(schema.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Fields[0].Name != "email" {
		t.Fatalf("stored schema changed through caller: %#v", got)
	}
	got.Fields[0].Name = "changed"
	again, err := registry.Get(schema.ID)
	if err != nil {
		t.Fatalf("second Get() error = %v", err)
	}
	if again.Fields[0].Name != "email" {
		t.Fatalf("returned schema aliases registry storage: %#v", again)
	}
}

func TestMemorySchemaRegistryRejectsInvalidAndDuplicateSchemas(t *testing.T) {
	registry := NewMemorySchemaRegistry()
	invalid := Schema{ID: 1, Name: "broken", Fields: []Field{{ID: 1, Name: "x", Type: "unknown"}}}
	if err := registry.Register(invalid); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("invalid schema error = %v, want ErrInvalidSchema", err)
	}

	valid := Schema{ID: 1, Name: "one", Fields: []Field{{ID: 1, Name: "x", Type: TypeString}}}
	if err := registry.Register(valid); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Register(valid); !errors.Is(err, ErrSchemaExists) {
		t.Fatalf("duplicate schema error = %v, want ErrSchemaExists", err)
	}
	if err := registry.Register(Schema{ID: 2, Name: "one", Fields: valid.Fields}); !errors.Is(err, ErrSchemaExists) {
		t.Fatalf("duplicate name error = %v, want ErrSchemaExists", err)
	}
}

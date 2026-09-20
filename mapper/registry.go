package mapper

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

var (
	ErrInvalidSchema  = errors.New("invalid schema")
	ErrSchemaExists   = errors.New("schema already exists")
	ErrSchemaNotFound = errors.New("schema not found")
)

// SchemaRegistry stores runtime schemas used by the mapper service.
type SchemaRegistry interface {
	Register(Schema) error
	Get(uint64) (Schema, error)
	List() []Schema
}

// MemorySchemaRegistry is a concurrency-safe in-memory SchemaRegistry.
type MemorySchemaRegistry struct {
	mu      sync.RWMutex
	schemas map[uint64]Schema
}

func NewMemorySchemaRegistry() *MemorySchemaRegistry {
	return &MemorySchemaRegistry{schemas: make(map[uint64]Schema)}
}

func (r *MemorySchemaRegistry) Register(schema Schema) error {
	if r == nil {
		return fmt.Errorf("%w: nil registry", ErrInvalidSchema)
	}
	if err := validateSchema(schema); err != nil {
		return err
	}

	copy := cloneSchema(schema)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.schemas == nil {
		r.schemas = make(map[uint64]Schema)
	}
	if _, exists := r.schemas[schema.ID]; exists {
		return fmt.Errorf("%w: id %d", ErrSchemaExists, schema.ID)
	}
	for _, registered := range r.schemas {
		if registered.Name == schema.Name {
			return fmt.Errorf("%w: name %q", ErrSchemaExists, schema.Name)
		}
	}
	r.schemas[schema.ID] = copy
	return nil
}

func (r *MemorySchemaRegistry) RegisterSchema(schema Schema) error {
	return r.Register(schema)
}

func (r *MemorySchemaRegistry) Get(id uint64) (Schema, error) {
	if r == nil {
		return Schema{}, fmt.Errorf("%w: nil registry", ErrSchemaNotFound)
	}
	if id == 0 {
		return Schema{}, fmt.Errorf("%w: id is zero", ErrSchemaNotFound)
	}

	r.mu.RLock()
	schema, ok := r.schemas[id]
	r.mu.RUnlock()
	if !ok {
		return Schema{}, fmt.Errorf("%w: id %d", ErrSchemaNotFound, id)
	}
	return cloneSchema(schema), nil
}

func (r *MemorySchemaRegistry) GetSchema(id uint64) (Schema, error) {
	return r.Get(id)
}

func (r *MemorySchemaRegistry) List() []Schema {
	if r == nil {
		return nil
	}

	r.mu.RLock()
	result := make([]Schema, 0, len(r.schemas))
	for _, schema := range r.schemas {
		result = append(result, cloneSchema(schema))
	}
	r.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (r *MemorySchemaRegistry) Schemas() []Schema {
	return r.List()
}

func validateSchema(schema Schema) error {
	if schema.ID == 0 {
		return fmt.Errorf("%w: schema id is zero", ErrInvalidSchema)
	}
	if schema.Name == "" {
		return fmt.Errorf("%w: schema name is empty", ErrInvalidSchema)
	}
	if len(schema.Fields) == 0 {
		return fmt.Errorf("%w: schema %q has no fields", ErrInvalidSchema, schema.Name)
	}

	fieldIDs := make(map[uint64]struct{}, len(schema.Fields))
	fieldNames := make(map[string]struct{}, len(schema.Fields))
	for index, field := range schema.Fields {
		if field.ID == 0 {
			return fmt.Errorf("%w: field %d id is zero", ErrInvalidSchema, index)
		}
		if field.Name == "" {
			return fmt.Errorf("%w: field %d name is empty", ErrInvalidSchema, index)
		}
		if _, exists := fieldIDs[field.ID]; exists {
			return fmt.Errorf("%w: duplicate field id %d", ErrInvalidSchema, field.ID)
		}
		if _, exists := fieldNames[field.Name]; exists {
			return fmt.Errorf("%w: duplicate field name %q", ErrInvalidSchema, field.Name)
		}
		if !supportedFieldType(field.Type) {
			return fmt.Errorf("%w: field %q has unsupported type %q", ErrInvalidSchema, field.Name, field.Type)
		}
		fieldIDs[field.ID] = struct{}{}
		fieldNames[field.Name] = struct{}{}
	}
	return nil
}

func supportedFieldType(fieldType FieldType) bool {
	switch fieldType {
	case TypeString, TypeInteger, TypeDecimal, TypeBoolean, TypeDateTime:
		return true
	default:
		return false
	}
}

func cloneSchema(schema Schema) Schema {
	schema.Fields = append([]Field(nil), schema.Fields...)
	return schema
}

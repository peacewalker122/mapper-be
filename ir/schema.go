package ir

type Schema struct {
	Version uint32
	Model   Model
}

type Model struct {
	ID     uint64
	Name   string
	Fields []Field
}

type Field struct {
	ID       uint64
	Name     string
	Type     FieldType
	Required bool
}

type FieldType string

const (
	TypeString   FieldType = "string"
	TypeInteger  FieldType = "integer"
	TypeDecimal  FieldType = "decimal"
	TypeBoolean  FieldType = "boolean"
	TypeDateTime FieldType = "datetime"
)

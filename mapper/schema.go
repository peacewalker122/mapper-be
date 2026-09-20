package mapper

type FieldType string

const (
	TypeString   FieldType = "string"
	TypeInteger  FieldType = "integer"
	TypeDecimal  FieldType = "decimal"
	TypeBoolean  FieldType = "boolean"
	TypeDateTime FieldType = "datetime"
)

type Schema struct {
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

package mapper

// MappingSpec is the portable source-to-target mapping contract.
type MappingSpec struct {
	FileID   FileID         `json:"file_id"`
	SchemaID uint64         `json:"schema_id"`
	Sheet    int            `json:"sheet"`
	Mappings []FieldMapping `json:"mappings"`
}

// FieldMapping maps one source column index to one stable target field ID.
type FieldMapping struct {
	Source int    `json:"source"`
	Target uint64 `json:"target"`
}

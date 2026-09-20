package executor

import (
	"errors"
	"fmt"

	"github.com/peacewalker122/mapper/mapper"
)

const (
	CodeInvalidMapping        = "invalid_mapping"
	CodeInvalidSourceIndex    = "invalid_source_index"
	CodeUnknownTarget         = "unknown_target"
	CodeDuplicateTarget       = "duplicate_target"
	CodeRequiredTargetMissing = "required_target_missing"
	CodeRequiredValueMissing  = "required_value_missing"
	CodeInvalidInteger        = "invalid_integer"
	CodeInvalidDecimal        = "invalid_decimal"
	CodeInvalidBoolean        = "invalid_boolean"
	CodeInvalidDateTime       = "invalid_datetime"
	CodeProcessorError        = "processor_error"
)

// MappingError is returned when a mapping cannot be compiled before rows run.
type MappingError struct {
	Code        string
	SourceIndex *int
	TargetID    *uint64
	Message     string
}

func (e *MappingError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return e.Code
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func mappingError(code, message string, sourceIndex *int, targetID *uint64) *MappingError {
	return &MappingError{Code: code, Message: message, SourceIndex: sourceIndex, TargetID: targetID}
}

// ValidateMapping checks stable target IDs, source indexes, uniqueness, and
// required target coverage before a plan enters the row loop.
func ValidateMapping(spec mapper.MappingSpec, schema mapper.Schema, sourceCount ...int) error {
	if schema.ID == 0 {
		return mappingError(CodeInvalidMapping, "schema ID is zero", nil, nil)
	}
	if spec.SchemaID != 0 && spec.SchemaID != schema.ID {
		return mappingError(CodeInvalidMapping, fmt.Sprintf("mapping schema %d does not match schema %d", spec.SchemaID, schema.ID), nil, nil)
	}
	if len(sourceCount) > 1 {
		return mappingError(CodeInvalidMapping, "source count specified more than once", nil, nil)
	}
	count := -1
	if len(sourceCount) == 1 {
		count = sourceCount[0]
		if count < 0 {
			return mappingError(CodeInvalidMapping, "source count is negative", nil, nil)
		}
	}

	fields := make(map[uint64]int, len(schema.Fields))
	for index, field := range schema.Fields {
		if field.ID == 0 {
			return mappingError(CodeInvalidMapping, fmt.Sprintf("schema field %q has zero ID", field.Name), nil, nil)
		}
		if _, exists := fields[field.ID]; exists {
			return mappingError(CodeInvalidMapping, fmt.Sprintf("schema has duplicate field ID %d", field.ID), nil, nil)
		}
		fields[field.ID] = index
	}

	mapped := make(map[int]struct{}, len(spec.Mappings))
	for _, fieldMapping := range spec.Mappings {
		source := fieldMapping.Source
		if source < 0 || (count >= 0 && source >= count) {
			return mappingError(CodeInvalidSourceIndex, fmt.Sprintf("source index %d is outside [0,%d)", source, count), &source, nil)
		}
		target := fieldMapping.Target
		if target == 0 {
			return mappingError(CodeUnknownTarget, "target ID is zero", &source, &target)
		}
		targetIndex, exists := fields[target]
		if !exists {
			return mappingError(CodeUnknownTarget, fmt.Sprintf("target field ID %d does not exist", target), &source, &target)
		}
		if _, exists := mapped[targetIndex]; exists {
			return mappingError(CodeDuplicateTarget, fmt.Sprintf("target field ID %d is mapped more than once", target), &source, &target)
		}
		mapped[targetIndex] = struct{}{}
	}

	for index, field := range schema.Fields {
		if field.Required {
			if _, exists := mapped[index]; !exists {
				target := field.ID
				return mappingError(CodeRequiredTargetMissing, fmt.Sprintf("required target field %q is not mapped", field.Name), nil, &target)
			}
		}
	}
	return nil
}

func rowError(code, message string, row int64, sourceIndex *int, targetID *uint64) mapper.RowError {
	return mapper.RowError{
		Row:         row,
		SourceIndex: sourceIndex,
		TargetID:    targetID,
		Code:        code,
		Message:     message,
	}
}

func rowErrorFrom(err error, row int64) mapper.RowError {
	if err == nil {
		return mapper.RowError{Row: row}
	}
	var existing mapper.RowError
	if errors.As(err, &existing) {
		existing.Row = row
		return existing
	}
	var mapping *MappingError
	if errors.As(err, &mapping) && mapping != nil {
		return rowError(mapping.Code, mapping.Message, row, mapping.SourceIndex, mapping.TargetID)
	}
	return rowError(CodeInvalidMapping, err.Error(), row, nil, nil)
}

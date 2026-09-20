package executor

import (
	"fmt"
	"strconv"
	"time"

	"github.com/peacewalker122/mapper/mapper"
)

// Convert converts one non-empty source value to its schema type.
func Convert(raw string, fieldType mapper.FieldType) (mapper.Value, error) {
	switch fieldType {
	case mapper.TypeString:
		return raw, nil
	case mapper.TypeInteger:
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, rowError(CodeInvalidInteger, fmt.Sprintf("%q is not an integer: %v", raw, err), 0, nil, nil)
		}
		return value, nil
	case mapper.TypeDecimal:
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, rowError(CodeInvalidDecimal, fmt.Sprintf("%q is not a decimal: %v", raw, err), 0, nil, nil)
		}
		return value, nil
	case mapper.TypeBoolean:
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, rowError(CodeInvalidBoolean, fmt.Sprintf("%q is not a boolean: %v", raw, err), 0, nil, nil)
		}
		return value, nil
	case mapper.TypeDateTime:
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil, rowError(CodeInvalidDateTime, fmt.Sprintf("%q is not RFC3339: %v", raw, err), 0, nil, nil)
		}
		return value, nil
	default:
		return nil, rowError(CodeInvalidMapping, fmt.Sprintf("unsupported field type %q", fieldType), 0, nil, nil)
	}
}

// ConvertValue is a descriptive alias for Convert.
func ConvertValue(raw string, fieldType mapper.FieldType) (mapper.Value, error) {
	return Convert(raw, fieldType)
}

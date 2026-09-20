package compiler

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/peacewalker122/mapper/ir"
)

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var supportedTypes = []string{
	string(ir.TypeString),
	string(ir.TypeInteger),
	string(ir.TypeDecimal),
	string(ir.TypeBoolean),
	string(ir.TypeDateTime),
}

var supportedTypeSet = func() map[string]struct{} {
	types := make(map[string]struct{}, len(supportedTypes))
	for _, fieldType := range supportedTypes {
		types[fieldType] = struct{}{}
	}
	return types
}()

func Validate(schema ParsedSchema) error {
	var validationErrors []error

	if schema.Version != 1 {
		validationErrors = append(validationErrors, fmt.Errorf("unsupported version %d: schema version must be 1", schema.Version))
	}
	if !identifierPattern.MatchString(schema.Model.Name) {
		validationErrors = append(validationErrors, fmt.Errorf("model name %q must match %s", schema.Model.Name, identifierPattern.String()))
	}
	if len(schema.Model.Fields) == 0 {
		validationErrors = append(validationErrors, fmt.Errorf("model %q: fields are empty: must contain at least one field", schema.Model.Name))
	}

	seenFields := make(map[string]struct{}, len(schema.Model.Fields))
	for index, field := range schema.Model.Fields {
		if !identifierPattern.MatchString(field.Name) {
			validationErrors = append(validationErrors, fmt.Errorf("field %d name %q must match %s", index, field.Name, identifierPattern.String()))
		}
		if _, seen := seenFields[field.Name]; seen {
			validationErrors = append(validationErrors, fmt.Errorf("duplicate field name %q", field.Name))
		} else {
			seenFields[field.Name] = struct{}{}
		}
		if _, supported := supportedTypeSet[field.Type]; !supported {
			validationErrors = append(validationErrors, fmt.Errorf("field %q: unsupported type %q; supported types: %s", field.Name, field.Type, strings.Join(supportedTypes, ", ")))
		}
	}

	return errors.Join(validationErrors...)
}

func ParseAndValidate(data []byte) (ParsedSchema, error) {
	schema, err := Parse(data)
	if err != nil {
		return ParsedSchema{}, err
	}
	if err := Validate(schema); err != nil {
		return ParsedSchema{}, err
	}
	return schema, nil
}

package compiler

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

type ParsedSchema struct {
	Version uint32      `yaml:"version"`
	Model   ParsedModel `yaml:"model"`
}

type ParsedModel struct {
	Name   string        `yaml:"name"`
	Fields []ParsedField `yaml:"fields"`
}

type ParsedField struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Required bool   `yaml:"required"`
}

func Parse(data []byte) (ParsedSchema, error) {
	var schema ParsedSchema
	if err := yaml.Unmarshal(data, &schema); err != nil {
		return ParsedSchema{}, fmt.Errorf("parse schema YAML: %w", err)
	}
	return schema, nil
}

func ParseReader(reader io.Reader) (ParsedSchema, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return ParsedSchema{}, fmt.Errorf("read schema: %w", err)
	}
	return Parse(data)
}

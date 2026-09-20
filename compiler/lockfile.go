package compiler

import (
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

type FieldStatus string

const (
	FieldActive  FieldStatus = "active"
	FieldRemoved FieldStatus = "removed"
)

type LockFile struct {
	Version uint32       `yaml:"version"`
	Schema  LockSchema   `yaml:"schema"`
	Fields  map[string]LockField `yaml:"fields"`
}

type LockSchema struct {
	ID   uint64 `yaml:"id"`
	Name string `yaml:"name"`
}

type LockField struct {
	ID     uint64      `yaml:"id"`
	Status FieldStatus `yaml:"status"`
}

func ParseLock(data []byte) (*LockFile, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var l LockFile
	if err := yaml.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("parse lock file: %w", err)
	}
	if l.Fields == nil {
		l.Fields = map[string]LockField{}
	}
	return &l, nil
}

func (l *LockFile) Marshal() ([]byte, error) {
	if l == nil {
		return nil, fmt.Errorf("nil lock file")
	}
	// Deterministic key order is handled by yaml marshal of maps? Sort via wrapper.
	// Use an ordered marshal by constructing a yaml.Node with sorted keys.
	return marshalLockDeterministic(l)
}

func marshalLockDeterministic(l *LockFile) ([]byte, error) {
	type fieldOut struct {
		ID     uint64      `yaml:"id"`
		Status FieldStatus `yaml:"status"`
	}
	type lockOut struct {
		Version uint32              `yaml:"version"`
		Schema  LockSchema          `yaml:"schema"`
		Fields  yaml.Node           `yaml:"fields"`
	}
	// Build fields node with sorted keys for determinism.
	fieldsNode := yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	names := make([]string, 0, len(l.Fields))
	for n := range l.Fields {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		f := l.Fields[n]
		key := yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: n}
		val := yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		idKey := yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "id"}
		idVal := yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", f.ID)}
		stKey := yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "status"}
		stVal := yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: string(f.Status)}
		val.Content = append(val.Content, &idKey, &idVal, &stKey, &stVal)
		fieldsNode.Content = append(fieldsNode.Content, &key, &val)
	}
	out := lockOut{Version: l.Version, Schema: l.Schema, Fields: fieldsNode}
	// Marshal via node to preserve order: encode manually.
	root := yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	vk := yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "version"}
	vv := yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", out.Version)}
	sk := yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "schema"}
	sv := yaml.Node{}
	if err := sv.Encode(out.Schema); err != nil {
		return nil, err
	}
	fk := yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "fields"}
	root.Content = append(root.Content, &vk, &vv, &sk, &sv, &fk, &fieldsNode)
	return yaml.Marshal(&root)
}

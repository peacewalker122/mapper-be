package compiler

import (
	"fmt"

	"github.com/peacewalker122/mapper/codegen/golang"
	"github.com/peacewalker122/mapper/idgen"
	"github.com/peacewalker122/mapper/ir"
)

type Compiler struct {
	IDGenerator idgen.IDGenerator
}

type CompileRequest struct {
	Source   []byte
	LockFile []byte
	Package  string
}

type CompileResult struct {
	Schema      ir.Schema
	LockFile    []byte
	GeneratedGo []byte
}

func (c *Compiler) generator() idgen.IDGenerator {
	if c == nil || c.IDGenerator == nil {
		return idgen.CryptoRandomIDGenerator{}
	}
	return c.IDGenerator
}

func (c *Compiler) Compile(req CompileRequest) (*CompileResult, error) {
	pkg := req.Package
	if pkg == "" {
		pkg = "generated"
	}
	parsed, err := ParseAndValidate(req.Source)
	if err != nil {
		return nil, err
	}
	lock, err := ParseLock(req.LockFile)
	if err != nil {
		return nil, fmt.Errorf("load lock file: %w", err)
	}
	schema, updatedLock, err := ResolveIDs(parsed, lock, c.generator())
	if err != nil {
		return nil, err
	}
	gen, err := golang.Generate(schema, pkg)
	if err != nil {
		return nil, err
	}
	lockBytes, err := updatedLock.Marshal()
	if err != nil {
		return nil, fmt.Errorf("marshal lock file: %w", err)
	}
	return &CompileResult{Schema: schema, LockFile: lockBytes, GeneratedGo: gen}, nil
}

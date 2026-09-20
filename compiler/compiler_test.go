package compiler

import (
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/peacewalker122/mapper/idgen"
)

func testCompiler(start uint64) *Compiler {
	return &Compiler{IDGenerator: idgen.NewSequentialIDGenerator(start)}
}

func TestParserValid(t *testing.T) {
	data, err := os.ReadFile("../testdata/subscriber.yaml")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	s, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s.Model.Name != "subscriber" {
		t.Fatalf("model name = %q", s.Model.Name)
	}
	if len(s.Model.Fields) != 4 {
		t.Fatalf("fields = %d", len(s.Model.Fields))
	}
}

func TestParserMalformed(t *testing.T) {
	if _, err := Parse([]byte(":\t: bad:\n  - [unclosed")); err == nil {
		t.Fatal("expected error for malformed YAML")
	}
}

func TestParserMissingVersion(t *testing.T) {
	data := []byte("model:\n  name: subscriber\n  fields:\n    - name: msisdn\n      type: string\n")
	s, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := Validate(s); err == nil {
		t.Fatal("expected validation error for missing version")
	}
}

func TestValidation(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"duplicate", "version: 1\nmodel:\n  name: subscriber\n  fields:\n    - {name: a, type: string}\n    - {name: a, type: string}\n", "duplicate"},
		{"invalid-identifier", "version: 1\nmodel:\n  name: Subscriber Model\n  fields:\n    - {name: a, type: string}\n", "model name"},
		{"unknown-type", "version: 1\nmodel:\n  name: subscriber\n  fields:\n    - {name: msisdn, type: strng}\n", "unsupported type"},
		{"empty-fields", "version: 1\nmodel:\n  name: subscriber\n  fields: []\n", "empty"},
		{"unsupported-version", "version: 2\nmodel:\n  name: subscriber\n  fields:\n    - {name: a, type: string}\n", "unsupported version"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Parse([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			err = Validate(s)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q missing %q", err.Error(), tc.want)
			}
		})
	}
}

func compileOnce(t *testing.T, src, lock []byte, start uint64, pkg string) *CompileResult {
	t.Helper()
	c := testCompiler(start)
	res, err := c.Compile(CompileRequest{Source: src, LockFile: lock, Package: pkg})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return res
}

func fieldIDByName(t *testing.T, schemaName string, res *CompileResult, name string) uint64 {
	t.Helper()
	for _, f := range res.Schema.Model.Fields {
		if f.Name == name {
			return f.ID
		}
	}
	t.Fatalf("%s: field %q not found", schemaName, name)
	return 0
}

func TestIDStability(t *testing.T) {
	src, _ := os.ReadFile("../testdata/subscriber.yaml")
	r1 := compileOnce(t, src, nil, 1001, "generated")
	r2 := compileOnce(t, src, r1.LockFile, 1001, "generated")
	if r1.Schema.Model.ID != r2.Schema.Model.ID {
		t.Fatalf("schema ID changed %d -> %d", r1.Schema.Model.ID, r2.Schema.Model.ID)
	}
	for _, f1 := range r1.Schema.Model.Fields {
		found := false
		for _, f2 := range r2.Schema.Model.Fields {
			if f1.Name == f2.Name {
				found = true
				if f1.ID != f2.ID {
					t.Fatalf("field %s ID changed %d -> %d", f1.Name, f1.ID, f2.ID)
				}
			}
		}
		if !found {
			t.Fatalf("field %s missing in second compile", f1.Name)
		}
	}
	if string(r1.GeneratedGo) != string(r2.GeneratedGo) {
		// GeneratedGo should be identical given same lock; lock bytes from r1 reused so IDs same.
		t.Fatal("generated output not deterministic")
	}
}

func TestFieldAddition(t *testing.T) {
	base := []byte("version: 1\nmodel:\n  name: subscriber\n  fields:\n    - {name: msisdn, type: string, required: true}\n    - {name: status, type: string, required: true}\n")
	r1 := compileOnce(t, base, nil, 1001, "generated")
	msisdn1 := fieldIDByName(t, "r1", r1, "msisdn")
	status1 := fieldIDByName(t, "r1", r1, "status")
	extended := []byte("version: 1\nmodel:\n  name: subscriber\n  fields:\n    - {name: msisdn, type: string, required: true}\n    - {name: status, type: string, required: true}\n    - {name: category, type: string}\n")
	c := testCompiler(1001)
	r2, err := c.Compile(CompileRequest{Source: extended, LockFile: r1.LockFile, Package: "generated"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got := fieldIDByName(t, "r2", r2, "msisdn"); got != msisdn1 {
		t.Fatalf("msisdn changed %d -> %d", msisdn1, got)
	}
	if got := fieldIDByName(t, "r2", r2, "status"); got != status1 {
		t.Fatalf("status changed %d -> %d", status1, got)
	}
	cat := fieldIDByName(t, "r2", r2, "category")
	if cat == msisdn1 || cat == status1 || cat == 0 {
		t.Fatalf("category ID invalid: %d", cat)
	}
}

func TestFieldRemoval(t *testing.T) {
	base := []byte("version: 1\nmodel:\n  name: subscriber\n  fields:\n    - {name: msisdn, type: string}\n    - {name: status, type: string}\n")
	r1 := compileOnce(t, base, nil, 1001, "generated")
	statusID := fieldIDByName(t, "r1", r1, "status")
	reduced := []byte("version: 1\nmodel:\n  name: subscriber\n  fields:\n    - {name: msisdn, type: string}\n")
	c := testCompiler(1001)
	r2, err := c.Compile(CompileRequest{Source: reduced, LockFile: r1.LockFile, Package: "generated"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, f := range r2.Schema.Model.Fields {
		if f.Name == "status" {
			t.Fatal("removed field still in IR")
		}
	}
	lock, err := ParseLock(r2.LockFile)
	if err != nil {
		t.Fatalf("ParseLock: %v", err)
	}
	lf, ok := lock.Fields["status"]
	if !ok {
		t.Fatal("removed field missing from lock")
	}
	if lf.Status != FieldRemoved {
		t.Fatalf("status = %q, want removed", lf.Status)
	}
	if lf.ID != statusID {
		t.Fatalf("removed ID changed %d -> %d", statusID, lf.ID)
	}
	if strings.Contains(string(r2.GeneratedGo), "Status") {
		t.Fatal("generated Go still contains removed field")
	}
}

func TestFieldReadd(t *testing.T) {
	base := []byte("version: 1\nmodel:\n  name: subscriber\n  fields:\n    - {name: msisdn, type: string}\n    - {name: status, type: string}\n")
	r1 := compileOnce(t, base, nil, 1001, "generated")
	statusID := fieldIDByName(t, "r1", r1, "status")
	reduced := []byte("version: 1\nmodel:\n  name: subscriber\n  fields:\n    - {name: msisdn, type: string}\n")
	c := testCompiler(1001)
	r2, err := c.Compile(CompileRequest{Source: reduced, LockFile: r1.LockFile, Package: "generated"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	r3, err := c.Compile(CompileRequest{Source: base, LockFile: r2.LockFile, Package: "generated"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got := fieldIDByName(t, "r3", r3, "status"); got != statusID {
		t.Fatalf("re-added ID %d != original %d", got, statusID)
	}
	lock, _ := ParseLock(r3.LockFile)
	if lock.Fields["status"].Status != FieldActive {
		t.Fatal("re-added field not active")
	}
}

func TestGoldenAndSyntax(t *testing.T) {
	src, _ := os.ReadFile("../testdata/subscriber.yaml")
	res := compileOnce(t, src, nil, 1001, "generated")
	// Golden file records deterministic output for sequential IDs starting at 1001.
	goldenPath := filepath.Join("..", "testdata", "subscriber.gen.go.golden")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, res.GeneratedGo, 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		// First run: create golden, pass.
		if err := os.WriteFile(goldenPath, res.GeneratedGo, 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	if string(golden) != string(res.GeneratedGo) {
		t.Fatal("golden mismatch: run with UPDATE_GOLDEN=1 to refresh after intentional changes")
	}
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "subscriber.gen.go", res.GeneratedGo, parser.AllErrors); err != nil {
		t.Fatalf("generated Go does not parse: %v", err)
	}
	if _, err := format.Source(res.GeneratedGo); err != nil {
		t.Fatalf("generated Go does not format: %v", err)
	}
	for _, want := range []string{"type Subscriber struct", "Msisdn", "LastTx", "SubscriberSchema", "Code generated by mapper-gen"} {
		if !strings.Contains(string(res.GeneratedGo), want) {
			t.Fatalf("generated Go missing %q", want)
		}
	}
}

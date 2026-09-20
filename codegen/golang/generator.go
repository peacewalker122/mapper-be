package golang

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/printer"
	"go/token"
	"strconv"
	"strings"

	"github.com/peacewalker122/mapper/ir"
)

func pascalCase(s string) string {
	parts := strings.Split(s, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		if len(p) > 1 {
			b.WriteString(p[1:])
		}
	}
	if b.Len() == 0 {
		return "Model"
	}
	return b.String()
}

func goTypeExpr(ft ir.FieldType, required bool) (ast.Expr, bool) {
	switch ft {
	case ir.TypeString:
		if required {
			return ast.NewIdent("string"), false
		}
		return &ast.StarExpr{X: ast.NewIdent("string")}, false
	case ir.TypeInteger:
		if required {
			return ast.NewIdent("int64"), false
		}
		return &ast.StarExpr{X: ast.NewIdent("int64")}, false
	case ir.TypeDecimal:
		if required {
			return ast.NewIdent("float64"), false
		}
		return &ast.StarExpr{X: ast.NewIdent("float64")}, false
	case ir.TypeBoolean:
		if required {
			return ast.NewIdent("bool"), false
		}
		return &ast.StarExpr{X: ast.NewIdent("bool")}, false
	case ir.TypeDateTime:
		sel := &ast.SelectorExpr{X: ast.NewIdent("time"), Sel: ast.NewIdent("Time")}
		if required {
			return sel, true
		}
		return &ast.StarExpr{X: sel}, true
	default:
		if required {
			return ast.NewIdent("string"), false
		}
		return &ast.StarExpr{X: ast.NewIdent("string")}, false
	}
}

func mapperTypeExpr(ft ir.FieldType) ast.Expr {
	name := "TypeString"
	switch ft {
	case ir.TypeString:
		name = "TypeString"
	case ir.TypeInteger:
		name = "TypeInteger"
	case ir.TypeDecimal:
		name = "TypeDecimal"
	case ir.TypeBoolean:
		name = "TypeBoolean"
	case ir.TypeDateTime:
		name = "TypeDateTime"
	}
	return &ast.SelectorExpr{X: ast.NewIdent("mapper"), Sel: ast.NewIdent(name)}
}

func intLit(v uint64) *ast.BasicLit {
	return &ast.BasicLit{Kind: token.INT, Value: strconv.FormatUint(v, 10)}
}

func stringLit(s string) *ast.BasicLit {
	return &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(s)}
}

func boolIdent(b bool) *ast.Ident {
	if b {
		return ast.NewIdent("true")
	}
	return ast.NewIdent("false")
}

func kv(key string, val ast.Expr) *ast.KeyValueExpr {
	return &ast.KeyValueExpr{Key: ast.NewIdent(key), Value: val}
}

func Generate(schema ir.Schema, pkg string) ([]byte, error) {
	if pkg == "" {
		pkg = "generated"
	}
	structName := pascalCase(schema.Model.Name)
	schemaVar := structName + "Schema"
	needsTime := false

	// Struct fields.
	var structFields []*ast.Field
	for _, f := range schema.Model.Fields {
		typ, nt := goTypeExpr(f.Type, f.Required)
		if nt {
			needsTime = true
		}
		structFields = append(structFields, &ast.Field{
			Names: []*ast.Ident{ast.NewIdent(pascalCase(f.Name))},
			Type:  typ,
		})
	}
	structDecl := &ast.GenDecl{
		Tok: token.TYPE,
		Specs: []ast.Spec{
			&ast.TypeSpec{
				Name: ast.NewIdent(structName),
				Type: &ast.StructType{
					Fields: &ast.FieldList{List: structFields},
				},
			},
		},
	}

	// Schema descriptor: var XSchema = mapper.Schema{...}
	var fieldElts []ast.Expr
	for _, f := range schema.Model.Fields {
		lit := &ast.CompositeLit{
			Elts: []ast.Expr{
				kv("ID", intLit(f.ID)),
				kv("Name", stringLit(f.Name)),
				kv("Type", mapperTypeExpr(f.Type)),
				kv("Required", boolIdent(f.Required)),
			},
		}
		fieldElts = append(fieldElts, lit)
	}
	schemaLit := &ast.CompositeLit{
		Type: &ast.SelectorExpr{X: ast.NewIdent("mapper"), Sel: ast.NewIdent("Schema")},
		Elts: []ast.Expr{
			kv("ID", intLit(schema.Model.ID)),
			kv("Name", stringLit(schema.Model.Name)),
			kv("Type", &ast.CompositeLit{
				// placeholder replaced below: Fields key
				Type: nil,
			}),
		},
	}
	// Build Fields key properly (cannot reuse kv trick above for slice type).
	schemaLit.Elts[2] = kv("Fields", &ast.CompositeLit{
		Type: &ast.ArrayType{Elt: &ast.SelectorExpr{X: ast.NewIdent("mapper"), Sel: ast.NewIdent("Field")}},
		Elts: fieldElts,
	})
	varDecl := &ast.GenDecl{
		Tok: token.VAR,
		Specs: []ast.Spec{
			&ast.ValueSpec{
				Names:  []*ast.Ident{ast.NewIdent(schemaVar)},
				Values: []ast.Expr{schemaLit},
			},
		},
	}

	// Imports.
	var imports []*ast.ImportSpec
	if needsTime {
		imports = append(imports, &ast.ImportSpec{Path: stringLit("time")})
	}
	imports = append(imports, &ast.ImportSpec{Path: stringLit("github.com/peacewalker122/mapper/mapper")})
	importDecl := &ast.GenDecl{
		Tok:    token.IMPORT,
		Lparen: 1,
		Specs:  []ast.Spec{},
	}
	for _, im := range imports {
		importDecl.Specs = append(importDecl.Specs, im)
	}

	file := &ast.File{
		Name:  ast.NewIdent(pkg),
		Decls: []ast.Decl{importDecl, structDecl, varDecl},
	}

	fset := token.NewFileSet()
	posFile := fset.AddFile("generated.go", -1, 1<<20)
	line := 0
	newLine := func() token.Pos {
		line++
		offset := line * 10
		posFile.AddLine(offset)
		return posFile.Pos(offset)
	}
	setScalarKV := func(e ast.Expr, ln token.Pos) {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			return
		}
		if id, ok := kv.Key.(*ast.Ident); ok {
			id.NamePos = ln
		}
		switch v := kv.Value.(type) {
		case *ast.BasicLit:
			v.ValuePos = ln
		case *ast.Ident:
			v.NamePos = ln
		}
	}
	schemaLit.Lbrace = newLine()
	setScalarKV(schemaLit.Elts[0], newLine())
	setScalarKV(schemaLit.Elts[1], newLine())
	fieldsKV, _ := schemaLit.Elts[2].(*ast.KeyValueExpr)
	fieldsLn := newLine()
	if id, ok := fieldsKV.Key.(*ast.Ident); ok {
		id.NamePos = fieldsLn
	}
	sliceLit, _ := fieldsKV.Value.(*ast.CompositeLit)
	sliceLit.Lbrace = fieldsLn
	for _, fe := range fieldElts {
		fl, _ := fe.(*ast.CompositeLit)
		if fl == nil {
			continue
		}
		fl.Lbrace = newLine()
		if sel, ok := fl.Type.(*ast.SelectorExpr); ok {
			if x, ok := sel.X.(*ast.Ident); ok {
				x.NamePos = fl.Lbrace
			}
			sel.Sel.NamePos = fl.Lbrace
		}
		for _, el := range fl.Elts {
			setScalarKV(el, newLine())
		}
		fl.Rbrace = newLine()
	}
	sliceLit.Rbrace = newLine()
	schemaLit.Rbrace = newLine()
	var buf bytes.Buffer
	cfg := &printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(&buf, fset, file); err != nil {
		return nil, fmt.Errorf("print generated Go: %w", err)
	}
	raw := "// Code generated by mapper-gen. DO NOT EDIT.\n\n" + buf.String()
	formatted, err := format.Source([]byte(raw))
	if err != nil {
		return nil, fmt.Errorf("format generated Go: %w\n--- raw ---\n%s", err, raw)
	}
	return formatted, nil
}

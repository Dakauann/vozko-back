package schemascan

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Model struct {
	Type   string
	Table  string
	Fields map[string]bool
}

func (m Model) Has(fields ...string) bool {
	for _, f := range fields {
		if !m.Fields[f] {
			return false
		}
	}
	return true
}

func Models(dir string) ([]Model, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("schemascan: no go files in %s", dir)
	}
	fset := token.NewFileSet()
	fields := map[string]map[string]bool{}
	tables := map[string]string{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("schemascan: parse %s: %w", path, err)
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				collectFields(d, fields)
			case *ast.FuncDecl:
				if recv, table, ok := tableName(d); ok {
					tables[recv] = table
				}
			}
		}
	}
	out := make([]Model, 0, len(tables))
	for typeName, table := range tables {
		out = append(out, Model{Type: typeName, Table: table, Fields: fields[typeName]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Table < out[j].Table })
	return out, nil
}

func collectFields(d *ast.GenDecl, into map[string]map[string]bool) {
	for _, spec := range d.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			continue
		}
		names := map[string]bool{}
		for _, f := range st.Fields.List {
			for _, n := range f.Names {
				names[n.Name] = true
			}
		}
		into[ts.Name.Name] = names
	}
}

func tableName(d *ast.FuncDecl) (string, string, bool) {
	if d.Name.Name != "TableName" || d.Recv == nil || len(d.Recv.List) != 1 || d.Body == nil {
		return "", "", false
	}
	recv := receiver(d.Recv.List[0].Type)
	for _, stmt := range d.Body.List {
		ret, ok := stmt.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			continue
		}
		lit, ok := ret.Results[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			continue
		}
		if v, err := strconv.Unquote(lit.Value); err == nil {
			return recv, v, true
		}
	}
	return "", "", false
}

func receiver(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return receiver(e.X)
	case *ast.Ident:
		return e.Name
	}
	return ""
}

package entrypurge_repository

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type schemaKeys struct {
	entryKeyed   map[string]bool
	contactKeyed map[string]bool
}

func scanSchema(t *testing.T) schemaKeys {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "database", "schema", "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("schema sources not found: %v", err)
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
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
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
					fields[ts.Name.Name] = names
				}
			case *ast.FuncDecl:
				if d.Name.Name != "TableName" || d.Recv == nil || len(d.Recv.List) != 1 || d.Body == nil || len(d.Body.List) != 1 {
					continue
				}
				recv := d.Recv.List[0].Type
				if star, ok := recv.(*ast.StarExpr); ok {
					recv = star.X
				}
				ident, ok := recv.(*ast.Ident)
				if !ok {
					continue
				}
				ret, ok := d.Body.List[0].(*ast.ReturnStmt)
				if !ok || len(ret.Results) != 1 {
					continue
				}
				lit, ok := ret.Results[0].(*ast.BasicLit)
				if !ok {
					continue
				}
				name, err := strconv.Unquote(lit.Value)
				if err == nil {
					tables[ident.Name] = name
				}
			}
		}
	}
	keys := schemaKeys{entryKeyed: map[string]bool{}, contactKeyed: map[string]bool{}}
	for typeName, names := range fields {
		table, ok := tables[typeName]
		if !ok {
			continue
		}
		if names["EntryID"] && names["EntryType"] || names["SourceEntryID"] && names["SourceEntryType"] {
			keys.entryKeyed[table] = true
		}
		if names["ContactID"] && names["ContactType"] {
			keys.contactKeyed[table] = true
		}
	}
	return keys
}

func registered(tables []keyedTable) map[string]bool {
	out := map[string]bool{}
	for _, tbl := range tables {
		out[tbl.table] = true
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestEveryTableKeyedByAnEntryIsPurged(t *testing.T) {
	keys := scanSchema(t)
	if len(keys.entryKeyed) < 10 {
		t.Fatalf("the schema scan found only %v; the scanner is broken", sortedKeys(keys.entryKeyed))
	}
	purged := registered(entryTables)
	for _, table := range sortedKeys(keys.entryKeyed) {
		if !purged[table] {
			t.Errorf("%s is keyed by (entry_id, entry_type) but the entry purge leaves it behind", table)
		}
	}
	for _, table := range sortedKeys(purged) {
		if !keys.entryKeyed[table] {
			t.Errorf("%s is purged by entry but no schema model keys it by entry", table)
		}
	}
}

func TestEveryTableKeyedByAChannelContactIsPurged(t *testing.T) {
	keys := scanSchema(t)
	purged := registered(contactTables)
	for _, table := range sortedKeys(keys.contactKeyed) {
		if !purged[table] {
			t.Errorf("%s is keyed by (contact_id, contact_type) but the contact purge leaves it behind", table)
		}
	}
	for _, table := range sortedKeys(purged) {
		if !keys.contactKeyed[table] {
			t.Errorf("%s is purged by contact but no schema model keys it by contact", table)
		}
	}
}

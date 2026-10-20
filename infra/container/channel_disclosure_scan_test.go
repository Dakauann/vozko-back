package container

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func parsedFiles(t *testing.T, root string, visit func(dir string, file *ast.File)) {
	t.Helper()
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		visit(filepath.Base(filepath.Dir(path)), file)
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
}

func isChannelAutomation(expr ast.Expr) bool {
	switch typ := expr.(type) {
	case *ast.SelectorExpr:
		return typ.Sel.Name == "ChannelAutomation"
	case *ast.Ident:
		return typ.Name == "ChannelAutomation"
	}
	return false
}

func setsDisclosure(lit *ast.CompositeLit) bool {
	for _, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Disclosure" {
				return true
			}
		}
	}
	return false
}

func disclosingChannels(t *testing.T, roots ...string) []string {
	t.Helper()
	found := map[string]bool{}
	for _, root := range roots {
		parsedFiles(t, root, func(dir string, file *ast.File) {
			ast.Inspect(file, func(n ast.Node) bool {
				if lit, ok := n.(*ast.CompositeLit); ok && isChannelAutomation(lit.Type) && setsDisclosure(lit) {
					found[dir] = true
				}
				return true
			})
		})
	}
	channels := make([]string, 0, len(found))
	for dir := range found {
		channels = append(channels, dir)
	}
	sort.Strings(channels)
	return channels
}

func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return ""
	}
	recv := fn.Recv.List[0].Type
	if star, ok := recv.(*ast.StarExpr); ok {
		recv = star.X
	}
	if ident, ok := recv.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

func adapterAnswersDisclosure(t *testing.T, dir string) bool {
	t.Helper()
	methods := map[string]map[string]bool{}
	parsedFiles(t, dir, func(_ string, file *ast.File) {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if recv := receiverName(fn); recv != "" {
				if methods[recv] == nil {
					methods[recv] = map[string]bool{}
				}
				methods[recv][fn.Name.Name] = true
			}
		}
	})
	for _, set := range methods {
		if set["EntryType"] && set["AutomationDisclosure"] {
			return true
		}
	}
	return false
}

func disclosureGaps(t *testing.T, domainRoot, usecasesRoot string) []string {
	t.Helper()
	var gaps []string
	for _, channel := range disclosingChannels(t, domainRoot, usecasesRoot) {
		dir := filepath.Join(usecasesRoot, channel)
		if _, err := os.Stat(dir); err != nil || !adapterAnswersDisclosure(t, dir) {
			gaps = append(gaps, channel)
		}
	}
	return gaps
}

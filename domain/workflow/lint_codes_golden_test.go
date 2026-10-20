package workflow

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"vozko/pkg/codegolden"
)

var lintCodesGolden = filepath.Join("testdata", "lint_codes.golden")

const lintCodesDriftAdvice = "update testdata/lint_codes.golden, and also add the translation to vozko-front src/lib/workflows/lint-codes.ts and to workflowsPage.lint.<CODE> (title and fix) in the pt, en, es and de catalogs"

func declaredLintIssueCodes(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var codes []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				value := spec.(*ast.ValueSpec)
				typed, ok := value.Type.(*ast.Ident)
				if !ok || typed.Name != "LintIssueCode" {
					continue
				}
				for _, literal := range value.Values {
					basic, ok := literal.(*ast.BasicLit)
					if !ok || basic.Kind != token.STRING {
						continue
					}
					code, err := strconv.Unquote(basic.Value)
					if err != nil {
						t.Fatal(err)
					}
					codes = append(codes, code)
				}
			}
		}
	}
	return codes
}

func TestEveryLintIssueCodeIsListedInTheGoldenSoTheFrontCanTranslateIt(t *testing.T) {
	codegolden.Compare(t, lintCodesGolden, declaredLintIssueCodes(t), lintCodesDriftAdvice)
}

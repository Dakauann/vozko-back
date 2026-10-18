package database

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/ddl.golden from the declared DDL")

var goldenDDL = filepath.Join("testdata", "ddl.golden")

func compositeLeadKeyDDL() []ddl {
	statements := make([]ddl, len(compositeLeadKeys))
	for i, key := range compositeLeadKeys {
		statements[i] = ddl{name: key.constraint, sql: key.addSQL()}
	}
	return statements
}

type ddlSection struct {
	title      string
	statements []ddl
}

func declaredDDL() []ddlSection {
	return []ddlSection{
		{"performance indexes", performanceIndexes()},
		{"schema constraints", schemaConstraints()},
		{"lead collection keys", leadCollectionConstraints()},
		{"call list keys", callListConstraints()},
		{"composite lead keys", compositeLeadKeyDDL()},
		{"concurrent indexes", concurrentIndexes()},
	}
}

func renderDDL() string {
	var b strings.Builder
	for _, section := range declaredDDL() {
		b.WriteString("## " + section.title + "\n\n")
		for _, s := range section.statements {
			b.WriteString("-- " + s.name + "\n" + strings.Join(strings.Fields(s.sql), " ") + ";\n\n")
		}
	}
	b.WriteString("## data repairs\n\n" + strings.Join(repairNames(), "\n") + "\n")
	return b.String()
}

func TestTheDeclaredDDLMatchesTheGoldenFile(t *testing.T) {
	got := renderDDL()
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenDDL), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenDDL, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(goldenDDL)
	if err != nil {
		t.Fatalf("read %s: %v; run go test -run TestTheDeclaredDDLMatchesTheGoldenFile -update", goldenDDL, err)
	}
	wantLines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	gotLines := strings.Split(got, "\n")
	for i := 0; i < max(len(wantLines), len(gotLines)); i++ {
		var want, have string
		if i < len(wantLines) {
			want = wantLines[i]
		}
		if i < len(gotLines) {
			have = gotLines[i]
		}
		if want != have {
			t.Fatalf("%s line %d\nwant: %s\ngot:  %s\nreview the change, then run go test -run TestTheDeclaredDDLMatchesTheGoldenFile -update", goldenDDL, i+1, want, have)
		}
	}
}

func TestEveryDeclaredStatementHasItsOwnName(t *testing.T) {
	seen := map[string]string{}
	for _, section := range declaredDDL() {
		for _, s := range section.statements {
			if other, taken := seen[s.name]; taken {
				t.Errorf("%s is declared in both %s and %s", s.name, other, section.title)
			}
			seen[s.name] = section.title
		}
	}
}

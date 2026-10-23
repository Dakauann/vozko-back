package testenv_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var bypass = regexp.MustCompile(`(?:Getenv|LookupEnv)\(\s*"(?:DB_|REDIS_)|"(?:VOZKO_TEST_DB|VOZKO_TEST_REDIS[A-Z_]*|DB_(?:HOST|PORT|USER|PASSWORD|NAME)|REDIS_ADDR|REDIS_PASSWORD)"|\bNew(?:Gorm|Migration)Database\(`)

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

func isTestCode(t *testing.T, path string, source []byte) bool {
	t.Helper()
	if strings.HasSuffix(path, "_test.go") {
		return true
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, source, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, spec := range file.Imports {
		if imported, err := strconv.Unquote(spec.Path.Value); err == nil && imported == "testing" {
			return true
		}
	}
	return false
}

func bypassingLines(source string) []int {
	var lines []int
	for i, line := range strings.Split(source, "\n") {
		if bypass.MatchString(line) {
			lines = append(lines, i+1)
		}
	}
	return lines
}

func TestBypassPatternCatchesEveryDirectRead(t *testing.T) {
	for _, line := range []string{
		`host := os.Getenv("DB_HOST")`,
		`name, _ := os.LookupEnv("DB_NAME")`,
		`os.Getenv("DB_SSLMODE")`,
		`if os.Getenv("VOZKO_TEST_DB") != "1" {`,
		`addr := os.Getenv("VOZKO_TEST_REDIS")`,
		`password := os.Getenv("VOZKO_TEST_REDIS_PASSWORD")`,
		`const gate = "VOZKO_TEST_DB"`,
		`keys := []string{"DB_HOST", "DB_PORT"}`,
		`app := os.Getenv("REDIS_ADDR")`,
		`db, err := database.NewGormDatabase()`,
		`db, err := NewMigrationDatabase()`,
	} {
		if len(bypassingLines(line)) != 1 {
			t.Errorf("the bypass check misses %s", line)
		}
	}
	for _, line := range []string{
		`target := testenv.Postgres(t)`,
		`redis := testenv.Redis(t)`,
		`dsn := target.DSN() + " search_path=" + schemaName`,
		`t.Setenv("OPENROUTER_API_KEY", key)`,
	} {
		if len(bypassingLines(line)) != 0 {
			t.Errorf("the bypass check flags %s", line)
		}
	}
}

func TestNoTestCodeReachesPostgresOrRedisAroundTheGuard(t *testing.T) {
	root := moduleRoot(t)
	guard, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	var offenders []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if path == guard || (path != root && (strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata" || name == "node_modules")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !isTestCode(t, path, source) {
			return nil
		}
		relative, _ := filepath.Rel(root, path)
		for _, line := range bypassingLines(string(source)) {
			offenders = append(offenders, filepath.ToSlash(relative)+":"+strconv.Itoa(line))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Fatalf("test code reads the database or Redis settings directly; take them from vozko/infra/testenv (testenv.Postgres(t).DSN(), testenv.Redis(t)) so a non-test target fails before connecting:\n%s", strings.Join(offenders, "\n"))
	}
}

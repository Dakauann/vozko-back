package codegolden

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recorder struct {
	testing.TB
	failures []string
}

func (r *recorder) Errorf(format string, args ...any) {
	r.failures = append(r.failures, format)
}

func (r *recorder) Fatalf(format string, args ...any) {
	r.failures = append(r.failures, format)
}

func (r *recorder) Helper() {}

func writeGolden(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codes.golden")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCompare(t *testing.T) {
	tests := []struct {
		name     string
		golden   string
		codes    []string
		failures int
	}{
		{"the same codes in any order and line ending pass", "a_code\r\nb_code\n", []string{"b_code", "a_code", "a_code"}, 0},
		{"a code only in the source fails so the front learns about it", "a_code\n", []string{"a_code", "b_code"}, 1},
		{"a code only in the golden fails so stale copy is removed", "a_code\nb_code\n", []string{"a_code"}, 1},
		{"an unsorted golden fails so diffs stay readable", "b_code\na_code\n", []string{"a_code", "b_code"}, 1},
		{"an empty source fails since the scan found nothing to guard", "", nil, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &recorder{TB: t}
			Compare(r, writeGolden(t, tc.golden), tc.codes, "fix it")
			if len(r.failures) != tc.failures {
				t.Fatalf("failures = %v, want %d", r.failures, tc.failures)
			}
		})
	}
}

func TestCompareNamesTheAdviceWhenTheGoldenIsMissing(t *testing.T) {
	r := &recorder{TB: t}
	Compare(r, filepath.Join(t.TempDir(), "missing.golden"), []string{"a_code"}, "fix it")
	if len(r.failures) != 1 || !strings.Contains(r.failures[0], "%s") {
		t.Fatalf("failures = %v", r.failures)
	}
}

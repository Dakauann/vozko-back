package codegolden

import (
	"os"
	"sort"
	"strings"
	"testing"
)

func Read(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var codes []string
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			codes = append(codes, line)
		}
	}
	return codes, nil
}

func Unique(codes []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(codes))
	for _, code := range codes {
		if !seen[code] {
			seen[code] = true
			out = append(out, code)
		}
	}
	sort.Strings(out)
	return out
}

func Compare(t testing.TB, golden string, codes []string, advice string) {
	t.Helper()
	declared := Unique(codes)
	if len(declared) == 0 {
		t.Errorf("no codes found to compare with %s", golden)
		return
	}
	listed, err := Read(golden)
	if err != nil {
		t.Fatalf("read %s: %v; %s", golden, err, advice)
		return
	}
	inGolden := map[string]bool{}
	for _, code := range listed {
		inGolden[code] = true
	}
	inSource := map[string]bool{}
	for _, code := range declared {
		inSource[code] = true
		if !inGolden[code] {
			t.Errorf("code %s was added: %s", code, advice)
		}
	}
	for _, code := range listed {
		if !inSource[code] {
			t.Errorf("code %s was removed: %s", code, advice)
		}
	}
	if !sort.StringsAreSorted(listed) {
		t.Errorf("%s must stay sorted", golden)
	}
}

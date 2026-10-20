package brand

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const portugueseArticles = `o|a|os|as|do|da|dos|das|no|na|nos|nas|ao|à|aos|às|pelo|pela|um|uma|num|numa`

var fixedArticleBeforeBrand = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(?:` + portugueseArticles + `)\s*"\s*\+\s*brand\.Active\(\)\.Name\b`),
	regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(?:` + portugueseArticles + `)\s+\{\{-?\s*\.Brand\.Name\s*-?\}\}`),
	regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(?:` + portugueseArticles + `)\s+%s"\s+\.Brand\.Name\b`),
	regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(?:` + portugueseArticles + `)\s+Vozko\b`),
}

var skippedDirs = map[string]bool{".git": true, "docs": true, "third_party": true, ".testbin": true, "node_modules": true}

func shippedText(path string) bool {
	if strings.HasSuffix(path, "_test.go") {
		return false
	}
	switch filepath.Ext(path) {
	case ".go", ".html", ".tmpl", ".js":
		return true
	case ".md":
		return filepath.Base(path) == "SKILL.md"
	}
	return false
}

func mentionsTheBrand(line string) bool {
	return strings.Contains(line, "Brand.Name") || strings.Contains(line, "brand.Active().Name") || strings.Contains(strings.ToLower(line), "vozko")
}

func TestNoShippedTextTypesAnArticleBeforeTheBrand(t *testing.T) {
	var offenders []string
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skippedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !shippedText(path) {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for number, line := range strings.Split(string(raw), "\n") {
			if !mentionsTheBrand(line) {
				continue
			}
			for _, pattern := range fixedArticleBeforeBrand {
				if found := pattern.FindString(line); found != "" {
					offenders = append(offenders, path+":"+strconv.Itoa(number+1)+": "+strings.TrimSpace(found))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Fatalf("an article typed before the brand ignores BRAND_GENDER; use brand.Active().Portuguese() or {{.Brand.Portuguese.*}}:\n%s", strings.Join(offenders, "\n"))
	}
}


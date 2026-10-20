package notification_service

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"vozko/brand"
)

var fixedArticleBeforeTemplateBrand = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(?:o|a|os|as|do|da|dos|das|no|na|nos|nas|ao|à|aos|às|pelo|pela|um|uma)\s+(?:\{\{-?\s*\.Brand\.Name\s*-?\}\}|%s"\s+\.Brand\.Name)`)

func TestNoTemplateTypesAnArticleBeforeTheBrandName(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("templates", "*.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if found := fixedArticleBeforeTemplateBrand.FindString(string(raw)); found != "" {
			t.Errorf("%s types %q: the article must come from .Brand.Portuguese so BRAND_GENDER decides it", file, strings.TrimSpace(found))
		}
	}
}

func TestEveryTemplate_AgreesWithTheConfiguredBrandGender(t *testing.T) {
	t.Cleanup(func() { brand.SetForTest(brand.Brand{}) })
	wrongForms := map[brand.Gender][]string{
		brand.Feminine:  {"o Acme", "do Acme", "no Acme", "ao Acme", "pelo Acme", "um Acme"},
		brand.Masculine: {"a Acme", "da Acme", "na Acme", "à Acme", "pela Acme", "uma Acme"},
	}
	loader := loaderForTests()
	for gender, wrong := range wrongForms {
		brand.SetForTest(brand.Brand{
			Name: "Acme", LegalName: "Acme Comercio LTDA", CNPJ: "11.222.333/0001-44",
			SiteURL: "https://acme.example", EmailDomain: "acme.example",
			SupportEmail: "ajuda@acme.example", LogoURL: "https://cdn.acme.example/logo.png",
			Gender: gender,
		})
		for _, c := range everyTemplateWithItsSenderData() {
			out, err := loader.LoadTemplate(c.name, c.data)
			if err != nil {
				t.Fatalf("%s: render error: %v", c.name, err)
			}
			for _, form := range wrong {
				if regexp.MustCompile(`(?:^|[^\p{L}])` + regexp.QuoteMeta(form) + `\b`).MatchString(out) {
					t.Errorf("%s with a %s brand says %q", c.name, gender, form)
				}
			}
		}
	}
}

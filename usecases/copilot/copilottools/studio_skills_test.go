package copilottools

import (
	"regexp"
	"testing"

	"vozko/domain/tools"
	"vozko/usecases/copilot/skills"
)

var snakeWord = regexp.MustCompile(`\b[a-z]+(?:_[a-z]+)+\b`)

func studioVocabulary() map[string]bool {
	known := map[string]bool{}
	var collect func(params map[string]tools.Parameter)
	collect = func(params map[string]tools.Parameter) {
		for name, p := range params {
			known[name] = true
			for _, value := range p.Enum {
				known[value] = true
			}
			if p.Items != nil {
				collect(p.Items.Properties)
			}
		}
	}
	for _, tool := range StudioTools(StudioDeps{}) {
		def := tool.Definition()
		known[def.Name] = true
		collect(def.Parameters)
	}
	return known
}

func TestStudioSkillsOnlyNameRealToolsOperationsAndFields(t *testing.T) {
	library, err := skills.Load()
	if err != nil {
		t.Fatal(err)
	}
	known := studioVocabulary()
	for _, name := range []string{"edicao-de-video", "motion-design", "legendas-e-texto", "design-de-imagem", "formatos-e-zonas-seguras", "carrossel-e-miniatura", "anuncio-ugc-e-depoimento", "oferta-e-preco", "explicativo-e-dados", "pecas-por-segmento"} {
		skill, ok := library.Find(name)
		if !ok {
			t.Fatalf("the %s skill is missing", name)
		}
		for _, word := range snakeWord.FindAllString(skill.Description+" "+skill.Body, -1) {
			if !known[word] {
				t.Errorf("%s mentions %q, which no studio tool offers", name, word)
			}
		}
	}
}

package copilottools_test

import (
	"regexp"
	"strings"
	"testing"

	"vozko/domain/tools"
	"vozko/usecases/copilot/skills"
)

var skillSnakeWord = regexp.MustCompile(`\b[a-z]+(?:_[a-z]+)+\b`)

func registeredVocabulary(t *testing.T) map[string]bool {
	t.Helper()
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
	for _, tool := range registeredTools(t) {
		def := tool.Definition()
		known[def.Name] = true
		collect(def.Parameters)
	}
	return known
}

var adToolOutputKeys = map[string]bool{"can_spend": true}

var adCreativeSkillsTeach = map[string][]string{
	"formatos-e-zonas-seguras": {"ad_placement_look", "FLEXIBLE", "área segura", "restringir placements"},
	"criativos-que-performam":  {"ad_placement_look", "criativo-com-a-marca"},
	"criativo-com-a-marca":     {"ask_user", "studio_ask", "new_studio_project", "studio_export", "ad_placement_look", "update_ad_draft", "não pergunte"},
}

func TestTheAdCreativeSkillsTeachThePlacementCheckAndTheCreativePaths(t *testing.T) {
	library, err := skills.Load()
	if err != nil {
		t.Fatal(err)
	}
	known := registeredVocabulary(t)
	for name, words := range adCreativeSkillsTeach {
		skill, ok := library.Find(name)
		if !ok {
			t.Fatalf("the %s skill is missing", name)
		}
		for _, word := range words {
			if !strings.Contains(skill.Body, word) {
				t.Errorf("%s must teach %q", name, word)
			}
		}
		for _, word := range skillSnakeWord.FindAllString(skill.Description+" "+skill.Body, -1) {
			if !known[word] && !adToolOutputKeys[word] {
				t.Errorf("%s names %q, which no tool offers", name, word)
			}
		}
	}
}

func TestNoSkillOrToolTellsEloToAvoidTextInGeneratedImages(t *testing.T) {
	library, err := skills.Load()
	if err != nil {
		t.Fatal(err)
	}
	banned := []string{"erram letras", "evite pedir texto", "sem texto escrito", "o mínimo, ou nenhum"}
	for _, skill := range library.All() {
		for _, phrase := range banned {
			if strings.Contains(strings.ToLower(skill.Body), phrase) {
				t.Errorf("%s still discourages text in generated images (%q)", skill.Name, phrase)
			}
		}
	}
	for _, tool := range registeredTools(t) {
		def := tool.Definition()
		if def.Name != "generate_image" && def.Name != "studio_generate_image" {
			continue
		}
		if strings.Contains(def.Parameters["prompt"].Description, "evite") {
			t.Errorf("%s still tells Elo to avoid text in the image", def.Name)
		}
	}
}

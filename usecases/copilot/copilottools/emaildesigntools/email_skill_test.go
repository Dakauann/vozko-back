package emaildesigntools

import (
	"regexp"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/usecases/copilot/copilottools/platformtools"
	"vozko/usecases/copilot/skills"
)

const emailSkill = "design-de-email"

var snakeWord = regexp.MustCompile(`\b[a-z]+(?:_[a-z]+)+\b`)

var wordsOutsideSchemas = map[string]bool{"first_name": true, resultOpenGaps: true}

func emailVocabulary() map[string]bool {
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
	for _, tool := range append(EmailDesignTools(Deps{}), platformtools.NewAskUserTool()) {
		def := tool.Definition()
		known[def.Name] = true
		collect(def.Parameters)
	}
	return known
}

func TestTheEmailSkillNamesOnlyRealToolsAndFields(t *testing.T) {
	library, err := skills.Load()
	if err != nil {
		t.Fatal(err)
	}
	skill, ok := library.Find(emailSkill)
	if !ok {
		t.Fatalf("the %s skill is missing", emailSkill)
	}
	known := emailVocabulary()
	for _, word := range snakeWord.FindAllString(skill.Description+" "+skill.Body, -1) {
		if !known[word] && !wordsOutsideSchemas[word] {
			t.Errorf("%s mentions %q, which no email tool offers", emailSkill, word)
		}
	}
	for _, tool := range EmailDesignTools(Deps{}) {
		if name := tool.Definition().Name; !strings.Contains(skill.Body, name) {
			t.Errorf("%s never teaches %s", emailSkill, name)
		}
	}
	for _, view := range []copilot.View{designView, {}} {
		if !skill.OfferedOn(view) {
			t.Errorf("%s must be offered on %q", emailSkill, view.Surface)
		}
	}
	if skill.OfferedOn(copilot.View{Surface: copilot.SurfaceStudio, ProjectID: designID, ProjectKind: copilot.StudioImage}) {
		t.Errorf("%s stays out of the studio", emailSkill)
	}
}

func TestTheEmailSkillKeepsTextInGeneratedImagesAllowed(t *testing.T) {
	library, err := skills.Load()
	if err != nil {
		t.Fatal(err)
	}
	skill, _ := library.Find(emailSkill)
	if !strings.Contains(skill.Body, "Texto dentro de uma imagem gerada é permitido") {
		t.Fatal("generated images may carry text; the skill must never teach avoiding it")
	}
}

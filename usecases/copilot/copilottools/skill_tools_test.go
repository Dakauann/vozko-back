package copilottools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/copilot"
)

type skillShelf []copilot.Skill

func (s skillShelf) All() []copilot.Skill { return s }

func (s skillShelf) Find(name string) (copilot.Skill, bool) {
	for _, skill := range s {
		if skill.Name == name {
			return skill, true
		}
	}
	return copilot.Skill{}, false
}

var shelf = skillShelf{
	{Name: "publico-e-segmentacao", Description: "Como montar o público", Body: "Comece amplo."},
	{Name: "criativo-com-a-marca", Description: "Artes com a marca do cliente", Body: "A arte é do cliente."},
}

func TestTheSkillIndexIsTheToolDescription(t *testing.T) {
	def := NewLoadSkillTool(shelf).Definition()
	for _, s := range shelf {
		if !strings.Contains(def.Description, s.Name) || !strings.Contains(def.Description, s.Description) {
			t.Fatalf("description misses %s: %s", s.Name, def.Description)
		}
		if strings.Contains(def.Description, s.Body) {
			t.Fatal("the index must not carry the bodies")
		}
	}
	enum := def.Parameters["name"].Enum
	if len(enum) != len(shelf) || enum[0] != "publico-e-segmentacao" {
		t.Fatalf("enum %v", enum)
	}
}

func TestLoadingASkillReturnsOnlyItsBody(t *testing.T) {
	tool := NewLoadSkillTool(shelf)
	if tool.Meta().Mutating {
		t.Fatal("reading knowledge must not wait for approval")
	}
	result := tool.Execute(context.Background(), adContext, map[string]interface{}{"name": "criativo-com-a-marca"})
	data, _ := result.Data.(map[string]interface{})
	if result.Status != copilot.StatusOK || data["skill"] != "criativo-com-a-marca" || data["instructions"] != "A arte é do cliente." {
		t.Fatalf("result %+v", result)
	}
}

func TestAnUnknownSkillListsTheRealOnes(t *testing.T) {
	result := NewLoadSkillTool(shelf).Execute(context.Background(), adContext, map[string]interface{}{"name": "inventada"})
	if result.Status != copilot.StatusError || !strings.Contains(result.Message, "publico-e-segmentacao") {
		t.Fatalf("result %+v", result)
	}
}

func TestSkillsStayAvailableInFocusedSurfaces(t *testing.T) {
	studio := copilot.View{Surface: copilot.SurfaceStudio, ProjectID: "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f", ProjectKind: copilot.StudioVideo}
	if !studio.Offers(NewLoadSkillTool(nil)) {
		t.Fatal("the studio needs its skills")
	}
}

func TestLoadingASkillNamesItForThePerson(t *testing.T) {
	library := skillShelf{{Name: "motion-design", Description: "Animar", Body: "# Motion design no Estúdio\nUse easeOut."}}
	result := NewLoadSkillTool(library).Execute(context.Background(), adContext, map[string]interface{}{"name": "motion-design"})
	if result.Subject == nil || *result.Subject != (copilot.Subject{Kind: copilot.SubjectSkill, Key: "motion-design", Label: "Motion design no Estúdio"}) {
		t.Fatalf("subject %+v", result.Subject)
	}
}

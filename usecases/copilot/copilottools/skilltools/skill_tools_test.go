package skilltools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/usecases/copilot/copilottools/tooltest"
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
	enum := def.Parameters["names"].Items.Enum
	if len(enum) != len(shelf) || enum[0] != "publico-e-segmentacao" {
		t.Fatalf("enum %v", enum)
	}
}

func TestLoadingASkillReturnsOnlyItsBody(t *testing.T) {
	tool := NewLoadSkillTool(shelf)
	if tool.Meta().Mutating {
		t.Fatal("reading knowledge must not wait for approval")
	}
	result := tool.Execute(context.Background(), tooltest.AdContext, map[string]interface{}{"name": "criativo-com-a-marca"})
	data, _ := result.Data.(map[string]interface{})
	if result.Status != copilot.StatusOK || data["skill"] != "criativo-com-a-marca" || data["instructions"] != "A arte é do cliente." {
		t.Fatalf("result %+v", result)
	}
}

func TestAnUnknownSkillListsTheRealOnes(t *testing.T) {
	result := NewLoadSkillTool(shelf).Execute(context.Background(), tooltest.AdContext, map[string]interface{}{"names": []interface{}{"inventada"}})
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
	result := NewLoadSkillTool(library).Execute(context.Background(), tooltest.AdContext, map[string]interface{}{"names": []interface{}{"motion-design"}})
	if result.Subject == nil || *result.Subject != (copilot.Subject{Kind: copilot.SubjectSkill, Key: "motion-design", Label: "Motion design no Estúdio"}) {
		t.Fatalf("subject %+v", result.Subject)
	}
}

func TestEachSurfaceIndexesOnlyTheSkillsItOffers(t *testing.T) {
	scoped := skillShelf{
		{Name: "publico-e-segmentacao", Description: "Como montar o público", Body: "Comece amplo."},
		{Name: "design-de-imagem", Description: "Arte no editor", Body: "Margens.", Surface: copilot.SkillStudioOnly},
		{Name: "metricas-de-atendimento", Description: "Números do atendimento", Body: "Período.", Surface: copilot.SkillPanelOnly},
	}
	tool := NewLoadSkillTool(scoped)
	studio := copilot.View{Surface: copilot.SurfaceStudio, ProjectID: "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f", ProjectKind: copilot.StudioImage}
	onStudio := copilot.DefinitionOn(tool, studio)
	if strings.Join(onStudio.Parameters["names"].Items.Enum, ",") != "publico-e-segmentacao,design-de-imagem" || strings.Contains(onStudio.Description, "metricas-de-atendimento") {
		t.Fatalf("studio index = %v %s", onStudio.Parameters["names"].Items.Enum, onStudio.Description)
	}
	onPanel := copilot.DefinitionOn(tool, copilot.View{})
	if strings.Join(onPanel.Parameters["names"].Items.Enum, ",") != "publico-e-segmentacao,metricas-de-atendimento" {
		t.Fatalf("panel index = %v", onPanel.Parameters["names"].Items.Enum)
	}
	studioContext := copilot.Context{View: studio}
	if result := tool.Execute(context.Background(), studioContext, map[string]interface{}{"names": []interface{}{"metricas-de-atendimento"}}); result.Status != copilot.StatusError || strings.Contains(result.Message, "metricas-de-atendimento") {
		t.Fatalf("a skill the surface does not offer must be refused, got %+v", result)
	}
	if result := tool.Execute(context.Background(), studioContext, map[string]interface{}{"names": []interface{}{"design-de-imagem"}}); result.Status != copilot.StatusOK {
		t.Fatalf("result %+v", result)
	}
}

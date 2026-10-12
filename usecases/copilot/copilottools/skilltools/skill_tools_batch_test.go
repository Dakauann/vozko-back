package skilltools

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/usecases/copilot/copilottools/tooltest"
)

var titledShelf = skillShelf{
	{Name: "direcao-de-arte", Description: "Direção da peça", Body: "# Direção de arte\nUma ideia por peça."},
	{Name: "design-de-imagem", Description: "Arte no editor", Body: "# Design de imagem\nMargens generosas."},
	{Name: "motion-design", Description: "Animar", Body: "# Motion design\nUse easeOut."},
}

func TestSeveralSkillsLoadInOneCallEachWithItsOwnText(t *testing.T) {
	result := NewLoadSkillTool(titledShelf).Execute(context.Background(), tooltest.AdContext, map[string]interface{}{
		"names": []interface{}{"direcao-de-arte", "design-de-imagem"},
	})
	if result.Status != copilot.StatusOK {
		t.Fatalf("result %+v", result)
	}
	data, _ := result.Data.(map[string]interface{})
	loaded, _ := data["skills"].([]map[string]interface{})
	want := []map[string]interface{}{
		{"skill": "direcao-de-arte", "instructions": "# Direção de arte\nUma ideia por peça."},
		{"skill": "design-de-imagem", "instructions": "# Design de imagem\nMargens generosas."},
	}
	if !reflect.DeepEqual(loaded, want) {
		t.Fatalf("skills = %+v", data)
	}
	if result.Subject == nil || result.Subject.Kind != copilot.SubjectSkill || result.Subject.Key != "direcao-de-arte,design-de-imagem" || result.Subject.Label != "Direção de arte, Design de imagem" {
		t.Fatalf("subject %+v", result.Subject)
	}
}

func TestASkillAskedTwiceLoadsOnce(t *testing.T) {
	result := NewLoadSkillTool(titledShelf).Execute(context.Background(), tooltest.AdContext, map[string]interface{}{
		"names": []interface{}{"motion-design", "motion-design"},
	})
	data, _ := result.Data.(map[string]interface{})
	if result.Status != copilot.StatusOK || data["skill"] != "motion-design" {
		t.Fatalf("a repeated name is one skill, got %+v", result)
	}
}

func TestOneUnknownNameLoadsNothingAndListsTheRealOnes(t *testing.T) {
	result := NewLoadSkillTool(titledShelf).Execute(context.Background(), tooltest.AdContext, map[string]interface{}{
		"names": []interface{}{"direcao-de-arte", "inventada"},
	})
	if result.Status != copilot.StatusError || result.Data != nil || !strings.Contains(result.Message, "motion-design") {
		t.Fatalf("result %+v", result)
	}
}

func TestNoNameIsAnError(t *testing.T) {
	result := NewLoadSkillTool(titledShelf).Execute(context.Background(), tooltest.AdContext, map[string]interface{}{})
	if result.Status != copilot.StatusError {
		t.Fatalf("result %+v", result)
	}
}

func TestANameSentAsPlainTextStillLoads(t *testing.T) {
	result := NewLoadSkillTool(titledShelf).Execute(context.Background(), tooltest.AdContext, map[string]interface{}{"names": "motion-design"})
	data, _ := result.Data.(map[string]interface{})
	if result.Status != copilot.StatusOK || data["skill"] != "motion-design" {
		t.Fatalf("result %+v", result)
	}
}

func TestTheSchemaAsksForAListOfKnownSkills(t *testing.T) {
	def := NewLoadSkillTool(titledShelf).Definition()
	names, ok := def.Parameters["names"]
	if !ok || names.Type != "array" || names.Items == nil || names.Items.Type != "string" {
		t.Fatalf("parameters %+v", def.Parameters)
	}
	if strings.Join(names.Items.Enum, ",") != "direcao-de-arte,design-de-imagem,motion-design" {
		t.Fatalf("enum %v", names.Items.Enum)
	}
	if _, legacy := def.Parameters["name"]; legacy || len(def.Parameters) != 1 {
		t.Fatalf("one list parameter only, got %+v", def.Parameters)
	}
	if !reflect.DeepEqual(def.Required, []string{"names"}) {
		t.Fatalf("required %v", def.Required)
	}
}

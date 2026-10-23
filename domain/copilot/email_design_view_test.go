package copilot

import (
	"errors"
	"testing"
)

const openDesignID = "8d6f2b1a-3c4e-4f5a-9b7c-1d2e3f4a5b6c"

func TestTheEmailDesignViewNamesTheOpenDesign(t *testing.T) {
	view := View{Surface: SurfaceEmailDesign, ProjectID: openDesignID}
	if err := view.Validate(); err != nil {
		t.Fatal(err)
	}
	if !view.OnEmailDesign() || view.OnStudio() || !view.OnEditor() || !view.Focused() {
		t.Fatalf("got %+v", view)
	}
	for name, bad := range map[string]View{
		"no design":      {Surface: SurfaceEmailDesign},
		"invented id":    {Surface: SurfaceEmailDesign, ProjectID: "d1"},
		"a studio kind":  {Surface: SurfaceEmailDesign, ProjectID: openDesignID, ProjectKind: StudioImage},
		"a period":       {Surface: SurfaceEmailDesign, ProjectID: openDesignID, DateFrom: "2026-01-01"},
		"selected leads": {Surface: SurfaceEmailDesign, ProjectID: openDesignID, SelectedLeads: 2},
	} {
		if err := bad.Validate(); !errors.Is(err, ErrInvalidView) {
			t.Errorf("%s: want ErrInvalidView, got %v", name, err)
		}
	}
}

func TestEditorsAreTheStudioAndTheEmailDesign(t *testing.T) {
	if !(View{Surface: SurfaceStudio, ProjectID: openDesignID, ProjectKind: StudioImage}).OnEditor() {
		t.Fatal("the studio is an editor")
	}
	for _, view := range []View{{}, {Surface: SurfaceAttendance}, {Surface: SurfaceLeads}} {
		if view.OnEditor() || view.Focused() {
			t.Fatalf("%q is not an editor", view.Surface)
		}
	}
}

func TestPanelSkillsStayOutOfTheEmailEditor(t *testing.T) {
	email := View{Surface: SurfaceEmailDesign, ProjectID: openDesignID}
	if (Skill{Surface: SkillPanelOnly}).OfferedOn(email) || (Skill{Surface: SkillStudioOnly}).OfferedOn(email) || !(Skill{}).OfferedOn(email) {
		t.Fatal("the email editor offers only skills meant everywhere")
	}
}

func TestEmailSkillsServeTheEmailEditorAndThePanel(t *testing.T) {
	skill := Skill{Surface: SkillEmailWork}
	if !skill.OfferedOn(View{Surface: SurfaceEmailDesign, ProjectID: openDesignID}) || !skill.OfferedOn(View{}) || skill.OfferedOn(View{Surface: SurfaceStudio, ProjectID: openDesignID, ProjectKind: StudioImage}) {
		t.Fatal("email craft is for the email editor and the panel, never the studio")
	}
	if err := (Skill{Name: "x", Description: "d", Body: "b", Surface: SkillEmailWork}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTheAppReportsWhetherTheNewDesignOpened(t *testing.T) {
	opened, err := EmailDesignOpeningOf(openDesignID, []byte(`{"projectId":"`+openDesignID+`","kind":"email","opened":true}`))
	if err != nil {
		t.Fatal(err)
	}
	want := View{Surface: SurfaceEmailDesign, ProjectID: openDesignID}
	if opened.View != want || !opened.Opened || opened.Awaiting() != nil {
		t.Fatalf("got %+v", opened)
	}
	for name, raw := range map[string]string{
		"another design": `{"projectId":"5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f","kind":"email","opened":true}`,
		"a studio kind":  `{"projectId":"` + openDesignID + `","kind":"image","opened":true}`,
		"unknown field":  `{"projectId":"` + openDesignID + `","kind":"email","opened":true,"path":"/x"}`,
		"not json":       `aberto`,
	} {
		if _, err := EmailDesignOpeningOf(openDesignID, []byte(raw)); !errors.Is(err, ErrInvalidCreatedProject) {
			t.Errorf("%s: want ErrInvalidCreatedProject, got %v", name, err)
		}
	}
	waiting := WaitingEmailDesign(openDesignID)
	if waiting.Opened || waiting.Awaiting() == nil || *waiting.Awaiting() != want {
		t.Fatalf("got %+v", waiting)
	}
}

func TestOpeningADesignIsAClaimedAppCommand(t *testing.T) {
	if !ScreenOpenDesign.Known() || ScreenOpenDesign.NeedsProject() || !ScreenOpenDesign.Commits() {
		t.Fatal("opening a design runs once, on one tab, before any editor is attached")
	}
}

func TestTheEmailDesignCardLinksToTheEditor(t *testing.T) {
	card, err := NewEmailDesignCard(openDesignID, "Black Friday")
	if err != nil || card.Kind != ActionEmailDesign || card.EmailDesign.DesignID != openDesignID || card.EmailDesign.Name != "Black Friday" {
		t.Fatalf("got %+v, %v", card, err)
	}
	for name, id := range map[string]string{"invented": "d1", "empty": ""} {
		if _, err := NewEmailDesignCard(id, "x"); !errors.Is(err, ErrInvalidEmailDesignCard) {
			t.Errorf("%s: want ErrInvalidEmailDesignCard, got %v", name, err)
		}
	}
	if _, err := NewEmailDesignCard(openDesignID, "linha\nquebrada"); !errors.Is(err, ErrInvalidEmailDesignCard) {
		t.Fatalf("want ErrInvalidEmailDesignCard, got %v", err)
	}
}

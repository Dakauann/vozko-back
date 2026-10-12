package copilot

import (
	"errors"
	"testing"
)

const openedProjectID = "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f"

func TestTheAppReportsTheProjectItCreatedAndWhetherItsEditorOpened(t *testing.T) {
	order := StudioProjectOrder{Kind: "image"}
	opened, err := OpeningOf(order, []byte(`{"projectId":"`+openedProjectID+`","kind":"image","opened":true}`))
	if err != nil {
		t.Fatal(err)
	}
	want := View{Surface: SurfaceStudio, ProjectID: openedProjectID, ProjectKind: StudioImage}
	if opened.View != want || !opened.Opened || opened.Awaiting() != nil {
		t.Fatalf("an opened editor is the studio screen of that project, got %+v", opened)
	}
	waiting, err := OpeningOf(order, []byte(`{"projectId":"`+openedProjectID+`","kind":"image","opened":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Opened || waiting.Awaiting() == nil || *waiting.Awaiting() != want {
		t.Fatalf("a closed editor is awaited on that same screen, got %+v", waiting)
	}
}

func TestAnUnusableCreatedProjectIsRefused(t *testing.T) {
	order := StudioProjectOrder{Kind: "video"}
	for name, raw := range map[string]string{
		"not json":         `projeto`,
		"no id":            `{"kind":"video","opened":true}`,
		"invented id":      `{"projectId":"p1","kind":"video","opened":true}`,
		"another kind":     `{"projectId":"` + openedProjectID + `","kind":"image","opened":true}`,
		"unknown field":    `{"projectId":"` + openedProjectID + `","kind":"video","opened":true,"path":"/x"}`,
		"two values":       `{"projectId":"` + openedProjectID + `","kind":"video"} {}`,
		"empty":            ``,
		"opened as string": `{"projectId":"` + openedProjectID + `","kind":"video","opened":"yes"}`,
	} {
		if _, err := OpeningOf(order, []byte(raw)); !errors.Is(err, ErrInvalidCreatedProject) {
			t.Errorf("%s: must be refused, got %v", name, err)
		}
	}
}

func TestOnlyTheCreateCommandRunsWithoutAProject(t *testing.T) {
	if ScreenCreateProject.NeedsProject() {
		t.Fatal("creating a project is the one command that comes before any project")
	}
	for _, name := range ScreenCommandNames() {
		if name != ScreenCreateProject && !name.NeedsProject() {
			t.Errorf("%s acts on the open project and must need one", name)
		}
	}
	if !ScreenCreateProject.Known() {
		t.Fatal("the create command must be a known screen command")
	}
}

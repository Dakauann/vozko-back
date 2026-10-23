package emaildesigntools

import (
	"context"
	"encoding/json"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/emailcampaign"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools/tooltest"
)

type openingScreen struct {
	opened   bool
	err      error
	commands []copilot.ScreenCommand
}

func (s *openingScreen) Run(_ context.Context, cmd copilot.ScreenCommand) (copilot.ScreenReply, error) {
	s.commands = append(s.commands, cmd)
	if s.err != nil {
		return copilot.ScreenReply{}, s.err
	}
	order := cmd.Args.(designOpening)
	data, _ := json.Marshal(map[string]interface{}{"projectId": order.DesignID, "kind": "email", "opened": s.opened})
	return copilot.ScreenReply{OK: true, Data: data}, nil
}

func TestANewDesignStartsFromAStarterAndOpensInTheEditor(t *testing.T) {
	r := newRig()
	screen := &openingScreen{opened: true}
	res := NewNewEmailDesignTool(r.deps).Execute(context.Background(), tooltest.OwnerWithScreen(copilot.View{}, screen), args(t, `{"name":"Black Friday","starter":"promotion"}`))
	if res.Status != copilot.StatusOK {
		t.Fatalf("got %+v", res)
	}
	created := r.designs.created[0]
	if created.Name != "Black Friday" || created.Kind != emailcampaign.DesignTemplate || created.Document.Settings.Preheader == "" {
		t.Fatalf("got %+v", created)
	}
	if len(screen.commands) != 1 || screen.commands[0].Name != copilot.ScreenOpenDesign {
		t.Fatalf("the open tab is asked to open the editor, got %+v", screen.commands)
	}
	if res.Opening == nil || !res.Opening.Opened || res.Opening.View != designView {
		t.Fatalf("the turn moves to the new design, got %+v", res.Opening)
	}
	if res.Card == nil || res.Card.Kind != copilot.ActionEmailDesign || res.Card.EmailDesign.DesignID != designID {
		t.Fatalf("got %+v", res.Card)
	}
	if decoded(t, res.Data)["state"] != "opened" {
		t.Fatalf("got %+v", res.Data)
	}
}

func TestANewDesignWaitsWhenTheEditorStaysClosed(t *testing.T) {
	r := newRig()
	for name, screen := range map[string]copilot.Screen{
		"closed by preference": &openingScreen{opened: false},
		"no tab answered":      &openingScreen{err: copilot.ErrScreenUnavailable},
		"no screen":            nil,
	} {
		cc := tooltest.OwnerOn(copilot.View{})
		cc.Screen = screen
		res := NewNewEmailDesignTool(r.deps).Execute(context.Background(), cc, args(t, `{"name":"Boas-vindas"}`))
		if res.Status != copilot.StatusOK || res.Opening == nil || res.Opening.Opened || res.Opening.Awaiting() == nil || decoded(t, res.Data)["state"] != "waiting" {
			t.Errorf("%s: the design exists and the thread waits for it, got %+v", name, res)
		}
	}
}

func TestANewDesignRefusesAnUnknownStarterAndBadNames(t *testing.T) {
	r := newRig()
	for name, raw := range map[string]string{
		"starter": `{"name":"x","starter":"poster"}`,
		"name":    `{"name":"   "}`,
	} {
		if res := NewNewEmailDesignTool(r.deps).Execute(context.Background(), tooltest.OwnerOn(copilot.View{}), args(t, raw)); res.Status != copilot.StatusError {
			t.Errorf("%s: got %+v", name, res)
		}
	}
	if len(r.designs.created) != 0 {
		t.Fatal("nothing is created")
	}
}

func TestANewDesignNeedsTheEmailCreatePermission(t *testing.T) {
	meta := NewNewEmailDesignTool(Deps{}).Meta()
	if meta.Resource != workspace.ResourceEmailMarketing || meta.Action != workspace.ActionCreate {
		t.Fatalf("got %+v", meta)
	}
}

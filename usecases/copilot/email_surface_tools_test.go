package copilot_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/ai"
	"vozko/domain/aichat"
	"vozko/domain/copilot"
	"vozko/usecases/copilot/copilottools/emaildesigntools"
)

var campaignView = copilot.View{Surface: copilot.SurfaceEmailCampaign, ProjectID: "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f"}

var emailEditorTools = []string{"email_read", "email_edit", "email_look", "email_generate_image"}

func emailTurnInput(t *testing.T, view copilot.View, model string) ai.GenerateInput {
	t.Helper()
	prov := &scriptAI{turns: [][]ai.ToolCall{{}}, texts: []string{"Vejo o e-mail aberto."}, models: []ai.ModelInfo{toolModel(model)}}
	th := &fakeThreads{thread: &aichat.Thread{ID: "t1", WorkspaceID: "ws1", UserID: "u1", Model: model, ProjectID: view.ProjectID}}
	plain := &fakeTool{name: "list_agents", meta: readMeta}
	svc := newService(prov, th, &fakeMessages{}, append(emaildesigntools.EmailDesignTools(emaildesigntools.Deps{}), plain)...)
	cc := ownerCtx
	cc.View = view
	if err := svc.Stream(context.Background(), th.thread, copilot.UserMessage{Content: "consegue ver o template que estou visualizando?"}, cc, func(string, interface{}) {}); err != nil {
		t.Fatal(err)
	}
	if len(prov.inputs) == 0 {
		t.Fatal("the model was never called")
	}
	return prov.inputs[0]
}

func TestTheEmailEditorToolsAreOfferedOnBothEmailSurfacesForAnyModel(t *testing.T) {
	for _, view := range []copilot.View{emailView, campaignView} {
		for _, model := range []string{"deepseek/deepseek-v4.1-flash", "m"} {
			in := emailTurnInput(t, view, model)
			for _, name := range emailEditorTools {
				if !offers(in, name) {
					t.Fatalf("%s with %s: %s is missing from the offered tools", view.Surface, model, name)
				}
			}
			if offers(in, "list_agents") || offers(in, "new_email_design") {
				t.Fatalf("%s: a focused surface offers only its own tools", view.Surface)
			}
			if !strings.Contains(in.SystemPrompt, view.ProjectID) || !strings.Contains(in.SystemPrompt, "email_read: ele lê o e-mail aberto, sem id") {
				t.Fatalf("%s: the prompt names the open email and the tool that reads it", view.Surface)
			}
		}
	}
}

func TestTheCampaignPromptSaysWhatTheCampaignKeeps(t *testing.T) {
	prompt := emailTurnInput(t, campaignView, "m").SystemPrompt
	for _, want := range []string{"etapa Conteúdo", "subject", "um design salvo em Modelos não muda", "returned_to_draft"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("the campaign prompt lacks %q", want)
		}
	}
	if strings.Contains(prompt, "botão Salvar") {
		t.Fatal("a campaign saves itself; only a design waits for Salvar")
	}
	if back := editorBackNote(campaignView); !strings.Contains(back, "campanha") || !strings.Contains(back, "email_read") {
		t.Fatalf("got %q", back)
	}
}

func TestWithoutThePermissionTheEmailEditorToolsAreNotOffered(t *testing.T) {
	for _, view := range []copilot.View{emailView, campaignView} {
		cc := ownerCtx
		cc.View = view
		d := NewDriver(cc, "deepseek/deepseek-v4.1-flash", NewRegistry(emaildesigntools.EmailDesignTools(emaildesigntools.Deps{})...), &fakeAccess{err: errors.New("negado")}, openFunds{}, func() string { return "act-1" })
		if got := offeredNames(d); got != "" {
			t.Fatalf("%s: without email_marketing nothing is offered, got %s", view.Surface, got)
		}
	}
}

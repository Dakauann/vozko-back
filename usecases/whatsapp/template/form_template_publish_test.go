package template_usecase

import (
	"errors"
	"testing"

	"vozko/domain/whatsapp/template"
	wf "vozko/domain/whatsappform"
)

func formBody() template.TemplateComponent {
	return template.TemplateComponent{Type: "BODY", Text: "Responda o cadastro"}
}

func publishCases() []struct {
	name      string
	pubs      *fakePublications
	noPubs    bool
	workspace string
	want      error
} {
	other := sendablePublication()
	other.WorkspaceID = "ws-other"
	deprecated := sendablePublication()
	deprecated.Status = wf.PublicationDeprecated
	return []struct {
		name      string
		pubs      *fakePublications
		noPubs    bool
		workspace string
		want      error
	}{
		{name: "sendable publication of the workspace", pubs: &fakePublications{pub: sendablePublication()}, workspace: "ws-1"},
		{name: "form not published on the account", pubs: &fakePublications{err: wf.ErrPublicationNotFound}, workspace: "ws-1", want: template.ErrFlowNotPublished},
		{name: "form of another workspace", pubs: &fakePublications{pub: other}, workspace: "ws-1", want: template.ErrFlowNotPublished},
		{name: "form Meta deprecated", pubs: &fakePublications{pub: deprecated}, workspace: "ws-1", want: template.ErrFlowNotPublished},
		{name: "no creating workspace", pubs: &fakePublications{pub: sendablePublication()}, want: template.ErrFlowNotPublished},
		{name: "no publication reader", noPubs: true, workspace: "ws-1", want: template.ErrFlowNotPublished},
	}
}

func TestCreateTemplate_FormButtonMustOpenAFormPublishedOnTheAccount(t *testing.T) {
	for _, tc := range publishCases() {
		t.Run(tc.name, func(t *testing.T) {
			client := &createMockWAClient{}
			var pubs FlowPublications
			if !tc.noPubs {
				pubs = tc.pubs
			}
			uc := NewCreateTemplateUseCase(&sendMockClientFactory{client: client, wabaID: "waba-1"}, &sendMockTemplateRepo{}, &fakeSetHeaderMediaUC{}, anyStorage{}, pubs)
			in := baseCreateInput(formBody(), formButtons())
			in.Category = template.TemplateCategoryMarketing
			in.WorkspaceID = tc.workspace

			_, err := uc.Execute(in)

			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if sent := client.createInput != nil; sent != (tc.want == nil) {
				t.Fatalf("reached Meta = %v, want %v", sent, tc.want == nil)
			}
			if tc.pubs != nil && len(tc.pubs.got) > 0 && tc.pubs.got[0] != "waba-1|F1" {
				t.Fatalf("looked up %v, want the phone's account and the button's flow", tc.pubs.got)
			}
		})
	}
}

func TestCreateTemplate_WithoutAFormNeedsNoPublication(t *testing.T) {
	client := &createMockWAClient{}
	uc := NewCreateTemplateUseCase(&sendMockClientFactory{client: client, wabaID: "waba-1"}, &sendMockTemplateRepo{}, &fakeSetHeaderMediaUC{}, anyStorage{}, nil)

	if _, err := uc.Execute(baseCreateInput(positionalBodyComponent())); err != nil {
		t.Fatalf("create: %v", err)
	}
	if client.createInput == nil {
		t.Fatal("a template without a form must still reach Meta")
	}
}

func TestEditTemplate_FormButtonMustOpenAFormPublishedOnTheAccount(t *testing.T) {
	for _, tc := range publishCases() {
		if tc.workspace == "" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			rig := newEditRig()
			if !tc.noPubs {
				rig.deps.Forms = tc.pubs
			}

			_, err := rig.run(t, template.Edit{Components: []template.TemplateComponent{formBody(), formButtons()}})

			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if edited := len(rig.client.edits) == 1; edited != (tc.want == nil) {
				t.Fatalf("reached Meta = %v, want %v", edited, tc.want == nil)
			}
			if tc.pubs != nil && len(tc.pubs.got) > 0 && tc.pubs.got[0] != "waba-1|F1" {
				t.Fatalf("looked up %v, want the template's account and the button's flow", tc.pubs.got)
			}
		})
	}
}

func TestEditTemplate_WithoutAFormNeedsNoPublication(t *testing.T) {
	rig := newEditRig()

	if _, err := rig.run(t, bodyEdit("Oi de novo")); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if len(rig.client.edits) != 1 {
		t.Fatal("an edit without a form must still reach Meta")
	}
}

func TestReplicateTemplate_AFormTemplateIsNeverCopiedToAnotherAccount(t *testing.T) {
	client := &createMockWAClient{}
	source := approvedFormTemplate()
	uc := NewReplicateTemplateUseCase(&stubTemplateRepo{tmpl: source}, &sendMockClientFactory{client: client, wabaID: "waba-2"})

	_, err := uc.Execute(template.ReplicateTemplateInput{TemplateID: source.ID, TargetBusinessPhoneID: "phone-2"})

	if !errors.Is(err, template.ErrFlowNotPublished) {
		t.Fatalf("err = %v, want ErrFlowNotPublished: the form lives on the source account only", err)
	}
	if client.createInput != nil {
		t.Fatal("the copy must not reach Meta")
	}
}

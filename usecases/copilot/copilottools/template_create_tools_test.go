package copilottools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/media"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	tmpl "vozko/domain/whatsapp/template"
)

const (
	knownPhone  = "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	knownBanner = "2c3d4e5f-6a7b-4c8d-9e0f-1a2b3c4d5e6f"
)

type fakeWorkspacePhones struct {
	workspace string
	none      bool
}

func (f *fakeWorkspacePhones) List(workspaceID string, _ businessphone.ListInput) (*shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber], error) {
	f.workspace = workspaceID
	if f.none {
		return &shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber]{}, nil
	}
	return &shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber]{Items: []*businessphone.WhatsAppBusinessPhoneNumber{{ID: knownPhone, DisplayPhoneNumber: "+55 84 99440-9624", VerifiedName: "Loja"}}}, nil
}

type fakeTemplateCreator struct {
	grantedBy string
	workspace string
	input     tmpl.CreateTemplateInput
	err       error
}

func (f *fakeTemplateCreator) List(string, tmpl.ListInput) (*shared.PaginatedResult[*tmpl.Template], error) {
	return nil, nil
}
func (f *fakeTemplateCreator) Get(string, string) (*tmpl.Template, error) { return nil, nil }
func (f *fakeTemplateCreator) Create(workspaceID, grantedBy string, in tmpl.CreateTemplateInput) (*tmpl.CreateTemplateOutput, error) {
	f.workspace, f.grantedBy, f.input = workspaceID, grantedBy, in
	if f.err != nil {
		return nil, f.err
	}
	return &tmpl.CreateTemplateOutput{ID: "t-new", Name: in.Name, Status: tmpl.TemplateStatus("PENDING")}, nil
}

type fakeChatMedia map[string]*media.Media

func (f fakeChatMedia) GetMedia(workspaceID, id string) (*media.Media, error) {
	m, ok := f[id]
	if !ok || m.WorkspaceID != workspaceID {
		return nil, media.ErrMediaNotFound
	}
	return m, nil
}

var bannerMedia = fakeChatMedia{knownBanner: {ID: knownBanner, WorkspaceID: "ws-1", URL: "https://files.test/ws-1/banner.jpg", Type: media.MediaTypeProductImage}}

func templateCreateDeps(creator *fakeTemplateCreator) TemplateCreateDeps {
	return TemplateCreateDeps{Phones: &fakeWorkspacePhones{}, Templates: creator, Media: bannerMedia}
}

func TestTemplateCreationToolsCheckTheirPermissions(t *testing.T) {
	deps := templateCreateDeps(&fakeTemplateCreator{})
	if m := NewListBusinessPhonesTool(deps).Meta(); m.Resource != "business_phones" || m.Action != "read" || m.Mutating {
		t.Fatalf("phones meta = %+v", m)
	}
	if m := NewCreateTemplateTool(deps).Meta(); m.Resource != "whatsapp_templates" || m.Action != "create" || !m.Mutating {
		t.Fatalf("create meta = %+v", m)
	}
}

func TestCreateTemplateBuildsMetaComponentsAsTheUser(t *testing.T) {
	creator := &fakeTemplateCreator{}
	res := NewCreateTemplateTool(templateCreateDeps(creator)).Execute(context.Background(), member(), map[string]interface{}{
		"business_phone_id": knownPhone, "name": "Pedido_Enviado", "category": "utility",
		"header_media_id": knownBanner, "body": "Olá {{1}}, seu pedido {{2}} saiu.", "body_examples": []interface{}{"Maria", "123"},
		"footer": "Loja", "quick_replies": []interface{}{"Obrigado"},
	})
	if res.Status != copilot.StatusOK {
		t.Fatalf("status %s: %s", res.Status, res.Message)
	}
	in := creator.input
	if creator.workspace != "ws-1" || creator.grantedBy != "u-1" || in.Name != "pedido_enviado" || in.Language != "pt_BR" || in.Category != tmpl.TemplateCategoryUtility {
		t.Fatalf("input %+v by %q in %q", in, creator.grantedBy, creator.workspace)
	}
	if len(in.Components) != 4 || in.Components[0].Format != "IMAGE" || in.HeaderMediaURL == nil || *in.HeaderMediaURL != "https://files.test/ws-1/banner.jpg" {
		t.Fatalf("components %+v", in.Components)
	}
	if got := in.Components[1].Example.BodyText[0]; len(got) != 2 || got[0] != "Maria" {
		t.Fatalf("body examples %v", got)
	}
}

func TestCreateTemplateRefusesAnotherWorkspacesAttachment(t *testing.T) {
	creator := &fakeTemplateCreator{}
	cc := member()
	cc.WorkspaceID = "ws-2"
	res := NewCreateTemplateTool(templateCreateDeps(creator)).Execute(context.Background(), cc, map[string]interface{}{
		"business_phone_id": knownPhone, "name": "x", "category": "MARKETING", "header_media_id": knownBanner, "body": "oi",
	})
	if res.Status != copilot.StatusError || creator.input.Name != "" {
		t.Fatalf("status %s created %+v", res.Status, creator.input)
	}
}

func TestCreateTemplateRefusesAuthenticationAndExplainsMetaRules(t *testing.T) {
	creator := &fakeTemplateCreator{}
	if res := NewCreateTemplateTool(templateCreateDeps(creator)).Execute(context.Background(), member(), map[string]interface{}{
		"business_phone_id": knownPhone, "name": "x", "category": "AUTHENTICATION", "body": "oi",
	}); res.Status != copilot.StatusError {
		t.Fatalf("authentication allowed: %s", res.Status)
	}
	rules := &fakeTemplateCreator{err: tmpl.ErrBodyVariableAtStart}
	res := NewCreateTemplateTool(templateCreateDeps(rules)).Execute(context.Background(), member(), map[string]interface{}{
		"business_phone_id": knownPhone, "name": "x", "category": "MARKETING", "body": "{{1}} oi",
	})
	if res.Status != copilot.StatusError || res.Message == "falha" {
		t.Fatalf("result %+v", res)
	}
	denied := &fakeTemplateCreator{err: tmpl.ErrPhoneOutsideWorkspace}
	if res := NewCreateTemplateTool(templateCreateDeps(denied)).Execute(context.Background(), member(), map[string]interface{}{
		"business_phone_id": knownPhone, "name": "x", "category": "MARKETING", "body": "oi",
	}); res.Status != copilot.StatusDenied {
		t.Fatalf("foreign phone: %s", res.Status)
	}
}

func newTemplateArgs() map[string]interface{} {
	return map[string]interface{}{
		"business_phone_id": knownPhone, "name": "Refiliacao_Convite", "category": "MARKETING",
		"header_media_id": knownBanner, "body": "Olá {{1}}! Sua filiação pode voltar.", "body_examples": []interface{}{"Maria"},
		"footer": "Responda quando puder", "quick_replies": []interface{}{"Quero reativar", "Agora não"},
	}
}

func TestCreateTemplatePreviewIsWhatWillBeSentToMeta(t *testing.T) {
	tool := NewCreateTemplateTool(templateCreateDeps(&fakeTemplateCreator{})).(copilot.Previewer)
	p := tool.Preview(context.Background(), member(), newTemplateArgs())
	if p == nil || p.Kind != copilot.PreviewWhatsAppTemplate {
		t.Fatalf("preview = %+v", p)
	}
	data := p.Data.(TemplatePreview)
	if data.Name != "refiliacao_convite" || data.Language != "pt_BR" || data.Category != "MARKETING" || data.HeaderMediaURL != "https://files.test/ws-1/banner.jpg" {
		t.Fatalf("data = %+v", data)
	}
	kinds := make([]string, 0, len(data.Components))
	for _, c := range data.Components {
		kinds = append(kinds, c.Type)
	}
	if strings.Join(kinds, ",") != "HEADER,BODY,FOOTER,BUTTONS" || data.Components[1].Example.BodyText[0][0] != "Maria" {
		t.Fatalf("components = %+v", data.Components)
	}
}

func TestCreateTemplatePreviewNeverShowsAnotherWorkspacesMedia(t *testing.T) {
	cc := member()
	cc.WorkspaceID = "ws-2"
	p := NewCreateTemplateTool(templateCreateDeps(&fakeTemplateCreator{})).(copilot.Previewer).Preview(context.Background(), cc, newTemplateArgs())
	if p != nil {
		t.Fatalf("preview with a foreign header media = %+v", p)
	}
}

func TestCreateTemplateFieldsKeepOnlyWhatTheBubbleCannotShow(t *testing.T) {
	fields := NewCreateTemplateTool(templateCreateDeps(&fakeTemplateCreator{})).(copilot.Describer).Describe(context.Background(), member(), newTemplateArgs())
	keys := make([]string, 0, len(fields))
	for _, f := range fields {
		keys = append(keys, f.Key)
	}
	if strings.Join(keys, ",") != "name,category,language,phone" || fieldValue(fields, "phone") != "+55 84 99440-9624" {
		t.Fatalf("fields = %+v", fields)
	}
}

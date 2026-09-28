package copilottools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/campaign"
	"vozko/domain/conversation"
	"vozko/domain/copilot"
	"vozko/domain/media"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	wd "vozko/domain/workspace/workspace_department"
)

const (
	knownQRNumber   = "4d5e6f70-8192-4a3b-9c4d-5e6f7a8b9c0d"
	knownQRCampaign = "5e6f7081-92a3-4b4c-8d5e-6f7a8b9c0d1e"
	knownQRImage    = "6f708192-a3b4-4c5d-9e6f-7a8b9c0d1e2f"
)

type fakeScopes struct{ denied bool }

func (f fakeScopes) GetDepartmentScope(string, string, bool) (conversation.DepartmentAccessScope, bool) {
	return conversation.DepartmentAccessScope{DepartmentIDs: []string{"d-sales"}, Restrict: true}, !f.denied
}

type fakeQRNumbers struct{ in uw.ListInstancesInput }

func (f *fakeQRNumbers) Execute(_ context.Context, in uw.ListInstancesInput) (*shared.PaginatedResult[*uw.Instance], error) {
	f.in = in
	return &shared.PaginatedResult[*uw.Instance]{Items: []*uw.Instance{
		{ID: knownQRNumber, DisplayName: "Loja Centro", PhoneNumber: "5584994409624", Status: uw.StatusConnected},
	}}, nil
}

type fakeUsable map[string]*uw.Instance

func (f fakeUsable) Usable(_ context.Context, workspaceID string, scope uw.DepartmentScope, id string) (*uw.Instance, error) {
	i, ok := f[id]
	if !ok || workspaceID != "ws-1" || !scope.Restrict {
		return nil, uw.ErrInstanceNotFound
	}
	if i.Status == uw.StatusBanned {
		return nil, uwc.NewInstanceUnusableError(i.Label(), "WhatsApp disabled this number")
	}
	return i, nil
}

var qrInstances = fakeUsable{knownQRNumber: {ID: knownQRNumber, DisplayName: "Loja Centro", Status: uw.StatusConnected}}

type fakeQRPreview struct {
	req   uwc.ImportRequest
	empty bool
}

func (f *fakeQRPreview) Preview(_ context.Context, req uwc.ImportRequest) (*uwc.ImportPreview, error) {
	f.req = req
	if req.MediaID != knownSheet || req.WorkspaceID != "ws-1" {
		return nil, media.ErrMediaNotFound
	}
	if f.empty {
		return &uwc.ImportPreview{ImportResult: campaign.ImportResult{TotalRows: 3}}, nil
	}
	return &uwc.ImportPreview{ImportResult: campaign.ImportResult{Variables: 1, TotalRows: 3, ValidRows: 2,
		Rows: []campaign.ImportRow{{Number: "5584994409624", Variables: []string{"Maria"}}, {Number: "5584994409625", Variables: []string{"João"}}}}}, nil
}

type fakeQRCreate struct {
	got      *uwc.Campaign
	scope    uw.DepartmentScope
	creation wd.CreationScope
}

func (f *fakeQRCreate) Execute(ctx context.Context, c *uwc.Campaign, scope uw.DepartmentScope) (*uwc.Campaign, error) {
	f.got, f.scope = c, scope
	f.creation, _ = wd.GetCreationScope(ctx)
	return &uwc.Campaign{ID: knownQRCampaign, Name: c.Name, Metrics: &campaign.Metrics{TotalNumbers: 2}}, nil
}

type fakeQRAccess map[string]*uwc.Campaign

func (f fakeQRAccess) Owned(_ context.Context, workspaceID string, _ uw.DepartmentScope, id string) (*uwc.Campaign, error) {
	c, ok := f[id]
	if !ok || workspaceID != "ws-1" {
		return nil, uwc.ErrCampaignNotFound
	}
	return c, nil
}

type fakeQRActions struct {
	action campaign.Action
	scope  uw.DepartmentScope
}

func (f *fakeQRActions) Act(_ context.Context, _ string, scope uw.DepartmentScope, id string, action campaign.Action) (*uwc.Campaign, error) {
	f.action, f.scope = action, scope
	return &uwc.Campaign{ID: id, Name: "Reativação"}, nil
}

func readyQRCampaign() *uwc.Campaign {
	return &uwc.Campaign{ID: knownQRCampaign, Name: "Reativação", InstanceID: knownQRNumber, InstanceLabel: "Loja Centro",
		Status: campaign.StatusStopped, SendDelayMinMS: 8000, SendDelayMaxMS: 15000, DailyCap: 200,
		Metrics: &campaign.Metrics{TotalNumbers: 40, Pending: 40}}
}

type qrFixture struct {
	deps    UnofficialCampaignDeps
	numbers *fakeQRNumbers
	preview *fakeQRPreview
	create  *fakeQRCreate
	actions *fakeQRActions
}

func newQRFixture() qrFixture {
	f := qrFixture{numbers: &fakeQRNumbers{}, preview: &fakeQRPreview{}, create: &fakeQRCreate{}, actions: &fakeQRActions{}}
	f.deps = UnofficialCampaignDeps{
		Scopes: fakeScopes{}, Numbers: f.numbers, Usable: qrInstances, Preview: f.preview, Create: f.create,
		Access: fakeQRAccess{knownQRCampaign: readyQRCampaign()}, Actions: f.actions,
		Media: fakeChatMedia{knownQRImage: {ID: knownQRImage, WorkspaceID: "ws-1", URL: "https://files.test/ws-1/promo.jpg", Type: media.MediaTypeProductImage}},
	}
	return f
}

func qrCreateArgs(changes map[string]interface{}) map[string]interface{} {
	return withArgs(map[string]interface{}{
		"media_id": knownSheet, "message": "Oi {{1}}, sentimos sua falta!", "name": "Reativação", "number_id": knownQRNumber,
	}, changes)
}

func TestUnofficialCampaignToolsDeclareTheirPermissions(t *testing.T) {
	deps := newQRFixture().deps
	cases := []struct {
		tool                   copilot.Tool
		resource, action       string
		mutating, hasValidator bool
	}{
		{NewListUnofficialNumbersTool(deps), "unofficial_whatsapp_instances", "read", false, false},
		{NewPreviewUnofficialImportTool(deps), "unofficial_whatsapp_campaigns", "create", false, false},
		{NewCreateUnofficialCampaignTool(deps), "unofficial_whatsapp_campaigns", "create", true, true},
		{NewStartUnofficialCampaignTool(deps), "unofficial_whatsapp_campaigns", "start", true, true},
	}
	for _, c := range cases {
		m := c.tool.Meta()
		_, validates := c.tool.(copilot.Validator)
		if string(m.Resource) != c.resource || string(m.Action) != c.action || m.Mutating != c.mutating || validates != c.hasValidator {
			t.Fatalf("%s meta = %+v validates %v", c.tool.Definition().Name, m, validates)
		}
	}
}

func TestListUnofficialNumbersUsesTheUsersDepartmentScope(t *testing.T) {
	f := newQRFixture()
	res := NewListUnofficialNumbersTool(f.deps).Execute(context.Background(), member(), nil)
	if res.Status != copilot.StatusOK || !f.numbers.in.Scope.Restrict || f.numbers.in.WorkspaceID != "ws-1" {
		t.Fatalf("result %+v input %+v", res, f.numbers.in)
	}
	numbers := res.Data.(map[string]interface{})["numbers"].([]map[string]interface{})
	if len(numbers) != 1 || numbers[0]["number_id"] != knownQRNumber || numbers[0]["can_send_now"] != true {
		t.Fatalf("numbers = %+v", numbers)
	}
	f.deps.Scopes = fakeScopes{denied: true}
	if res := NewListUnofficialNumbersTool(f.deps).Execute(context.Background(), member(), nil); res.Status == copilot.StatusOK {
		t.Fatal("a user without access listed the numbers")
	}
}

func TestPreviewUnofficialImportBuildsTheMessageFromTextAndAttachment(t *testing.T) {
	f := newQRFixture()
	res := NewPreviewUnofficialImportTool(f.deps).Execute(context.Background(), member(), map[string]interface{}{
		"media_id": knownSheet, "message": "Oi {{1}}!", "attachment_media_id": knownQRImage,
	})
	if res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	msg := f.preview.req.Message
	if msg.Kind != uwc.KindImage || msg.MediaID != knownQRImage || msg.FileName != "promo.jpg" || msg.Bodies[0] != "Oi {{1}}!" {
		t.Fatalf("message = %+v", msg)
	}
	if sample := res.Data.(map[string]interface{})["sample"].([]string); sample[0] != "Oi Maria!" {
		t.Fatalf("sample = %v", sample)
	}
	cc := member()
	cc.WorkspaceID = "ws-2"
	if res := NewPreviewUnofficialImportTool(f.deps).Execute(context.Background(), cc, map[string]interface{}{"media_id": knownSheet, "message": "Oi", "attachment_media_id": knownQRImage}); res.Status == copilot.StatusOK {
		t.Fatal("another workspace's attachment was used")
	}
}

func TestCreateUnofficialCampaignPreflight(t *testing.T) {
	f := newQRFixture()
	tool := NewCreateUnofficialCampaignTool(f.deps)
	if err := validate(t, tool, member(), qrCreateArgs(nil)); err != nil {
		t.Fatalf("valid campaign refused: %v", err)
	}
	cases := map[string]map[string]interface{}{
		"invented number": {"number_id": "loja-centro"},
		"foreign number":  {"number_id": "7a8b9c0d-1e2f-4a3b-8c4d-5e6f7a8b9c0d"},
		"foreign sheet":   {"media_id": "8b9c0d1e-2f3a-4b4c-9d5e-6f7a8b9c0d1e"},
	}
	for label, changes := range cases {
		if err := validate(t, tool, member(), qrCreateArgs(changes)); !errors.Is(err, errInvalidArgs) {
			t.Errorf("%s: %v", label, err)
		}
	}
	f.preview.empty = true
	if err := validate(t, tool, member(), qrCreateArgs(nil)); err == nil || !strings.Contains(err.Error(), "nenhuma linha válida") {
		t.Fatalf("empty sheet = %v", err)
	}
	f.deps.Usable = fakeUsable{knownQRNumber: {ID: knownQRNumber, DisplayName: "Loja Centro", Status: uw.StatusBanned}}
	if err := validate(t, NewCreateUnofficialCampaignTool(f.deps), member(), qrCreateArgs(nil)); err == nil || !strings.Contains(err.Error(), "não pode fazer campanhas") {
		t.Fatalf("banned number = %v", err)
	}
}

func TestCreateUnofficialCampaignUsesTheRowsReadByTheServer(t *testing.T) {
	f := newQRFixture()
	cc := member()
	res := NewCreateUnofficialCampaignTool(f.deps).Execute(creating(cc), cc, qrCreateArgs(map[string]interface{}{
		"targets": []interface{}{"5500000000000"}, "workspace_id": "ws-2", "department_id": knownDepartment,
	}))
	if res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	got := f.create.got
	if got.WorkspaceID != "ws-1" || got.InstanceID != knownQRNumber || got.CreatedByID != "u-1" || len(got.Targets) != 2 || got.Targets[0].Variables[0] != "Maria" {
		t.Fatalf("campaign = %+v", got)
	}
	if !f.create.scope.Restrict || f.create.creation.RequestedDepartmentID != knownDepartment {
		t.Fatalf("scope %+v creation %+v", f.create.scope, f.create.creation)
	}
}

func TestCreateUnofficialCampaignShowsTheFirstMessage(t *testing.T) {
	f := newQRFixture()
	tool := NewCreateUnofficialCampaignTool(f.deps)
	p := tool.(copilot.Previewer).Preview(context.Background(), member(), qrCreateArgs(nil))
	data, ok := p.Data.(MessagePreview)
	if p.Kind != copilot.PreviewMessage || !ok || data.Text != "Oi Maria, sentimos sua falta!" || data.Channel != "unofficial_whatsapp" {
		t.Fatalf("preview = %+v", p)
	}
	fields := tool.(copilot.Describer).Describe(context.Background(), member(), qrCreateArgs(nil))
	if fieldValue(fields, "phone") != "Loja Centro" || fieldValue(fields, "contacts") != "2 de 3 linhas" {
		t.Fatalf("fields = %+v", fields)
	}
}

func TestStartUnofficialCampaignPreflight(t *testing.T) {
	f := newQRFixture()
	args := map[string]interface{}{"campaign_id": knownQRCampaign}
	if err := validate(t, NewStartUnofficialCampaignTool(f.deps), member(), args); err != nil {
		t.Fatalf("ready campaign refused: %v", err)
	}
	running := readyQRCampaign()
	running.Status = campaign.StatusRunning
	done := readyQRCampaign()
	done.Metrics = &campaign.Metrics{TotalNumbers: 2, Processed: 2}
	cases := map[string]UnofficialCampaignDeps{}
	for label, c := range map[string]*uwc.Campaign{"running": running, "all sent": done} {
		deps := f.deps
		deps.Access = fakeQRAccess{knownQRCampaign: c}
		cases[label] = deps
	}
	offline := f.deps
	offline.Usable = fakeUsable{knownQRNumber: {ID: knownQRNumber, Status: uw.StatusDisconnected}}
	cases["disconnected"] = offline
	for label, deps := range cases {
		if err := validate(t, NewStartUnofficialCampaignTool(deps), member(), args); !errors.Is(err, errInvalidArgs) {
			t.Errorf("%s: %v", label, err)
		}
	}
	cc := member()
	cc.WorkspaceID = "ws-2"
	if err := validate(t, NewStartUnofficialCampaignTool(f.deps), cc, args); err == nil {
		t.Fatal("a foreign campaign passed preflight")
	}
}

func TestStartUnofficialCampaignShowsThePaceAndActsInScope(t *testing.T) {
	f := newQRFixture()
	tool := NewStartUnofficialCampaignTool(f.deps)
	fields := tool.(copilot.Describer).Describe(context.Background(), member(), map[string]interface{}{"campaign_id": knownQRCampaign})
	if fieldValue(fields, "contacts") != "40" || fieldValue(fields, "pace") != "uma mensagem a cada 8 a 15 segundos" || fieldValue(fields, "dailyCap") != "até 200 por dia" {
		t.Fatalf("fields = %+v", fields)
	}
	res := tool.Execute(context.Background(), member(), map[string]interface{}{"campaign_id": knownQRCampaign})
	if res.Status != copilot.StatusOK || f.actions.action != campaign.ActionStart || !f.actions.scope.Restrict {
		t.Fatalf("result %+v actions %+v", res, f.actions)
	}
}

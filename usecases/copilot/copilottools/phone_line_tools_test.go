package copilottools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/sip_trunk"
	sip_trunk_usecase "vozko/usecases/sip_trunk"
)

const lineUUID = "7a1d3c2b-4e5f-4a6b-8c7d-9e0f1a2b3c4d"

type phoneLineBook struct {
	lines   map[string]*sip_trunk.SIPTrunk
	created sip_trunk_usecase.CreateTrunkInput
	updated sip_trunk_usecase.UpdateTrunkInput
	deleted string
	outcome sip_trunk.RegistrationStatus
}

func newPhoneLineBook() *phoneLineBook {
	return &phoneLineBook{lines: map[string]*sip_trunk.SIPTrunk{lineUUID: {
		ID: lineUUID, WorkspaceID: "ws-1", Name: "Principal", TrunkType: sip_trunk.TrunkTypeBidirectional,
		Host: "sip.operadora.com", Transport: sip_trunk.TransportUDP, Username: "1001", Password: "segredo-antigo",
		Enabled: true, RegistrationStatus: sip_trunk.RegistrationStatusRegistered,
	}}, outcome: sip_trunk.RegistrationStatusRegistered}
}

func (b *phoneLineBook) deps() PhoneLineDeps {
	return PhoneLineDeps{List: lineLister{b}, Get: lineGetter{b}, Create: lineCreator{b}, Update: lineUpdater{b}, Delete: lineDeleter{b}}
}

type lineLister struct{ b *phoneLineBook }

func (l lineLister) Execute(_ context.Context, workspaceID string) ([]*sip_trunk.SIPTrunk, error) {
	var out []*sip_trunk.SIPTrunk
	for _, line := range l.b.lines {
		if line.WorkspaceID == workspaceID {
			out = append(out, line)
		}
	}
	return out, nil
}

type lineGetter struct{ b *phoneLineBook }

func (g lineGetter) Execute(_ context.Context, workspaceID, id string) (*sip_trunk.SIPTrunk, error) {
	line, ok := g.b.lines[id]
	if !ok || line.WorkspaceID != workspaceID {
		return nil, sip_trunk.ErrTrunkNotFound
	}
	clone := *line
	return &clone, nil
}

type lineCreator struct{ b *phoneLineBook }

func (c lineCreator) Execute(_ context.Context, input sip_trunk_usecase.CreateTrunkInput) (*sip_trunk.SIPTrunk, error) {
	c.b.created = input
	line := sip_trunk_usecase.DraftTrunk(input)
	if err := line.Validate(); err != nil {
		return nil, err
	}
	line.ID = "new-line"
	line.RegistrationStatus = c.b.outcome
	if c.b.outcome == sip_trunk.RegistrationStatusFailed {
		line.LastError = "401 Unauthorized"
	}
	return line, nil
}

type lineUpdater struct{ b *phoneLineBook }

func (u lineUpdater) Execute(ctx context.Context, input sip_trunk_usecase.UpdateTrunkInput) (*sip_trunk.SIPTrunk, error) {
	u.b.updated = input
	line, err := lineGetter(u).Execute(ctx, input.WorkspaceID, input.ID)
	if err != nil {
		return nil, err
	}
	sip_trunk_usecase.ApplyUpdate(line, input)
	return line, nil
}

type lineDeleter struct{ b *phoneLineBook }

func (d lineDeleter) Execute(_ context.Context, workspaceID, id string) error {
	if line, ok := d.b.lines[id]; !ok || line.WorkspaceID != workspaceID {
		return sip_trunk.ErrTrunkNotFound
	}
	d.b.deleted = id
	return nil
}

func TestListingPhoneLinesNeverRevealsThePassword(t *testing.T) {
	book := newPhoneLineBook()
	res := NewListPhoneLinesTool(book.deps()).Execute(context.Background(), accessCtx, nil)
	if res.Status != copilot.StatusOK {
		t.Fatalf("res = %+v", res)
	}
	raw, _ := json.Marshal(res.Data)
	if strings.Contains(string(raw), "segredo-antigo") {
		t.Fatal("the password reached the model")
	}
	lines := res.Data.(map[string]interface{})["lines"].([]map[string]interface{})
	if len(lines) != 1 || lines[0]["status"] != "conectada à operadora" || lines[0]["has_password"] != true {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestCreatingAPhoneLineAsksForThePasswordOnTheCardOnly(t *testing.T) {
	tool := NewCreatePhoneLineTool(newPhoneLineBook().deps())
	asker, ok := tool.(copilot.SecretAsker)
	if !ok || len(asker.Secrets(nil)) != 1 || asker.Secrets(nil)[0].Key != "password" {
		t.Fatal("create_phone_line must ask for the password as a protected field")
	}
	if _, takesPassword := tool.Definition().Parameters["password"]; takesPassword {
		t.Fatal("the model must not be offered a password parameter")
	}
}

func TestAPhoneLineIsCheckedBeforeApprovalWithoutItsPassword(t *testing.T) {
	tool := NewCreatePhoneLineTool(newPhoneLineBook().deps()).(copilot.Validator)
	valid := map[string]interface{}{"name": "Principal", "host": "sip.operadora.com", "username": "1001"}
	if err := tool.Validate(context.Background(), accessCtx, valid); err != nil {
		t.Fatalf("a complete line without its password must pass the preflight: %v", err)
	}
	bad := map[string]interface{}{"name": "Principal", "host": "sip:operadora.com:5060", "username": "1001"}
	if err := tool.Validate(context.Background(), accessCtx, bad); err == nil || !strings.Contains(err.Error(), "sem porta") {
		t.Fatalf("a malformed server must be refused before approval, got %v", err)
	}
}

func TestCreatingAPhoneLineUsesTheTypedPasswordAndReportsTheConnection(t *testing.T) {
	book := newPhoneLineBook()
	book.outcome = sip_trunk.RegistrationStatusFailed
	res := NewCreatePhoneLineTool(book.deps()).Execute(context.Background(), accessCtx, map[string]interface{}{
		"name": "Nova", "host": "sip.operadora.com", "username": "2002", "password": "digitada-no-cartao",
	})
	if res.Status != copilot.StatusOK || book.created.Password != "digitada-no-cartao" || !book.created.Enabled {
		t.Fatalf("res = %+v created = %+v", res, book.created)
	}
	data := res.Data.(map[string]interface{})
	raw, _ := json.Marshal(data)
	if strings.Contains(string(raw), "digitada-no-cartao") {
		t.Fatal("the result echoed the password back to the model")
	}
	if data["last_error"] != "401 Unauthorized" || !strings.Contains(data["next_step"].(string), "recusou") {
		t.Fatalf("a refused connection must be explained, got %+v", data)
	}
}

func TestUpdatingAPhoneLineNeverTouchesThePassword(t *testing.T) {
	book := newPhoneLineBook()
	res := NewUpdatePhoneLineTool(book.deps()).Execute(context.Background(), accessCtx, map[string]interface{}{
		"line_id": lineUUID, "name": "Comercial", "password": "sneaked",
	})
	if res.Status != copilot.StatusOK || book.updated.Password != nil || *book.updated.Name != "Comercial" {
		t.Fatalf("res = %+v updated = %+v", res, book.updated)
	}
	if err := NewUpdatePhoneLineTool(book.deps()).(copilot.Validator).Validate(context.Background(), accessCtx, map[string]interface{}{"line_id": lineUUID}); err == nil {
		t.Fatal("an update that changes nothing must be refused")
	}
}

func TestChangingThePasswordRequiresTheProtectedField(t *testing.T) {
	book := newPhoneLineBook()
	tool := NewChangePhoneLinePasswordTool(book.deps())
	if _, ok := tool.(copilot.SecretAsker); !ok {
		t.Fatal("change_phone_line_password must ask for the password as a protected field")
	}
	if res := tool.Execute(context.Background(), accessCtx, map[string]interface{}{"line_id": lineUUID}); res.Status != copilot.StatusError || book.updated.ID != "" {
		t.Fatalf("a missing password must change nothing, got %+v", res)
	}
	res := tool.Execute(context.Background(), accessCtx, map[string]interface{}{"line_id": lineUUID, "password": "nova"})
	if res.Status != copilot.StatusOK || book.updated.Password == nil || *book.updated.Password != "nova" {
		t.Fatalf("res = %+v updated = %+v", res, book.updated)
	}
}

func TestPhoneLinesOfAnotherWorkspaceAreUnknown(t *testing.T) {
	book := newPhoneLineBook()
	other := copilot.Context{WorkspaceID: "ws-2", UserID: "user-1"}
	err := NewDeletePhoneLineTool(book.deps()).(copilot.Validator).Validate(context.Background(), other, map[string]interface{}{"line_id": lineUUID})
	if !errors.Is(err, errInvalidArgs) || !strings.Contains(err.Error(), "não encontrada") {
		t.Fatalf("err = %v", err)
	}
	if res := NewDeletePhoneLineTool(book.deps()).Execute(context.Background(), other, map[string]interface{}{"line_id": lineUUID}); res.Status != copilot.StatusError || book.deleted != "" {
		t.Fatalf("res = %+v deleted = %q", res, book.deleted)
	}
}

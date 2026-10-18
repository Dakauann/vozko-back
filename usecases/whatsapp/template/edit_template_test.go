package template_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
)

var editAt = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

type editClient struct {
	syncTemplatesClientMock
	edits   []conversation.EditTemplateInput
	editErr error
	uploads []string
	current *conversation.Template
}

func (c *editClient) EditTemplate(_ context.Context, _ string, in conversation.EditTemplateInput) error {
	c.edits = append(c.edits, in)
	return c.editErr
}

func (c *editClient) GetTemplate(context.Context, string) (*conversation.Template, error) {
	return c.current, nil
}

func (c *editClient) UploadMediaForTemplate(_ context.Context, in conversation.UploadMediaForTemplateInput) (string, error) {
	c.uploads = append(c.uploads, in.URL)
	return "4::fresh", nil
}

type editTemplates struct {
	sendMockTemplateRepo
	stored *template.Template
}

func (r *editTemplates) FindByID(string) (*template.Template, error) { return r.stored, nil }

type editAccess struct {
	granted bool
	err     error
}

func (a editAccess) GetTemplateIDsForWorkspace(string) ([]string, error) { return nil, nil }
func (a editAccess) HasAccess(string, string) (bool, error)              { return a.granted, a.err }

type editPhones struct {
	phones []*businessphone.WhatsAppBusinessPhoneNumber
	err    error
}

func (p editPhones) FindByWABAId(string) ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return p.phones, p.err
}

type editCampaigns struct {
	busy bool
	err  error
}

func (c editCampaigns) BusyWithTemplate(string, time.Time) (bool, error) { return c.busy, c.err }

type editLog struct {
	previous []time.Time
	records  []template.EditRecord
	err      error
}

func (l *editLog) Record(r template.EditRecord) error {
	l.records = append(l.records, r)
	return nil
}

func (l *editLog) EditedSince(string, time.Time) ([]time.Time, error) { return l.previous, l.err }

type editSync struct{ calls int }

func (s *editSync) Execute(in template.SyncTemplateInput) (*template.Template, error) {
	s.calls++
	return &template.Template{ID: in.TemplateID, Status: template.TemplateStatusPending}, nil
}

type editRig struct {
	client    *editClient
	templates *editTemplates
	log       *editLog
	sync      *editSync
	media     *fakeSetHeaderMediaUC
	deps      EditTemplateDeps
}

func newEditRig() *editRig {
	client := &editClient{current: &conversation.Template{Components: []conversation.TemplateComponent{
		{Type: "HEADER", Format: "IMAGE", Example: &conversation.TemplateExample{HeaderHandle: []string{"https://scontent.example/old.jpg"}}},
		{Type: "BODY", Text: "Oi"},
	}}}
	rig := &editRig{
		client:    client,
		templates: &editTemplates{stored: &template.Template{ID: "tpl-1", ExternalID: "x1", WABAId: "waba-1", Name: "promo", Language: "pt_BR", Status: template.TemplateStatusApproved, Category: template.TemplateCategoryMarketing, Components: []template.TemplateComponent{{Type: "BODY", Text: "Oi"}}}},
		log:       &editLog{},
		sync:      &editSync{},
		media:     &fakeSetHeaderMediaUC{},
	}
	rig.deps = EditTemplateDeps{
		Templates: rig.templates,
		Access:    editAccess{granted: true},
		Phones:    editPhones{phones: []*businessphone.WhatsAppBusinessPhoneNumber{{ID: "phone-1", OwnerWorkspaceID: "ws-1"}}},
		Campaigns: editCampaigns{},
		Edits:     rig.log,
		Clients:   &sendMockClientFactory{wabaForWABA: client},
		Storage:   ourStorage{},
		Sync:      rig.sync,
		Media:     rig.media,
		Now:       func() time.Time { return editAt },
	}
	return rig
}

func (r *editRig) run(t *testing.T, edit template.Edit) (*template.Template, error) {
	t.Helper()
	uc, err := NewEditTemplate(r.deps)
	if err != nil {
		t.Fatal(err)
	}
	return uc.EditContent(context.Background(), template.EditContentInput{WorkspaceID: "ws-1", UserID: "user-1", TemplateID: "tpl-1", Edit: edit})
}

func bodyEdit(text string) template.Edit {
	return template.Edit{Components: []template.TemplateComponent{{Type: "BODY", Text: text}}}
}

func TestAnEditGoesToMetaIsRecordedAndReadBack(t *testing.T) {
	rig := newEditRig()

	got, err := rig.run(t, bodyEdit("Oi, nova oferta. Responda /sair"))

	if err != nil {
		t.Fatal(err)
	}
	if len(rig.client.edits) != 1 || rig.client.edits[0].Components[0].Text != "Oi, nova oferta. Responda /sair" {
		t.Fatalf("edits = %+v", rig.client.edits)
	}
	if len(rig.log.records) != 1 || rig.log.records[0].UserID != "user-1" || rig.log.records[0].WorkspaceID != "ws-1" || !rig.log.records[0].At.Equal(editAt) || rig.log.records[0].Before[0].Text != "Oi" {
		t.Fatalf("records = %+v", rig.log.records)
	}
	if rig.sync.calls != 1 || got.Status != template.TemplateStatusPending {
		t.Fatalf("sync = %d, got = %+v", rig.sync.calls, got)
	}
}

func TestAnEditIsRefusedBeforeMetaWhenTheWorkspaceMayNotEdit(t *testing.T) {
	cases := map[string]struct {
		change func(*EditTemplateDeps)
		want   error
	}{
		"no grant":         {func(d *EditTemplateDeps) { d.Access = editAccess{granted: false} }, template.ErrTemplateAccessDenied},
		"unreadable grant": {func(d *EditTemplateDeps) { d.Access = editAccess{err: errors.New("db")} }, nil},
		"granted but no number": {func(d *EditTemplateDeps) {
			d.Phones = editPhones{phones: []*businessphone.WhatsAppBusinessPhoneNumber{{OwnerWorkspaceID: "ws-other"}}}
		}, template.ErrEditNotOwner},
		"unreadable numbers":     {func(d *EditTemplateDeps) { d.Phones = editPhones{err: errors.New("db")} }, nil},
		"a campaign is using it": {func(d *EditTemplateDeps) { d.Campaigns = editCampaigns{busy: true} }, template.ErrTemplateInUse},
		"unreadable campaigns":   {func(d *EditTemplateDeps) { d.Campaigns = editCampaigns{err: errors.New("db")} }, nil},
		"edited an hour ago":     {func(d *EditTemplateDeps) { d.Edits = &editLog{previous: []time.Time{editAt.Add(-time.Hour)}} }, template.ErrEditWindow},
		"unreadable edit log":    {func(d *EditTemplateDeps) { d.Edits = &editLog{err: errors.New("db")} }, nil},
	}
	for name, tc := range cases {
		rig := newEditRig()
		tc.change(&rig.deps)
		_, err := rig.run(t, bodyEdit("Oi"))
		if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
		if len(rig.client.edits) != 0 {
			t.Errorf("%s: Meta was called", name)
		}
	}
}

func TestAMetaRefusalIsNotRecorded(t *testing.T) {
	rig := newEditRig()
	rig.client.editErr = errors.New("meta said no")

	if _, err := rig.run(t, bodyEdit("Oi")); err == nil {
		t.Fatal("the refusal must surface")
	}
	if len(rig.log.records) != 0 || rig.sync.calls != 0 {
		t.Fatal("nothing changed on Meta, so nothing is recorded")
	}
}

func TestAKeptMediaHeaderIsUploadedAgainFromWhatMetaHolds(t *testing.T) {
	rig := newEditRig()
	edit := template.Edit{Components: []template.TemplateComponent{{Type: "HEADER", Format: "IMAGE"}, {Type: "BODY", Text: "Oi"}}}

	if _, err := rig.run(t, edit); err != nil {
		t.Fatal(err)
	}
	if len(rig.client.uploads) != 1 || rig.client.uploads[0] != "https://scontent.example/old.jpg" {
		t.Fatalf("uploads = %v", rig.client.uploads)
	}
	header := rig.client.edits[0].Components[0]
	if header.Example == nil || header.Example.HeaderHandle[0] != "4::fresh" {
		t.Fatalf("header = %+v", header)
	}
	if len(rig.media.calls) != 0 {
		t.Fatal("the send media is unchanged")
	}
}

func TestAStoredHeaderMediaIsPreferredOverMetasCopy(t *testing.T) {
	rig := newEditRig()
	stored := "https://files.test/ws1/banner.jpg"
	rig.templates.stored.HeaderMediaURL = &stored
	edit := template.Edit{Components: []template.TemplateComponent{{Type: "HEADER", Format: "IMAGE"}, {Type: "BODY", Text: "Oi"}}}

	if _, err := rig.run(t, edit); err != nil {
		t.Fatal(err)
	}
	if len(rig.client.uploads) != 1 || rig.client.uploads[0] != stored {
		t.Fatalf("uploads = %v", rig.client.uploads)
	}
}

func TestANewHeaderMediaComesFromOurStorageAndBecomesTheSendMedia(t *testing.T) {
	rig := newEditRig()
	replacement := "https://files.test/ws1/new.jpg"
	edit := template.Edit{Components: []template.TemplateComponent{{Type: "HEADER", Format: "IMAGE"}, {Type: "BODY", Text: "Oi"}}, HeaderMediaURL: &replacement}

	if _, err := rig.run(t, edit); err != nil {
		t.Fatal(err)
	}
	if len(rig.client.uploads) != 1 || rig.client.uploads[0] != replacement {
		t.Fatalf("uploads = %v", rig.client.uploads)
	}
	if len(rig.media.calls) != 1 || *rig.media.calls[0].HeaderMediaURL != replacement {
		t.Fatalf("media = %+v", rig.media.calls)
	}
}

func TestANewHeaderMediaOutsideOurStorageIsRefused(t *testing.T) {
	rig := newEditRig()
	outside := "http://169.254.169.254/latest"
	edit := template.Edit{Components: []template.TemplateComponent{{Type: "HEADER", Format: "IMAGE"}, {Type: "BODY", Text: "Oi"}}, HeaderMediaURL: &outside}

	if _, err := rig.run(t, edit); !errors.Is(err, template.ErrHeaderMediaOutsideStorage) || len(rig.client.uploads) != 0 {
		t.Fatalf("err = %v, uploads = %v", err, rig.client.uploads)
	}
}

func TestEveryDependencyIsRequired(t *testing.T) {
	deps := newEditRig().deps
	deps.Sync = nil
	if _, err := NewEditTemplate(deps); err == nil {
		t.Fatal("an edit that cannot read the template back must not be built")
	}
}

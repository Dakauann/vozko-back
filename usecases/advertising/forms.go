package advertising

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	ads "vozko/domain/advertising"
	"vozko/domain/lead"
)

const (
	formPollBatch     = 100
	firstPollLookback = 90 * 24 * time.Hour
)

type leadGateway interface {
	ListPages(ctx context.Context, token string) ([]ads.RemotePage, error)
	ListForms(ctx context.Context, token, pageID string) ([]ads.LeadForm, error)
	CreateForm(ctx context.Context, token string, draft ads.LeadFormDraft) (string, error)
	ArchiveForm(ctx context.Context, token, pageID, formID string) error
	ListLeads(ctx context.Context, token, pageID, formID string, since time.Time) ([]ads.FormLead, error)
	GetLead(ctx context.Context, token, pageID, leadgenID string) (*ads.FormLead, error)
	SubscribeLeadgen(ctx context.Context, token, pageID string) error
}

type crmLeads interface {
	FindOrCreate(workspaceID, number string, update lead.LeadUpdate) (*lead.Lead, bool, error)
	FindByIDs(workspaceID string, ids []string) ([]*lead.Lead, error)
}

type FormLeadView struct {
	Lead     *ads.FormLead
	LeadName string
}

type FormsUseCase struct {
	access  accountAccess
	gateway leadGateway
	forms   ads.LeadFormRepository
	leads   ads.FormLeadRepository
	crm     crmLeads
}

func NewFormsUseCase(sync *SyncUseCase, gateway leadGateway, forms ads.LeadFormRepository, leads ads.FormLeadRepository, crm crmLeads) *FormsUseCase {
	return &FormsUseCase{access: sync.access, gateway: gateway, forms: forms, leads: leads, crm: crm}
}

func (uc *FormsUseCase) page(ctx context.Context, token, pageID string) error {
	pages, err := uc.gateway.ListPages(ctx, token)
	if err != nil {
		return err
	}
	for _, p := range pages {
		if p.PageID == pageID {
			return nil
		}
	}
	return ads.FieldError("pageId", "not_available")
}

func (uc *FormsUseCase) List(ctx context.Context, workspaceID, accountID, pageID string) ([]ads.LeadForm, error) {
	account, token, err := uc.access.open(ctx, workspaceID, accountID, ads.UseRead)
	if err != nil {
		return nil, err
	}
	if err := uc.page(ctx, token, pageID); err != nil {
		return nil, uc.access.failed(ctx, account, err)
	}
	forms, err := uc.gateway.ListForms(ctx, token, pageID)
	if err != nil {
		return nil, uc.access.failed(ctx, account, err)
	}
	for _, f := range forms {
		if err := uc.forms.Track(ctx, &ads.TrackedForm{MetaID: f.MetaID, WorkspaceID: workspaceID, AdAccountID: account.ID, PageID: pageID, Name: f.Name}); err != nil {
			log.Printf("[ads] form %s is not tracked for polling: %v", f.MetaID, err)
		}
	}
	return forms, nil
}

func (uc *FormsUseCase) prepare(ctx context.Context, workspaceID string, draft *ads.LeadFormDraft) (*ads.AdAccount, string, error) {
	draft.Normalize()
	if err := draft.Validate(); err != nil {
		return nil, "", err
	}
	account, token, err := uc.access.open(ctx, workspaceID, draft.AdAccountID, ads.UseWrite)
	if err != nil {
		return nil, "", err
	}
	if err := uc.page(ctx, token, draft.PageID); err != nil {
		return nil, "", uc.access.failed(ctx, account, err)
	}
	return account, token, nil
}

func (uc *FormsUseCase) Check(ctx context.Context, workspaceID string, draft ads.LeadFormDraft) (ads.LeadFormDraft, error) {
	if _, _, err := uc.prepare(ctx, workspaceID, &draft); err != nil {
		return ads.LeadFormDraft{}, err
	}
	return draft, nil
}

func (uc *FormsUseCase) Create(ctx context.Context, workspaceID string, draft ads.LeadFormDraft) (*ads.LeadForm, error) {
	account, token, err := uc.prepare(ctx, workspaceID, &draft)
	if err != nil {
		return nil, err
	}
	id, err := uc.gateway.CreateForm(ctx, token, draft)
	if err != nil {
		return nil, uc.access.failed(ctx, account, err)
	}
	if err := uc.forms.Track(ctx, &ads.TrackedForm{MetaID: id, WorkspaceID: workspaceID, AdAccountID: account.ID, PageID: draft.PageID, Name: draft.Name}); err != nil {
		return nil, err
	}
	if err := uc.gateway.SubscribeLeadgen(ctx, token, draft.PageID); err != nil {
		log.Printf("[ads] page %s did not subscribe to lead webhooks; polling still imports its leads: %v", draft.PageID, err)
	}
	return &ads.LeadForm{MetaID: id, PageID: draft.PageID, Name: draft.Name, Status: ads.FormActive, Locale: draft.Locale, Questions: draft.Questions, PrivacyURL: draft.PrivacyURL}, nil
}

func (uc *FormsUseCase) tracked(ctx context.Context, workspaceID, formID string) (*ads.TrackedForm, error) {
	form, err := uc.forms.FindByMetaID(ctx, formID)
	if err != nil {
		return nil, err
	}
	if form.WorkspaceID != workspaceID {
		return nil, ads.ErrLeadFormNotFound
	}
	return form, nil
}

func (uc *FormsUseCase) Archive(ctx context.Context, workspaceID, formID string) error {
	form, err := uc.tracked(ctx, workspaceID, formID)
	if err != nil {
		return err
	}
	account, token, err := uc.access.open(ctx, workspaceID, form.AdAccountID, ads.UseWrite)
	if err != nil {
		return err
	}
	return uc.access.failed(ctx, account, uc.gateway.ArchiveForm(ctx, token, form.PageID, formID))
}

func (uc *FormsUseCase) Leads(ctx context.Context, workspaceID, formID string, limit, offset int) ([]FormLeadView, int64, error) {
	if _, err := uc.tracked(ctx, workspaceID, formID); err != nil {
		return nil, 0, err
	}
	query := ads.FormLeadQuery{WorkspaceID: workspaceID, FormMetaID: formID, Limit: limit, Offset: offset}
	if err := query.Page(); err != nil {
		return nil, 0, err
	}
	rows, total, err := uc.leads.List(ctx, query)
	if err != nil {
		return nil, 0, err
	}
	names, err := uc.crmNames(workspaceID, rows)
	if err != nil {
		return nil, 0, err
	}
	out := make([]FormLeadView, 0, len(rows))
	for _, r := range rows {
		out = append(out, FormLeadView{Lead: r, LeadName: names[r.LeadID]})
	}
	return out, total, nil
}

func (uc *FormsUseCase) crmNames(workspaceID string, rows []*ads.FormLead) (map[string]string, error) {
	var ids []string
	for _, r := range rows {
		if r.LeadID != "" {
			ids = append(ids, r.LeadID)
		}
	}
	names := map[string]string{}
	if len(ids) == 0 {
		return names, nil
	}
	found, err := uc.crm.FindByIDs(workspaceID, ids)
	if err != nil {
		return nil, err
	}
	for _, l := range found {
		names[l.ID] = l.Name
	}
	return names, nil
}

func (uc *FormsUseCase) Sync(ctx context.Context, workspaceID, formID string) (int, error) {
	form, err := uc.tracked(ctx, workspaceID, formID)
	if err != nil {
		return 0, err
	}
	return uc.poll(ctx, form)
}

func (uc *FormsUseCase) poll(ctx context.Context, form *ads.TrackedForm) (int, error) {
	account, token, err := uc.access.open(ctx, form.WorkspaceID, form.AdAccountID, ads.UseRead)
	if err != nil {
		return 0, err
	}
	now := uc.access.now()
	since := now.Add(-firstPollLookback)
	if form.LastPolledAt != nil {
		since = form.LastPolledAt.Add(-time.Hour)
	}
	leads, err := uc.gateway.ListLeads(ctx, token, form.PageID, form.MetaID, since)
	if err != nil {
		return 0, uc.access.failed(ctx, account, err)
	}
	imported := 0
	for i := range leads {
		created, err := uc.importLead(ctx, form, &leads[i])
		if err != nil {
			return imported, err
		}
		if created {
			imported++
		}
	}
	return imported, uc.forms.MarkPolled(ctx, form.MetaID, now)
}

func (uc *FormsUseCase) PollAll(ctx context.Context) error {
	var failures []error
	for offset := 0; ; offset += formPollBatch {
		forms, err := uc.forms.ListAll(ctx, formPollBatch, offset)
		if err != nil {
			return err
		}
		for _, f := range forms {
			if _, err := uc.poll(ctx, f); err != nil {
				log.Printf("[ads] polling form %s failed: %v", f.MetaID, err)
				failures = append(failures, err)
			}
		}
		if len(forms) < formPollBatch {
			break
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("ads: %d form(s) failed to poll: %w", len(failures), errors.Join(failures...))
	}
	return nil
}

func (uc *FormsUseCase) HandleLeadgen(ctx context.Context, event ads.LeadgenEvent) error {
	form, err := uc.forms.FindByMetaID(ctx, event.FormID)
	if errors.Is(err, ads.ErrLeadFormNotFound) {
		log.Printf("[ads] lead %s belongs to form %s that no workspace tracks; ignored", event.LeadgenID, event.FormID)
		return nil
	}
	if err != nil {
		return err
	}
	account, token, err := uc.access.open(ctx, form.WorkspaceID, form.AdAccountID, ads.UseRead)
	if err != nil {
		return err
	}
	l, err := uc.gateway.GetLead(ctx, token, form.PageID, event.LeadgenID)
	if err != nil {
		return uc.access.failed(ctx, account, err)
	}
	_, err = uc.importLead(ctx, form, l)
	return err
}

func (uc *FormsUseCase) importLead(ctx context.Context, form *ads.TrackedForm, l *ads.FormLead) (bool, error) {
	l.WorkspaceID, l.FormMetaID = form.WorkspaceID, form.MetaID
	if l.PageID == "" {
		l.PageID = form.PageID
	}
	created, err := uc.leads.Save(ctx, l)
	if err != nil || !created {
		return created, err
	}
	contact := l.Contact()
	number := lead.NormalizeNumber(contact.Phone)
	if number == "" {
		return true, nil
	}
	crmLead, _, err := uc.crm.FindOrCreate(form.WorkspaceID, number, lead.LeadUpdate{Name: contact.Name})
	if err != nil {
		return true, fmt.Errorf("ads: lead %s saved but not added to the crm: %w", l.MetaID, err)
	}
	return true, uc.leads.LinkLead(ctx, l.MetaID, crmLead.ID)
}

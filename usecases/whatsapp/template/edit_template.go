package template_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/conversation"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
)

var ErrEditDependenciesMissing = errors.New("whatsapp template: the template edit is missing a dependency")

type PhonesByWABA interface {
	FindByWABAId(wabaID string) ([]*businessphone.WhatsAppBusinessPhoneNumber, error)
}

type TemplateCampaigns interface {
	BusyWithTemplate(templateID string, at time.Time) (bool, error)
}

type EditTemplateDeps struct {
	Templates template.Repository
	Access    TemplateAccess
	Phones    PhonesByWABA
	Campaigns TemplateCampaigns
	Edits     template.EditLog
	Clients   template.WhatsAppClientFactory
	Storage   StorageURLs
	Sync      template.SyncTemplateUseCase
	Media     template.SetTemplateHeaderMediaUseCase
	Forms     FlowPublications
	Now       func() time.Time
}

type EditTemplate struct {
	d EditTemplateDeps
}

func NewEditTemplate(deps EditTemplateDeps) (*EditTemplate, error) {
	if deps.Templates == nil || deps.Access == nil || deps.Phones == nil || deps.Campaigns == nil || deps.Edits == nil ||
		deps.Clients == nil || deps.Storage == nil || deps.Sync == nil || deps.Media == nil || deps.Now == nil {
		return nil, ErrEditDependenciesMissing
	}
	return &EditTemplate{d: deps}, nil
}

func (uc *EditTemplate) EditContent(ctx context.Context, in template.EditContentInput) (*template.Template, error) {
	tmpl, err := uc.editableBy(in.WorkspaceID, in.TemplateID)
	if err != nil {
		return nil, err
	}
	now := uc.d.Now()
	previous, err := uc.d.Edits.EditedSince(tmpl.ID, now.Add(-template.EditLookback()))
	if err != nil {
		return nil, fmt.Errorf("edits of template %s: %w", tmpl.ID, err)
	}
	if err := template.CheckEdit(tmpl, in.Edit, previous, now); err != nil {
		return nil, err
	}
	if err := CheckFormPublished(ctx, uc.d.Forms, in.WorkspaceID, tmpl.WABAId, &template.Template{Components: in.Edit.Components}); err != nil {
		return nil, err
	}
	busy, err := uc.d.Campaigns.BusyWithTemplate(tmpl.ID, now)
	if err != nil {
		return nil, fmt.Errorf("campaigns using template %s: %w", tmpl.ID, err)
	}
	if busy {
		return nil, template.ErrTemplateInUse
	}
	client, err := uc.d.Clients.ClientForWABA(tmpl.WABAId)
	if err != nil {
		return nil, err
	}
	components, err := uc.withFreshHeaderHandle(ctx, client, tmpl, in.Edit)
	if err != nil {
		return nil, err
	}
	category := ""
	if edited := in.Edit.CategoryFor(tmpl); edited != tmpl.Category {
		category = string(edited)
	}
	if err := client.EditTemplate(ctx, tmpl.ExternalID, conversation.EditTemplateInput{Category: category, Components: template.ToClientComponents(components)}); err != nil {
		return nil, err
	}
	return uc.settle(tmpl, in, now)
}

func (uc *EditTemplate) editableBy(workspaceID, templateID string) (*template.Template, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, template.ErrTemplateAccessDenied
	}
	granted, err := uc.d.Access.HasAccess(workspaceID, templateID)
	if err != nil {
		return nil, fmt.Errorf("template access of %s: %w", workspaceID, err)
	}
	if !granted {
		return nil, template.ErrTemplateAccessDenied
	}
	tmpl, err := uc.d.Templates.FindByID(templateID)
	if err != nil {
		return nil, err
	}
	if tmpl == nil {
		return nil, template.ErrTemplateNotFound
	}
	phones, err := uc.d.Phones.FindByWABAId(tmpl.WABAId)
	if err != nil {
		return nil, fmt.Errorf("numbers on account %s: %w", tmpl.WABAId, err)
	}
	for _, phone := range phones {
		if phone != nil && phone.BelongsToWorkspace(workspaceID) {
			return tmpl, nil
		}
	}
	return nil, template.ErrEditNotOwner
}

func (uc *EditTemplate) withFreshHeaderHandle(ctx context.Context, client conversation.WhatsAppClient, tmpl *template.Template, edit template.Edit) ([]template.TemplateComponent, error) {
	if _, ok := template.MediaHeaderOf(edit.Components); !ok {
		if edit.HeaderMediaURL != nil {
			return nil, template.ErrHeaderMediaURLNotApplicable
		}
		return edit.Components, nil
	}
	source, err := uc.headerSource(ctx, client, tmpl, edit)
	if err != nil {
		return nil, err
	}
	handle, err := uploadTemplateMedia(ctx, client, source)
	if err != nil {
		return nil, err
	}
	return template.WithHeaderHandle(edit.Components, handle), nil
}

func (uc *EditTemplate) headerSource(ctx context.Context, client conversation.WhatsAppClient, tmpl *template.Template, edit template.Edit) (string, error) {
	if edit.HeaderMediaURL != nil {
		if _, ours := uc.d.Storage.KeyFromURL(*edit.HeaderMediaURL); !ours {
			return "", template.ErrHeaderMediaOutsideStorage
		}
		return *edit.HeaderMediaURL, nil
	}
	if stored := tmpl.GetHeaderMediaURL(); stored != "" {
		if _, ours := uc.d.Storage.KeyFromURL(stored); ours {
			return stored, nil
		}
	}
	current, err := client.GetTemplate(ctx, tmpl.ExternalID)
	if err != nil {
		return "", err
	}
	header, ok := template.MediaHeaderOf(template.FromClientComponents(current.Components))
	if !ok || header.Example == nil || len(header.Example.HeaderHandle) == 0 || !isURL(header.Example.HeaderHandle[0]) {
		return "", template.ErrHeaderMediaNeedsHandle
	}
	return header.Example.HeaderHandle[0], nil
}

func (uc *EditTemplate) settle(tmpl *template.Template, in template.EditContentInput, now time.Time) (*template.Template, error) {
	if err := uc.d.Edits.Record(template.EditRecord{
		TemplateID: tmpl.ID, WorkspaceID: in.WorkspaceID, UserID: in.UserID, At: now,
		Before: tmpl.Components, After: in.Edit.Components,
	}); err != nil {
		return nil, fmt.Errorf("edited on Meta, but the edit of template %s could not be recorded: %w", tmpl.ID, err)
	}
	if in.Edit.HeaderMediaURL != nil {
		if err := uc.d.Media.Execute(template.SetTemplateHeaderMediaInput{TemplateID: tmpl.ID, HeaderMediaURL: in.Edit.HeaderMediaURL}); err != nil {
			return nil, fmt.Errorf("edited on Meta, but the header media of template %s could not be prepared for sending: %w", tmpl.ID, err)
		}
	}
	return uc.d.Sync.Execute(template.SyncTemplateInput{TemplateID: tmpl.ID})
}

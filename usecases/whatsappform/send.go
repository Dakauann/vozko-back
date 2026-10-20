package whatsappform_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	wf "vozko/domain/whatsappform"
)

const (
	FailureOutsideWindow  = "outside_window"
	FailureOutcomeUnknown = "outcome_unknown"
	FailureProvider       = "provider_refused"
)

type SendInput struct {
	EntryID string
	FormID  string
	Copy    wf.SendCopy
	Origin  wf.SendOrigin
}

func (in SendInput) origin() wf.SendOrigin {
	if in.Origin == wf.OriginElo {
		return wf.OriginElo
	}
	return wf.OriginInbox
}

type WorkflowSend struct {
	WorkspaceID string
	EntryID     string
	FormID      string
	Copy        wf.SendCopy
	RunID       string
}

type TemplateSend struct {
	WorkspaceID     string
	BusinessPhoneID string
	ContactNumber   string
	MetaFlowID      string
	TemplateID      string
	Origin          wf.SendOrigin
	OriginRef       string
	EntryID         string
	LeadID          string
	WorkflowRunID   string
	SentBy          string
}

type SendableQuery struct {
	PhoneID    string
	MetaWABAID string
}

type SendableForm struct {
	FormID      string `json:"formId"`
	Name        string `json:"name"`
	VersionID   string `json:"versionId"`
	MetaFlowID  string `json:"metaFlowId"`
	FirstScreen string `json:"firstScreen"`
}

type sendRequest struct {
	workspaceID string
	target      ConversationTarget
	formID      string
	copy        wf.SendCopy
	origin      wf.SendOrigin
	sentBy      string
	runID       string
}

func failureReason(err error) string {
	switch {
	case errors.Is(err, conversation.ErrWindowClosed):
		return FailureOutsideWindow
	case errors.Is(err, conversation.ErrSendOutcomeUnknown):
		return FailureOutcomeUnknown
	}
	return FailureProvider
}

func (s *Service) ownedSendingPhone(workspaceID, phoneID string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	phone, err := s.deps.Phones.FindByID(phoneID)
	if err != nil {
		return nil, err
	}
	if !phone.BelongsToWorkspace(workspaceID) {
		return nil, wf.ErrNotOwner
	}
	if phone.Provider.Normalized() != businessphone.ProviderMeta {
		return nil, wf.ErrProviderUnsupported
	}
	return phone, nil
}

func (s *Service) sendablePublication(ctx context.Context, workspaceID, formID, metaWABAID string) (*wf.Publication, error) {
	pub, err := s.deps.Publications.Find(ctx, workspaceID, formID, metaWABAID)
	if errors.Is(err, wf.ErrPublicationNotFound) {
		return nil, wf.ErrNoPublication
	}
	if err != nil {
		return nil, err
	}
	if !pub.Sendable() {
		return nil, wf.ErrPublicationNotSendable
	}
	return pub, nil
}

func (s *Service) SendInConversation(ctx context.Context, a Actor, in SendInput) (wf.Send, error) {
	if err := s.require(a, wf.CapabilityView, wf.CapabilitySend); err != nil {
		return wf.Send{}, err
	}
	if err := in.Copy.Validate(); err != nil {
		return wf.Send{}, err
	}
	person := shared.Person{UserID: a.UserID, SystemAdmin: a.IsAdmin}
	if !person.MayActOn(s.deps.Access, a.WorkspaceID, in.EntryID, string(shared.EntryTypeWhatsApp)) {
		return wf.Send{}, wf.ErrForbidden
	}
	target, err := s.deps.Conversations.Target(ctx, a.WorkspaceID, in.EntryID)
	if err != nil {
		return wf.Send{}, err
	}
	return s.send(ctx, sendRequest{workspaceID: a.WorkspaceID, target: target, formID: in.FormID, copy: in.Copy, origin: in.origin(), sentBy: a.UserID})
}

func (s *Service) SendFromWorkflow(ctx context.Context, in WorkflowSend) (wf.Send, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" || strings.TrimSpace(in.RunID) == "" {
		return wf.Send{}, wf.ErrWorkspaceRequired
	}
	if err := in.Copy.Validate(); err != nil {
		return wf.Send{}, err
	}
	target, err := s.deps.Conversations.Target(ctx, in.WorkspaceID, in.EntryID)
	if err != nil {
		return wf.Send{}, err
	}
	return s.send(ctx, sendRequest{workspaceID: in.WorkspaceID, target: target, formID: in.FormID, copy: in.Copy, origin: wf.OriginWorkflow, runID: in.RunID})
}

func (s *Service) send(ctx context.Context, req sendRequest) (wf.Send, error) {
	form, err := s.activeForm(ctx, req.workspaceID, req.formID)
	if err != nil {
		return wf.Send{}, err
	}
	phone, err := s.ownedSendingPhone(req.workspaceID, req.target.BusinessPhoneID)
	if err != nil {
		return wf.Send{}, err
	}
	pub, err := s.sendablePublication(ctx, req.workspaceID, form.ID, phone.WABAId)
	if err != nil {
		return wf.Send{}, err
	}
	if !req.target.WindowOpen {
		return wf.Send{}, wf.ErrOutsideWindow
	}
	token, err := wf.MintToken(s.deps.Random)
	if err != nil {
		return wf.Send{}, err
	}
	send := wf.Send{
		ID: s.deps.NewID(), WorkspaceID: req.workspaceID, FormID: form.ID, VersionID: pub.VersionID, PublicationID: pub.ID,
		Token: token, Origin: req.origin, EntryID: req.target.EntryID, EntryType: req.target.EntryType,
		ConversationID: req.target.ConversationID, LeadID: req.target.LeadID, BusinessPhoneID: phone.ID,
		ContactNumber: req.target.ContactNumber, WorkflowRunID: req.runID, SentBy: req.sentBy, Status: wf.SendPrepared, CreatedAt: s.now(),
	}
	if err := s.deps.Sends.Insert(ctx, send); err != nil {
		return wf.Send{}, err
	}
	messageID, sendErr := s.deps.Sender.SendFlowMessage(ctx, phone.ID, conversation.SendFlowMessageInput{
		To: req.target.ContactNumber, FlowID: pub.MetaFlowID, FlowToken: token,
		CTA: req.copy.CTA, Header: req.copy.Header, Body: req.copy.Body, Footer: req.copy.Footer,
	})
	if sendErr != nil {
		if err := send.MarkFailed(failureReason(sendErr)); err != nil {
			return wf.Send{}, errors.Join(sendErr, err)
		}
		if err := s.deps.Sends.Update(ctx, send); err != nil {
			return wf.Send{}, errors.Join(sendErr, err)
		}
		return send, sendErr
	}
	if err := send.MarkSent(messageID, s.now()); err != nil {
		return wf.Send{}, err
	}
	if err := s.deps.Sends.Update(ctx, send); err != nil {
		return wf.Send{}, err
	}
	s.record(ctx, form, send, req, phone)
	return send, nil
}

func (s *Service) record(ctx context.Context, form *wf.Form, send wf.Send, req sendRequest, phone *businessphone.WhatsAppBusinessPhoneNumber) {
	out := OutboundForm{
		WorkspaceID: req.workspaceID, EntryID: send.EntryID, EntryType: send.EntryType, ConversationID: send.ConversationID,
		BusinessPhoneID: phone.ID, From: phone.DisplayPhoneNumber, To: send.ContactNumber, MessageID: send.MessageID, Body: req.copy.Body,
		Metadata: map[string]any{wf.MetadataKey: wf.NewMessageCard(*form, send, req.copy)},
		SentBy:   req.sentBy, Automated: req.sentBy == "", At: s.now(),
	}
	if err := s.deps.History.RecordFormMessage(ctx, out); err != nil {
		log.Printf("[whatsapp-forms] send %s reached WhatsApp as %s but the conversation did not record it: %v", send.ID, send.MessageID, err)
	}
}

func (s *Service) PrepareTemplateSend(ctx context.Context, in TemplateSend) (wf.Send, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return wf.Send{}, wf.ErrWorkspaceRequired
	}
	if strings.TrimSpace(in.ContactNumber) == "" || strings.TrimSpace(in.MetaFlowID) == "" {
		return wf.Send{}, wf.ErrTokenRequired
	}
	phone, err := s.ownedSendingPhone(in.WorkspaceID, in.BusinessPhoneID)
	if err != nil {
		return wf.Send{}, err
	}
	pub, err := s.deps.Publications.FindByMetaFlow(ctx, phone.WABAId, in.MetaFlowID)
	if errors.Is(err, wf.ErrPublicationNotFound) || (err == nil && pub.WorkspaceID != in.WorkspaceID) {
		return wf.Send{}, wf.ErrNoPublication
	}
	if err != nil {
		return wf.Send{}, err
	}
	if !pub.Sendable() {
		return wf.Send{}, wf.ErrPublicationNotSendable
	}
	token, err := wf.MintToken(s.deps.Random)
	if err != nil {
		return wf.Send{}, err
	}
	origin := in.Origin
	if !origin.Valid() {
		origin = wf.OriginTemplate
	}
	send := wf.Send{
		ID: s.deps.NewID(), WorkspaceID: in.WorkspaceID, FormID: pub.FormID, VersionID: pub.VersionID, PublicationID: pub.ID,
		Token: token, Origin: origin, OriginRef: in.OriginRef, EntryID: in.EntryID, EntryType: string(shared.EntryTypeWhatsApp),
		LeadID: in.LeadID, BusinessPhoneID: phone.ID, ContactNumber: in.ContactNumber, TemplateID: in.TemplateID,
		WorkflowRunID: in.WorkflowRunID, SentBy: in.SentBy, Status: wf.SendPrepared, CreatedAt: s.now(),
	}
	if err := s.deps.Sends.Insert(ctx, send); err != nil {
		return wf.Send{}, err
	}
	return send, nil
}

func (s *Service) SettleTemplateSend(ctx context.Context, token, messageID string, sendErr error) error {
	send, err := s.deps.Sends.FindByToken(ctx, token)
	if err != nil {
		return err
	}
	if sendErr != nil {
		if err := send.MarkFailed(failureReason(sendErr)); err != nil {
			return err
		}
	} else if err := send.MarkSent(messageID, s.now()); err != nil {
		return err
	}
	return s.deps.Sends.Update(ctx, *send)
}

func (s *Service) Sendable(ctx context.Context, a Actor, q SendableQuery) ([]SendableForm, error) {
	if err := s.require(a, wf.CapabilityView); err != nil {
		return nil, err
	}
	metaWABAID, err := s.sendableAccount(ctx, a.WorkspaceID, q)
	if err != nil {
		return nil, err
	}
	summaries, err := s.deps.Forms.List(ctx, wf.FormQuery{WorkspaceID: a.WorkspaceID})
	if err != nil {
		return nil, err
	}
	out := []SendableForm{}
	for _, summary := range summaries {
		pub, err := s.sendablePublication(ctx, a.WorkspaceID, summary.Form.ID, metaWABAID)
		if errors.Is(err, wf.ErrNoPublication) || errors.Is(err, wf.ErrPublicationNotSendable) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, SendableForm{FormID: summary.Form.ID, Name: summary.Form.Name, VersionID: pub.VersionID, MetaFlowID: pub.MetaFlowID, FirstScreen: wf.FirstScreenID})
	}
	return out, nil
}

func (s *Service) sendableAccount(ctx context.Context, workspaceID string, q SendableQuery) (string, error) {
	if q.PhoneID != "" {
		phone, err := s.ownedSendingPhone(workspaceID, q.PhoneID)
		if err != nil {
			return "", err
		}
		return phone.WABAId, nil
	}
	acct, err := s.accountFor(ctx, workspaceID, q.MetaWABAID)
	if err != nil {
		return "", fmt.Errorf("account %s: %w", q.MetaWABAID, err)
	}
	return acct.metaWABAID, nil
}

func (s *Service) Sends(ctx context.Context, a Actor, formID, after string, limit int) (wf.Page[wf.Send], error) {
	if err := s.require(a, wf.CapabilityView); err != nil {
		return wf.Page[wf.Send]{}, err
	}
	scope, err := s.readScope(a)
	if err != nil {
		return wf.Page[wf.Send]{}, err
	}
	if _, err := s.deps.Forms.Get(ctx, a.WorkspaceID, formID); err != nil {
		return wf.Page[wf.Send]{}, err
	}
	return s.deps.Sends.List(ctx, wf.SendQuery{WorkspaceID: a.WorkspaceID, FormID: formID, After: after, Limit: limit, Scope: scope})
}

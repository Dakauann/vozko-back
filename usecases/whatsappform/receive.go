package whatsappform_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/actor"
	"vozko/domain/lead"
	wf "vozko/domain/whatsappform"
	lead_usecase "vozko/usecases/lead"
)

const (
	ConsentGranted   = "granted"
	ConsentUnchanged = "unchanged"
	FileStoreFailed  = "store_failed"
	refusedNoLead    = "lead_missing"
	runDataResponse  = "form_response_id"
	runDataForm      = "form_id"
	runDataPrefix    = "form."
)

type InboundForm struct {
	WorkspaceID      string
	BusinessPhoneID  string
	ContactNumber    string
	EntryID          string
	EntryType        string
	ConversationID   string
	LeadID           string
	InboundMessageID string
	ContextMessageID string
	ResponseJSON     string
	SubmittedAt      time.Time
}

type Received struct {
	Response     wf.Response
	Text         string
	Metadata     map[string]any
	ResumedRunID string
}

type matched struct {
	send    *wf.Send
	form    *wf.Form
	version *wf.Version
}

func (s *Service) findSend(ctx context.Context, token, contextID string) (*wf.Send, error) {
	if wf.IsVozkoToken(token) {
		send, err := s.deps.Sends.FindByToken(ctx, token)
		switch {
		case err == nil:
			return send, nil
		case !errors.Is(err, wf.ErrSendNotFound):
			return nil, err
		}
	}
	if strings.TrimSpace(contextID) == "" {
		return nil, nil
	}
	send, err := s.deps.Sends.FindByMessageID(ctx, contextID)
	if errors.Is(err, wf.ErrSendNotFound) {
		return nil, nil
	}
	return send, err
}

func (s *Service) match(ctx context.Context, in InboundForm, token string) (wf.MatchResult, matched, error) {
	send, err := s.findSend(ctx, token, in.ContextMessageID)
	if err != nil {
		return "", matched{}, err
	}
	result := wf.MatchResponse(send, wf.Inbound{Token: token, ContextMessageID: in.ContextMessageID, BusinessPhoneID: in.BusinessPhoneID, ContactNumber: in.ContactNumber})
	if send != nil && send.WorkspaceID != in.WorkspaceID {
		result = wf.UnmatchedSender
	}
	if !result.Matched() {
		return result, matched{}, nil
	}
	form, err := s.deps.Forms.Get(ctx, send.WorkspaceID, send.FormID)
	if err != nil {
		return "", matched{}, err
	}
	version, err := s.deps.Versions.Get(ctx, send.WorkspaceID, send.VersionID)
	if err != nil {
		return "", matched{}, err
	}
	return result, matched{send: send, form: form, version: version}, nil
}

func (s *Service) Receive(ctx context.Context, in InboundForm) (Received, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return Received{}, wf.ErrWorkspaceRequired
	}
	raw := []byte(in.ResponseJSON)
	token, err := wf.ReadToken(raw)
	if err != nil {
		return Received{}, err
	}
	result, m, err := s.match(ctx, in, token)
	if err != nil {
		return Received{}, err
	}
	var doc wf.Document
	if m.version != nil {
		doc = m.version.Document
	}
	answers, err := wf.ParseResponse(doc, raw)
	if err != nil {
		return Received{}, err
	}
	resp := wf.Response{
		ID: s.deps.NewID(), WorkspaceID: in.WorkspaceID, Match: result, InboundMessageID: in.InboundMessageID,
		ConversationID: in.ConversationID, EntryID: in.EntryID, EntryType: in.EntryType, LeadID: in.LeadID,
		BusinessPhoneID: in.BusinessPhoneID, ContactNumber: in.ContactNumber, Answers: answers, Raw: json.RawMessage(raw),
		ApplyStatus: wf.ApplySkipped, SubmittedAt: in.SubmittedAt.UTC(), CreatedAt: s.now(),
	}
	if resp.SubmittedAt.IsZero() {
		resp.SubmittedAt = resp.CreatedAt
	}
	if m.send != nil {
		resp.FormID, resp.VersionID, resp.SendID, resp.Origin, resp.ApplyStatus = m.form.ID, m.version.ID, m.send.ID, m.send.Origin, wf.ApplyPending
		if resp.LeadID == "" {
			resp.LeadID = m.send.LeadID
		}
	}
	stored, _, err := s.deps.Responses.Insert(ctx, resp)
	if err != nil {
		return Received{}, err
	}
	if stored.ApplyStatus == wf.ApplyPending && m.send != nil {
		if stored, err = s.apply(ctx, m, stored); err != nil {
			return Received{}, err
		}
	}
	return s.received(m, stored), nil
}

func (s *Service) received(m matched, resp wf.Response) Received {
	out := Received{Response: resp, ResumedRunID: resp.Report.Resumed}
	if m.version == nil {
		out.Text = wf.UnmatchedSummary(resp.Answers)
		out.Metadata = map[string]any{wf.MetadataKey: wf.NewResponseCard(nil, "", resp)}
		return out
	}
	out.Text = wf.TextSummary(m.version.Document, m.form.Name, resp.Answers, false)
	out.Metadata = map[string]any{wf.MetadataKey: wf.NewResponseCard(&m.version.Document, m.form.Name, resp)}
	return out
}

func (s *Service) checkpoint(ctx context.Context, resp *wf.Response) error {
	return s.deps.Responses.SaveApply(ctx, resp.WorkspaceID, resp.ID, resp.ApplyStatus, resp.Report)
}

func (s *Service) apply(ctx context.Context, m matched, resp wf.Response) (wf.Response, error) {
	doc := m.version.Document
	if !resp.Report.Written {
		if err := s.writeLead(ctx, doc, &resp); err != nil {
			return wf.Response{}, err
		}
		s.storeFiles(ctx, doc, &resp)
		resp.Report.Written = true
		if err := s.checkpoint(ctx, &resp); err != nil {
			return wf.Response{}, err
		}
	}
	if err := m.send.MarkResponded(resp.SubmittedAt); err != nil {
		return wf.Response{}, err
	}
	if err := s.deps.Sends.Update(ctx, *m.send); err != nil {
		return wf.Response{}, err
	}
	data := runData(doc, resp)
	if m.send.WorkflowRunID != "" && resp.Report.Resumed == "" {
		if err := s.deps.Runs.ResumeForm(ctx, m.send.WorkflowRunID, data); err != nil {
			return wf.Response{}, err
		}
		resp.Report.Resumed = m.send.WorkflowRunID
		if err := s.checkpoint(ctx, &resp); err != nil {
			return wf.Response{}, err
		}
	}
	if !resp.Report.Triggered {
		trigger := FormTrigger{WorkspaceID: resp.WorkspaceID, EntryID: resp.EntryID, EntryType: resp.EntryType, FormID: resp.FormID, ResponseID: resp.ID, Data: data}
		if err := s.deps.Triggers.FormSubmitted(ctx, trigger); err != nil {
			return wf.Response{}, err
		}
		resp.Report.Triggered = true
	}
	resp.ApplyStatus = wf.ApplyApplied
	if len(resp.Report.Refused) > 0 || len(resp.Report.Conflicts) > 0 || filesFailed(resp.Report) {
		resp.ApplyStatus = wf.ApplyPartial
	}
	if err := s.checkpoint(ctx, &resp); err != nil {
		return wf.Response{}, err
	}
	return resp, nil
}

func filesFailed(report wf.ApplyReport) bool {
	for _, f := range report.Files {
		if f.Problem != "" {
			return true
		}
	}
	return false
}

func refusalCode(err error) string {
	if code := lead.ErrorCode(err); code != "" {
		return code
	}
	return "lead_refused"
}

func (s *Service) writeLead(ctx context.Context, doc wf.Document, resp *wf.Response) error {
	if resp.LeadID == "" {
		resp.Report.Refused = append(resp.Report.Refused, refusedNoLead)
		return nil
	}
	defs, err := s.leadDefinitions(resp.WorkspaceID)
	if err != nil {
		return err
	}
	if profile := wf.ToProfile(doc, resp.Answers, defs); !profile.Empty() {
		result, err := s.deps.Profiles.Update(ctx, lead_usecase.ProfileUpdate{
			WorkspaceID: resp.WorkspaceID, LeadID: resp.LeadID, Actor: actor.SystemID, Source: lead.ProfileFromForm, Profile: profile,
		})
		switch {
		case lead.IsInputRefusal(err):
			resp.Report.Refused = append(resp.Report.Refused, refusalCode(err))
		case err != nil:
			return fmt.Errorf("lead %s from response %s: %w", resp.LeadID, resp.ID, err)
		default:
			resp.Report.Changed, resp.Report.Conflicts = result.Changed, result.Conflicts
		}
	}
	change := wf.ToConsent(doc, resp.Answers, resp.ID, resp.SubmittedAt)
	if change == nil {
		return nil
	}
	outcome, err := s.deps.Consents.Record(ctx, resp.WorkspaceID, resp.LeadID, *change)
	switch {
	case lead.IsInputRefusal(err):
		resp.Report.Refused = append(resp.Report.Refused, refusalCode(err))
	case err != nil:
		return fmt.Errorf("consent of lead %s from response %s: %w", resp.LeadID, resp.ID, err)
	case outcome.Changed:
		resp.Report.Consent = ConsentGranted
	default:
		resp.Report.Consent = ConsentUnchanged
	}
	return nil
}

func (s *Service) storeFiles(ctx context.Context, doc wf.Document, resp *wf.Response) {
	resp.Report.Files = nil
	for _, ref := range doc.Fields() {
		answer, ok := resp.Answers.Values[ref.Field.Key]
		if !ok {
			continue
		}
		for _, f := range answer.Files {
			outcome := wf.FileOutcome{Key: ref.Field.Key, FileID: f.ID}
			mediaID, err := s.deps.Media.StoreFormFile(ctx, StoredFile{
				WorkspaceID: resp.WorkspaceID, BusinessPhoneID: resp.BusinessPhoneID, EntryID: resp.EntryID, EntryType: resp.EntryType,
				ResponseID: resp.ID, FieldKey: ref.Field.Key, File: f,
			})
			if err != nil {
				outcome.Problem = FileStoreFailed
			} else {
				outcome.MediaID = mediaID
			}
			resp.Report.Files = append(resp.Report.Files, outcome)
		}
	}
}

func runData(doc wf.Document, resp wf.Response) map[string]any {
	data := map[string]any{runDataResponse: resp.ID, runDataForm: resp.FormID}
	for _, ref := range doc.Fields() {
		if answer, ok := resp.Answers.Values[ref.Field.Key]; ok {
			data[runDataPrefix+ref.Field.Key] = answer.Shown(ref.Field, false)
		}
	}
	return data
}

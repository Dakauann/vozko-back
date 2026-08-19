package whatsapp_outreach

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	lcs "vozko/domain/lead_campaign_send"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	wo "vozko/domain/whatsapp_outreach"
	"vozko/domain/workspace_phone_access"
	"vozko/domain/workspace_template_access"
)

// SpamPolicyReader is the workspace's own cooldown setting.
//
// Narrow port rather than the whole workspace-config repository: this use case
// needs one integer, and depending on the entire config surface would make it
// impossible to test without one.
type SpamPolicyReader interface {
	SpamProtectionDays(ctx context.Context, workspaceID string) (int, error)
}

// WindowReader answers whether a free reply is already possible.
//
// One method rather than the whole message-window repository: this use case asks
// a single question, and depending on the rest of that surface would make it
// impossible to substitute — and would invite some later edit to RECORD a window
// here, which must never happen (Meta opens the window on the customer's reply,
// not on ours).
type WindowReader interface {
	IsWindowOpen(leadID, businessPhoneID string) (bool, error)
}

// RateLimiter bounds how fast one workspace may start paid conversations.
//
// Cold outbound at operator pace is a handful an hour; a script driving the same
// endpoint is thousands. The permission system cannot tell them apart because
// both are the same operator with the same rights — only pace distinguishes them.
type RateLimiter interface {
	Allow(ctx context.Context, workspaceID string, limit int, window time.Duration) (bool, error)
}

// Deps is everything reaching a stranger touches. Long, because the guard list
// is the feature: each entry is one way this send could be wrong.
type Deps struct {
	Phones        businessphone.Repository
	PhoneGrants   workspace_phone_access.Repository
	Templates     template.Repository
	TemplateGrant workspace_template_access.CheckAccessUseCase

	Leads         lead.Repository
	Entries       wce.Repository
	Campaigns     wc.Repository
	EnsureOrganic wc.EnsureOrganicCoexistenceCampaignUseCase
	Windows       WindowReader
	CampaignSends lcs.Repository
	SpamPolicy    SpamPolicyReader
	History       conversation.MessageHistoryManager
	Sender        template.BilledTemplateSendUseCase
	Limiter       RateLimiter
	HourlySendCap int
	Now           func() time.Time
}

type startConversationUseCase struct {
	deps Deps
}

// NewStartConversationUseCase refuses to build an orchestrator that cannot
// enforce its own guards.
//
// The sender is checked hardest: without it there is nothing to send with, and a
// nil-tolerant version of this constructor would produce a use case that creates
// conversations nobody ever receives a message in.
func NewStartConversationUseCase(deps Deps) (wo.StartOfficialConversationUseCase, error) {
	var missing []string
	if deps.Phones == nil {
		missing = append(missing, "business phone repository")
	}
	if deps.Templates == nil {
		missing = append(missing, "template repository")
	}
	if deps.Leads == nil {
		missing = append(missing, "lead repository")
	}
	if deps.Entries == nil {
		missing = append(missing, "campaign entry repository")
	}
	if deps.EnsureOrganic == nil {
		missing = append(missing, "organic campaign use case")
	}
	if deps.Sender == nil {
		missing = append(missing, "billed template sender")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("whatsapp outreach: %s not configured", strings.Join(missing, ", "))
	}
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	if deps.HourlySendCap <= 0 {
		deps.HourlySendCap = 60
	}
	return &startConversationUseCase{deps: deps}, nil
}

// Execute reaches a number that never wrote to us.
//
// Read the order as a single claim: NOTHING below the money line can refuse the
// send. Every check that can say no — permission, ownership, tenancy, blocked
// contact, an already-open window, the workspace's own spam cooldown, pace — is
// asked first, because a refusal after the debit is a charge followed by a
// refund, and the operator experiences that as "it failed and took my money".
//
// On ErrWindowAlreadyOpen the result is still returned, populated with the entry
// the operator should be taken to. That is deliberate: the answer to "you are
// already talking to this person" is to open the conversation, not to show an
// error and leave them where they were.
func (uc *startConversationUseCase) Execute(ctx context.Context, in wo.StartConversationInput) (*wo.StartedConversation, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return nil, template.ErrWorkspaceRequired
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return nil, template.ErrIdempotencyKeyRequired
	}

	// The server's own normalisation, before any lookup and long before any
	// charge. A number that does not survive this can never match a lead or an
	// entry, so accepting it would create a charge against a contact that cannot
	// be found again.
	number := lead.NormalizeNumber(in.PhoneNumber)
	if number == "" {
		return nil, wo.ErrInvalidPhone
	}

	// ---- who may send, from what, to whom ---------------------------------

	phone, err := uc.deps.Phones.FindByID(in.BusinessPhoneID)
	if err != nil || phone == nil {
		return nil, wo.ErrBusinessPhoneNotFound
	}
	allowed, err := businessphone.CanWorkspaceSendFrom(in.WorkspaceID, in.BusinessPhoneID, phone, uc.deps.PhoneGrants)
	if err != nil {
		return nil, err
	}
	if !allowed {
		// Not-found flavoured on purpose: a "forbidden" here would confirm that a
		// given phone id exists in some other workspace.
		return nil, wo.ErrBusinessPhoneNotFound
	}
	if !phone.IsVerified() {
		return nil, wo.ErrPhoneNotConnected
	}

	tmpl, err := uc.deps.Templates.FindByID(in.TemplateID)
	if err != nil || tmpl == nil {
		return nil, wo.ErrTemplateNotFound
	}
	if uc.deps.TemplateGrant != nil {
		granted, accessErr := uc.deps.TemplateGrant.Execute(in.WorkspaceID, tmpl.ID)
		if accessErr != nil {
			return nil, accessErr
		}
		if !granted {
			// The same rule the template LIST already applies. Enforcing it only on
			// the list would mean anyone who learned an id could spend money on a
			// template their workspace was never given.
			return nil, wo.ErrTemplateForbidden
		}
	}

	// ---- the container, and the department that owns it --------------------

	campaign, _, err := uc.deps.EnsureOrganic.Execute(in.WorkspaceID, phone.ID, phone.DisplayPhoneNumber)
	if err != nil || campaign == nil {
		return nil, fmt.Errorf("whatsapp outreach: could not resolve the conversation container: %w", err)
	}
	if !uc.departmentAllows(in, campaign) {
		return nil, wo.ErrDepartmentForbidden
	}

	// ---- the contact -------------------------------------------------------

	leadRecord, _, err := uc.deps.Leads.FindOrCreate(in.WorkspaceID, number, lead.LeadUpdate{Name: strings.TrimSpace(in.Name)})
	if err != nil {
		return nil, err
	}
	if leadRecord.Blocked {
		return nil, wo.ErrLeadBlocked
	}

	// Reuse the conversation this number already has on this number, exactly as
	// the inbound webhook would. Skipping this is how you get two threads with
	// the same human, one of which nobody is watching.
	entry, entryExisted := uc.findExistingEntry(number, phone.ID)

	// ---- the free alternative ---------------------------------------------

	if uc.deps.Windows != nil {
		open, windowErr := uc.deps.Windows.IsWindowOpen(leadRecord.ID, phone.ID)
		if windowErr != nil {
			// Fail closed. An unreadable window is not permission to charge.
			return nil, fmt.Errorf("whatsapp outreach: could not read the messaging window: %w", windowErr)
		}
		if open {
			// Refusing here is a REFUND avoided, not a feature withheld: inside the
			// 24h window the ordinary composer sends the same message for nothing.
			result := &wo.StartedConversation{
				LeadID:              leadRecord.ID,
				EntryType:           string(shared.EntryTypeWhatsApp),
				ConversationExisted: entryExisted,
			}
			if entry != nil {
				result.EntryID = entry.ID
			}
			return result, wo.ErrWindowAlreadyOpen
		}
	}

	if err := uc.refuseIfSpam(ctx, in.WorkspaceID, leadRecord.ID, phone.ID); err != nil {
		return nil, err
	}
	if err := uc.refuseIfTooFast(ctx, in.WorkspaceID); err != nil {
		return nil, err
	}

	// ---- the entry, then the money ----------------------------------------

	if entry == nil {
		entry = &wce.WhatsAppCampaignEntry{
			ID:         uuid.New().String(),
			CampaignID: campaign.ID,
			LeadID:     leadRecord.ID,
			// PENDING, not SENT: SENT is counted as a billed dispatch in the campaign
			// rollups, and nothing has been dispatched yet.
			Status:    wce.SendStatusPending,
			Variables: in.BodyParams,
		}
		if createErr := uc.deps.Entries.Create(entry); createErr != nil {
			if errors.Is(createErr, wce.ErrEntryDuplicate) {
				// Lost a race against another operator opening the same conversation.
				if existing, findErr := uc.deps.Entries.FindByCampaignAndLead(campaign.ID, leadRecord.ID); findErr == nil && existing != nil {
					entry, entryExisted = existing, true
				} else {
					return nil, createErr
				}
			} else {
				return nil, createErr
			}
		}
	}

	sendResult, sendErr := uc.deps.Sender.Execute(ctx, template.BilledSendInput{
		WorkspaceID:     in.WorkspaceID,
		UserID:          in.UserID,
		IdempotencyKey:  in.IdempotencyKey,
		BusinessPhoneID: phone.ID,
		TemplateID:      tmpl.ID,
		// The addressing form, which is not the same as the stored form: Meta wants
		// the 9th digit that Brazilian mobile numbers may or may not carry.
		ToNumber:     lead.NormalizeWhatsAppNumber(number),
		BodyParams:   in.BodyParams,
		HeaderParams: in.HeaderParams,
		CampaignID:   campaign.ID,
		EntryID:      entry.ID,
	})
	if sendErr != nil {
		uc.markEntryFailed(entry.ID, sendResult, sendErr)
		return nil, sendErr
	}

	// ---- everything after here is bookkeeping ------------------------------
	//
	// The template has been delivered and paid for. From this point NOTHING may
	// return an error: a caller that believes the send failed retries it, and a
	// retry of a delivered message is a second message and a second charge. Every
	// failure below is logged and reported as a flag on a successful result.

	result := &wo.StartedConversation{
		EntryID:             entry.ID,
		EntryType:           string(shared.EntryTypeWhatsApp),
		LeadID:              leadRecord.ID,
		AttemptID:           sendResult.AttemptID,
		MessageID:           sendResult.MessageID,
		ConversationExisted: entryExisted,
		Replayed:            sendResult.Replayed,
		ChargedMicros:       sendResult.ChargedMicros,
	}
	if sendResult.Replayed {
		// Nothing was sent and nothing was spent, so nothing should be written
		// either — the original send already did all of it.
		result.Recorded = true
		return result, nil
	}

	if err := uc.deps.Entries.UpdateStatus(entry.ID, wce.SendStatusSent, sendResult.MessageID, 0, ""); err != nil {
		log.Printf("[whatsapp-outreach] could not mark entry %s as sent: %v", entry.ID, err)
	}
	uc.storeTemplateInfo(entry, tmpl, in.BodyParams)
	result.Recorded = uc.recordMessage(ctx, entry, tmpl, in, leadRecord, sendResult)

	if uc.deps.CampaignSends != nil {
		// The same record the campaign pipeline writes, so the next send — from
		// either path — sees this one and honours the cooldown.
		if err := uc.deps.CampaignSends.Record(leadRecord.ID, phone.ID, campaign.ID); err != nil {
			log.Printf("[whatsapp-outreach] could not record the send against lead %s: %v", leadRecord.ID, err)
		}
	}

	return result, nil
}

func (uc *startConversationUseCase) findExistingEntry(number, phoneID string) (*wce.WhatsAppCampaignEntry, bool) {
	existing, err := uc.deps.Entries.FindByNumberAndBusinessPhone(number, phoneID)
	if err != nil || existing == nil {
		return nil, false
	}
	return existing, true
}

// departmentAllows mirrors the rule the rest of the CRM applies: a restricted
// operator sees their departments' work, and a container with NO department
// belongs to everyone.
//
// The permissive default is deliberate and load-bearing here — organic
// containers are created without a department, so a fail-closed reading would
// refuse every departmentally-scoped operator on every number.
func (uc *startConversationUseCase) departmentAllows(in wo.StartConversationInput, campaign *wc.Campaign) bool {
	if in.IsAdmin || len(in.DepartmentIDs) == 0 {
		return true
	}
	if strings.TrimSpace(campaign.DepartmentID) == "" {
		return true
	}
	for _, id := range in.DepartmentIDs {
		if id == campaign.DepartmentID {
			return true
		}
	}
	return false
}

func (uc *startConversationUseCase) refuseIfSpam(ctx context.Context, workspaceID, leadID, phoneID string) error {
	if uc.deps.SpamPolicy == nil || uc.deps.CampaignSends == nil {
		return nil
	}
	days, err := uc.deps.SpamPolicy.SpamProtectionDays(ctx, workspaceID)
	if err != nil || days <= 0 {
		return nil
	}
	lastSent, err := uc.deps.CampaignSends.GetLastSendTime(leadID, phoneID)
	if err != nil {
		return nil
	}
	if lcs.WithinSpamWindow(lastSent, days, uc.deps.Now()) {
		return wo.ErrWithinSpamWindow
	}
	return nil
}

func (uc *startConversationUseCase) refuseIfTooFast(ctx context.Context, workspaceID string) error {
	if uc.deps.Limiter == nil {
		return nil
	}
	ok, err := uc.deps.Limiter.Allow(ctx, workspaceID, uc.deps.HourlySendCap, time.Hour)
	if err != nil {
		// A limiter we cannot reach must not become a reason to refuse legitimate
		// work: the guard below it is the balance, which is authoritative.
		log.Printf("[whatsapp-outreach] rate limiter unavailable for workspace %s: %v", workspaceID, err)
		return nil
	}
	if !ok {
		return wo.ErrRateLimited
	}
	return nil
}

func (uc *startConversationUseCase) markEntryFailed(entryID string, result *template.BilledSendResult, sendErr error) {
	code, message := 0, sendErr.Error()
	if result != nil && result.Outcome == template.OutcomeUnknown {
		// Not a failure we can assert. Leave the entry pending so the reconcile
		// sweep, not this line, decides what happened.
		return
	}
	if len(message) > 500 {
		message = message[:500]
	}
	if err := uc.deps.Entries.UpdateStatus(entryID, wce.SendStatusFailed, "", code, message); err != nil {
		log.Printf("[whatsapp-outreach] could not mark entry %s as failed: %v", entryID, err)
	}
}

// storeTemplateInfo puts the rendered template on the entry, the source the CRM
// panel reads.
func (uc *startConversationUseCase) storeTemplateInfo(entry *wce.WhatsAppCampaignEntry, tmpl *template.Template, params []string) {
	meta := entry.Metadata
	if meta == nil {
		meta = map[string]interface{}{}
	}
	meta["template_info"] = tmpl.RenderInfo(params)
	if err := uc.deps.Entries.UpdateMetadata(entry.ID, meta); err != nil {
		log.Printf("[whatsapp-outreach] could not store template info on entry %s: %v", entry.ID, err)
	}
}

// recordMessage writes the template into the thread.
//
// This is what makes the conversation exist for the operator: the inbox lists
// entries whose last_message_at is set, and only writing a message sets it. An
// entry without one is a conversation that was paid for and cannot be found.
func (uc *startConversationUseCase) recordMessage(
	ctx context.Context,
	entry *wce.WhatsAppCampaignEntry,
	tmpl *template.Template,
	in wo.StartConversationInput,
	leadRecord *lead.Lead,
	sendResult *template.BilledSendResult,
) bool {
	if uc.deps.History == nil {
		return false
	}

	info := tmpl.RenderInfo(in.BodyParams)
	bodyText, _ := info["body_text"].(string)
	metaBytes, err := json.Marshal(info)
	if err != nil {
		log.Printf("[whatsapp-outreach] could not marshal template metadata for entry %s: %v", entry.ID, err)
		return false
	}

	record := conversation.MessageHistoryRecord{
		EntryID:     entry.ID,
		EntryType:   shared.EntryTypeWhatsApp,
		Channel:     conversation.MessageChannelWhatsApp,
		MessageType: conversation.MessageTypeTemplate,
		MessageID:   sendResult.MessageID,
		// The operator, not the system. A paid message with no author is a charge
		// nobody can be asked about.
		From:      in.UserID,
		To:        leadRecord.Number,
		Text:      bodyText,
		Timestamp: uc.deps.Now(),
		Metadata:  json.RawMessage(metaBytes),
	}
	if err := uc.deps.History.Record(ctx, conversation.MessageDirectionOutbound, record); err != nil {
		log.Printf("[whatsapp-outreach] template delivered but not recorded on entry %s: %v", entry.ID, err)
		return false
	}
	return true
}

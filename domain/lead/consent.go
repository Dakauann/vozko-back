package lead

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/actor"
	"vozko/domain/recordevent"
)

const (
	MaxConsentExcerptLength = 280
	FieldConsent            = "consent"
	EventConsentGranted     = recordevent.Kind("consent_granted")
	EventConsentWithdrawn   = recordevent.Kind("consent_withdrawn")
	SheetEvidenceRef        = "lead_sheet"
)

var (
	ErrConsentSourceUnknown    = errors.New("lead: the consent change names a source this server does not know")
	ErrConsentKindInvalid      = errors.New("lead: the consent change must be a grant or a withdrawal")
	ErrConsentNotAllowed       = errors.New("lead: this source may not record this consent change")
	ErrConsentActorInvalid     = errors.New("lead: the consent change needs a known actor allowed for its source")
	ErrConsentEvidenceRequired = errors.New("lead: the consent change needs the evidence its source requires")
	ErrConsentEvidenceInvalid  = errors.New("lead: the consent evidence is not of a kind this source accepts")
	ErrConsentExcerptTooLong   = errors.New("lead: the consent evidence excerpt is longer than 280 characters")
	ErrConsentPurposeTooLong   = errors.New("lead: the consent purpose is longer than 200 characters")
	ErrConsentTimeRequired     = errors.New("lead: the consent change needs the time it happened")
	ErrConsentSourceForbidden  = errors.New("lead: this consent source cannot be recorded from here")
	ErrLeadOptedOut            = errors.New("lead: this lead asked not to receive messages")
	ErrOptOutConfirmationStale = errors.New("lead: this opt-out confirmation does not match the lead's current withdrawal")

	ErrConsentEvidenceIsTheWithdrawal = fmt.Errorf("%w: it cites the message the lead used to withdraw", ErrConsentEvidenceInvalid)
)

type ConsentKind string

const (
	ConsentGrant    ConsentKind = "grant"
	ConsentWithdraw ConsentKind = "withdraw"
)

const (
	ConsentButtonReply ConsentSource = "button_reply"
	ConsentWorkflow    ConsentSource = "workflow"
	ConsentAIAgent     ConsentSource = "ai_agent"
	ConsentElo         ConsentSource = "elo"
	ConsentKeyword     ConsentSource = "keyword"
	ConsentAPI         ConsentSource = "api"
)

type ConsentActorType string

const (
	ConsentActorUser     ConsentActorType = "user"
	ConsentActorAgent    ConsentActorType = "agent"
	ConsentActorWorkflow ConsentActorType = "workflow"
	ConsentActorSystem   ConsentActorType = "system"
	ConsentActorLead     ConsentActorType = "lead"
)

type ConsentActor struct {
	Type ConsentActorType `json:"type"`
	ID   string           `json:"id,omitempty"`
}

type EvidenceType string

const (
	EvidenceMessage        EvidenceType = "message"
	EvidenceFormSubmission EvidenceType = "form_submission"
	EvidenceImportLine     EvidenceType = "import_line"
	EvidenceNote           EvidenceType = "note"
	EvidenceButtonReply    EvidenceType = "button_reply"
)

type ConsentEvidence struct {
	Type    EvidenceType `json:"type,omitempty"`
	Ref     string       `json:"ref,omitempty"`
	Excerpt string       `json:"excerpt,omitempty"`
}

type ConsentChange struct {
	ID          string
	Kind        ConsentKind
	Source      ConsentSource
	Actor       ConsentActor
	Purpose     string
	Evidence    ConsentEvidence
	RequestedBy OptOutSource
	Reverts     string
	At          time.Time
}

type ConsentRecord struct {
	Kind               ConsentKind     `json:"kind"`
	Source             ConsentSource   `json:"source"`
	Actor              ConsentActor    `json:"actor"`
	Purpose            string          `json:"purpose,omitempty"`
	Evidence           ConsentEvidence `json:"evidence"`
	At                 time.Time       `json:"at"`
	EventID            string          `json:"eventId,omitempty"`
	ConfirmationSentAt *time.Time      `json:"confirmationSentAt,omitempty"`
	PriorGrant         *Consent        `json:"priorGrant,omitempty"`
	Reverts            string          `json:"reverts,omitempty"`
}

type evidenceRule struct {
	kinds    []EvidenceType
	optional bool
	excerpt  bool
	anyKind  bool
}

type consentRule struct {
	actors    []ConsentActorType
	grant     *evidenceRule
	withdraw  *evidenceRule
	leadAsked bool
}

func needs(kinds ...EvidenceType) *evidenceRule { return &evidenceRule{kinds: kinds} }

var consentRules = map[ConsentSource]consentRule{
	ConsentManual: {actors: []ConsentActorType{ConsentActorUser},
		grant: needs(EvidenceNote), withdraw: &evidenceRule{kinds: []EvidenceType{EvidenceNote, EvidenceMessage, EvidenceButtonReply}, optional: true}},
	ConsentForm: {actors: []ConsentActorType{ConsentActorSystem, ConsentActorLead},
		grant: needs(EvidenceFormSubmission)},
	ConsentImport: {actors: []ConsentActorType{ConsentActorUser, ConsentActorSystem},
		grant: needs(EvidenceImportLine)},
	ConsentConversationRequest: {actors: []ConsentActorType{ConsentActorUser, ConsentActorAgent, ConsentActorLead},
		grant: needs(EvidenceMessage), withdraw: needs(EvidenceMessage), leadAsked: true},
	ConsentButtonReply: {actors: []ConsentActorType{ConsentActorLead},
		grant: needs(EvidenceButtonReply), withdraw: needs(EvidenceButtonReply), leadAsked: true},
	ConsentWorkflow: {actors: []ConsentActorType{ConsentActorWorkflow},
		grant: needs(EvidenceMessage, EvidenceButtonReply), withdraw: needs(EvidenceMessage, EvidenceButtonReply, EvidenceNote)},
	ConsentAIAgent: {actors: []ConsentActorType{ConsentActorAgent},
		grant:    &evidenceRule{kinds: []EvidenceType{EvidenceMessage}, excerpt: true},
		withdraw: &evidenceRule{kinds: []EvidenceType{EvidenceMessage}, excerpt: true}, leadAsked: true},
	ConsentElo: {actors: []ConsentActorType{ConsentActorUser},
		grant: needs(EvidenceNote), withdraw: needs(EvidenceNote)},
	ConsentKeyword: {actors: []ConsentActorType{ConsentActorLead},
		withdraw: needs(EvidenceMessage), leadAsked: true},
	ConsentAPI: {actors: []ConsentActorType{ConsentActorUser, ConsentActorSystem},
		grant: &evidenceRule{anyKind: true}, withdraw: &evidenceRule{anyKind: true}},
}

var knownEvidence = []EvidenceType{EvidenceMessage, EvidenceFormSubmission, EvidenceImportLine, EvidenceNote, EvidenceButtonReply}

func ConsentRules(c ConsentChange) error {
	if c.Kind == ConsentRevertWithdrawal {
		return revertRules(c)
	}
	rule, known := consentRules[c.Source]
	if !known {
		return ErrConsentSourceUnknown
	}
	evidence, err := rule.evidenceFor(c.Kind)
	if err != nil {
		return err
	}
	if c.At.IsZero() {
		return ErrConsentTimeRequired
	}
	if !rule.admits(c.Actor) {
		return ErrConsentActorInvalid
	}
	if err := evidence.check(c.Evidence); err != nil {
		return err
	}
	if err := ValidateConsentPurpose(c.Purpose); err != nil {
		return err
	}
	_, err = rule.optOutSource(c)
	return err
}

func (r consentRule) evidenceFor(kind ConsentKind) (*evidenceRule, error) {
	var evidence *evidenceRule
	switch kind {
	case ConsentGrant:
		evidence = r.grant
	case ConsentWithdraw:
		evidence = r.withdraw
	default:
		return nil, ErrConsentKindInvalid
	}
	if evidence == nil {
		return nil, ErrConsentNotAllowed
	}
	return evidence, nil
}

func (r consentRule) admits(a ConsentActor) bool {
	if !slices.Contains(r.actors, a.Type) {
		return false
	}
	switch a.Type {
	case ConsentActorUser, ConsentActorAgent, ConsentActorWorkflow:
		return strings.TrimSpace(a.ID) != ""
	}
	return true
}

func (e evidenceRule) check(given ConsentEvidence) error {
	if utf8.RuneCountInString(given.Excerpt) > MaxConsentExcerptLength {
		return ErrConsentExcerptTooLong
	}
	if given == (ConsentEvidence{}) {
		if e.optional {
			return nil
		}
		return ErrConsentEvidenceRequired
	}
	if !slices.Contains(knownEvidence, given.Type) {
		return ErrConsentEvidenceInvalid
	}
	if !e.anyKind && !slices.Contains(e.kinds, given.Type) {
		return ErrConsentEvidenceInvalid
	}
	if !given.identified() {
		return ErrConsentEvidenceRequired
	}
	if e.excerpt && strings.TrimSpace(given.Excerpt) == "" {
		return ErrConsentEvidenceRequired
	}
	return nil
}

func (e ConsentEvidence) identified() bool {
	if strings.TrimSpace(e.Ref) != "" {
		return true
	}
	return e.Type == EvidenceNote && strings.TrimSpace(e.Excerpt) != ""
}

func (r consentRule) optOutSource(c ConsentChange) (OptOutSource, error) {
	if c.Kind == ConsentGrant {
		if c.RequestedBy != "" {
			return "", ErrLeadOptOutSourceInvalid
		}
		return "", nil
	}
	if r.leadAsked {
		if c.RequestedBy != "" && c.RequestedBy != OptOutLeadRequest {
			return "", ErrLeadOptOutSourceInvalid
		}
		return OptOutLeadRequest, nil
	}
	requested := c.RequestedBy.OrDefault()
	if !requested.Valid() {
		return "", ErrLeadOptOutSourceInvalid
	}
	return requested, nil
}

func NormalizeConsentPurpose(purpose string) string {
	return strings.Join(strings.Fields(purpose), " ")
}

func ValidateConsentPurpose(purpose string) error {
	if utf8.RuneCountInString(NormalizeConsentPurpose(purpose)) > MaxConsentPurposeLength {
		return ErrConsentPurposeTooLong
	}
	return nil
}

func (l *Lead) ApplyConsentChange(c ConsentChange) (bool, error) {
	if err := ConsentRules(c); err != nil {
		return false, err
	}
	switch c.Kind {
	case ConsentGrant:
		return l.grantConsent(c)
	case ConsentRevertWithdrawal:
		return l.revertWithdrawal(c)
	}
	return l.withdrawConsent(c), nil
}

func (l *Lead) grantConsent(c ConsentChange) (bool, error) {
	if l.OptedOutAt != nil && c.Source == ConsentImport {
		return false, ErrLeadOptedOut
	}
	if l.WhatsAppOptIn != nil && l.OptedOutAt == nil {
		return false, nil
	}
	if l.citesTheWithdrawal(c.Evidence) {
		return false, ErrConsentEvidenceIsTheWithdrawal
	}
	l.WhatsAppOptIn = &Consent{GrantedAt: c.At, Source: c.Source, Purpose: NormalizeConsentPurpose(c.Purpose)}
	l.OptedOutAt, l.OptOutSource = nil, ""
	l.ConsentRecord = c.record()
	return true, nil
}

func (l *Lead) citesTheWithdrawal(evidence ConsentEvidence) bool {
	current := l.ConsentRecord
	if l.OptedOutAt == nil || current == nil || current.Kind != ConsentWithdraw {
		return false
	}
	withdrawnWith := strings.TrimSpace(current.Evidence.Ref)
	return withdrawnWith != "" && strings.TrimSpace(evidence.Ref) == withdrawnWith
}

func (l *Lead) withdrawConsent(c ConsentChange) bool {
	if l.OptedOutAt != nil {
		return false
	}
	source, _ := consentRules[c.Source].optOutSource(c)
	at := c.At
	l.OptedOutAt, l.OptOutSource = &at, source
	record := c.record()
	record.PriorGrant = l.WhatsAppOptIn.clone()
	l.WhatsAppOptIn = nil
	l.ConsentRecord = record
	return true
}

func (c ConsentChange) record() *ConsentRecord {
	return &ConsentRecord{
		Kind: c.Kind, Source: c.Source, Actor: c.Actor, Purpose: NormalizeConsentPurpose(c.Purpose),
		Evidence: c.Evidence, At: c.At, EventID: c.ID,
	}
}

func (l *Lead) WroteAfterOptOut(lastInbound *time.Time) bool {
	return l.OptedOutAt != nil && lastInbound != nil && lastInbound.After(*l.OptedOutAt)
}

func (l *Lead) ClaimOptOutConfirmation(withdrawalID string, at time.Time) error {
	if l.OptedOutAt == nil || strings.TrimSpace(withdrawalID) == "" {
		return ErrOptOutConfirmationStale
	}
	current := l.ConsentRecord
	if current == nil || current.Kind != ConsentWithdraw || current.EventID != withdrawalID || current.ConfirmationSentAt != nil {
		return ErrLeadOptedOut
	}
	claimed := *current
	sent := at
	claimed.ConfirmationSentAt = &sent
	l.ConsentRecord = &claimed
	return nil
}

func ConsentEvent(c ConsentChange, before, after *Lead) recordevent.Event {
	kind, detail := EventConsentWithdrawn, c.eventDetail()
	switch c.Kind {
	case ConsentGrant:
		kind = EventConsentGranted
	case ConsentRevertWithdrawal:
		kind, detail = EventConsentWithdrawalReverted, c.revertDetail(before)
	}
	event := Changes(kind, c.Actor.eventActor(), before, after, nil)
	event.ID = c.ID
	event.Changes = append(event.Changes, recordevent.Change{Field: FieldConsent, After: detail})
	return event
}

func (c ConsentChange) eventDetail() map[string]any {
	return map[string]any{
		"kind": string(c.Kind), "source": string(c.Source),
		"actorType": string(c.Actor.Type), "actorId": c.Actor.ID,
		"purpose": NormalizeConsentPurpose(c.Purpose), "evidenceType": string(c.Evidence.Type), "evidenceRef": c.Evidence.Ref,
	}
}

func (a ConsentActor) eventActor() string {
	switch a.Type {
	case ConsentActorUser:
		return a.ID
	case ConsentActorAgent:
		return actor.FormatAI(a.ID)
	case ConsentActorWorkflow:
		return actor.FormatWorkflow(a.ID)
	}
	return actor.SystemID
}

func SheetConsent(granted bool, userID string, at time.Time) ConsentChange {
	kind := ConsentWithdraw
	evidence := ConsentEvidence{}
	if granted {
		kind, evidence = ConsentGrant, ConsentEvidence{Type: EvidenceNote, Ref: SheetEvidenceRef}
	}
	return ConsentChange{Kind: kind, Source: ConsentManual, Actor: ConsentActor{Type: ConsentActorUser, ID: userID}, Evidence: evidence, At: at}
}

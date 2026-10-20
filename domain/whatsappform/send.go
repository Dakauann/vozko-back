package whatsappform

import (
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

const (
	tokenPrefix      = "vf1_"
	tokenRandomBytes = 16
)

var (
	tokenEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)
	tokenPattern  = regexp.MustCompile(`^vf1_[A-Z2-7]{26}$`)

	ErrSendTransition = errors.New("whatsappform: the send cannot move to that state")
)

func MintToken(random io.Reader) (string, error) {
	raw := make([]byte, tokenRandomBytes)
	if _, err := io.ReadFull(random, raw); err != nil {
		return "", fmt.Errorf("whatsappform: no randomness for the form token: %w", err)
	}
	return tokenPrefix + tokenEncoding.EncodeToString(raw), nil
}

func IsVozkoToken(token string) bool {
	return tokenPattern.MatchString(token)
}

type SendOrigin string

const (
	OriginInbox             SendOrigin = "inbox"
	OriginTemplateStart     SendOrigin = "template_start"
	OriginTemplateScheduled SendOrigin = "template_scheduled"
	OriginTemplateBulk      SendOrigin = "template_bulk"
	OriginTemplate          SendOrigin = "template"
	OriginCampaign          SendOrigin = "campaign"
	OriginWorkflow          SendOrigin = "workflow"
	OriginElo               SendOrigin = "elo"
)

var SendOrigins = []SendOrigin{OriginInbox, OriginTemplateStart, OriginTemplateScheduled, OriginTemplateBulk, OriginTemplate, OriginCampaign, OriginWorkflow, OriginElo}

func (o SendOrigin) Valid() bool {
	for _, known := range SendOrigins {
		if o == known {
			return true
		}
	}
	return false
}

type SendStatus string

const (
	SendPrepared  SendStatus = "prepared"
	SendSent      SendStatus = "sent"
	SendFailed    SendStatus = "failed"
	SendResponded SendStatus = "responded"
)

type Send struct {
	ID              string     `json:"id"`
	WorkspaceID     string     `json:"workspaceId"`
	FormID          string     `json:"formId"`
	VersionID       string     `json:"versionId"`
	PublicationID   string     `json:"publicationId"`
	Token           string     `json:"-"`
	Origin          SendOrigin `json:"origin"`
	OriginRef       string     `json:"originRef,omitempty"`
	EntryID         string     `json:"entryId,omitempty"`
	EntryType       string     `json:"entryType,omitempty"`
	ConversationID  string     `json:"conversationId,omitempty"`
	LeadID          string     `json:"leadId,omitempty"`
	BusinessPhoneID string     `json:"businessPhoneId"`
	ContactNumber   string     `json:"contactNumber"`
	MessageID       string     `json:"messageId,omitempty"`
	TemplateID      string     `json:"templateId,omitempty"`
	WorkflowRunID   string     `json:"workflowRunId,omitempty"`
	SentBy          string     `json:"sentBy,omitempty"`
	Status          SendStatus `json:"status"`
	FailureReason   string     `json:"failureReason,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	SentAt          *time.Time `json:"sentAt,omitempty"`
	RespondedAt     *time.Time `json:"respondedAt,omitempty"`
}

func (s *Send) MarkSent(messageID string, at time.Time) error {
	messageID = strings.TrimSpace(messageID)
	if s.Status != SendPrepared || messageID == "" {
		return fmt.Errorf("%w: %s to sent", ErrSendTransition, s.Status)
	}
	s.Status, s.MessageID, s.SentAt = SendSent, messageID, &at
	return nil
}

func (s *Send) MarkFailed(reason string) error {
	if s.Status != SendPrepared {
		return fmt.Errorf("%w: %s to failed", ErrSendTransition, s.Status)
	}
	s.Status, s.FailureReason = SendFailed, reason
	return nil
}

func (s *Send) MarkResponded(at time.Time) error {
	if s.Status != SendResponded {
		s.Status, s.RespondedAt = SendResponded, &at
	}
	return nil
}

type Inbound struct {
	Token            string
	ContextMessageID string
	BusinessPhoneID  string
	ContactNumber    string
}

type MatchResult string

const (
	MatchByToken          MatchResult = "token"
	MatchByContext        MatchResult = "context"
	UnmatchedNoToken      MatchResult = "unmatched_no_token"
	UnmatchedUnknownToken MatchResult = "unmatched_unknown_token"
	UnmatchedSender       MatchResult = "unmatched_sender"
)

func (m MatchResult) Matched() bool {
	return m == MatchByToken || m == MatchByContext
}

func MatchResponse(send *Send, in Inbound) MatchResult {
	if send == nil {
		if strings.TrimSpace(in.Token) == "" && strings.TrimSpace(in.ContextMessageID) == "" {
			return UnmatchedNoToken
		}
		return UnmatchedUnknownToken
	}
	if send.BusinessPhoneID != in.BusinessPhoneID || !sameContact(send.ContactNumber, in.ContactNumber) {
		return UnmatchedSender
	}
	if in.Token != "" && in.Token == send.Token {
		return MatchByToken
	}
	if in.ContextMessageID != "" && in.ContextMessageID == send.MessageID {
		return MatchByContext
	}
	return UnmatchedUnknownToken
}

type SendCopy struct {
	Header string `json:"header,omitempty"`
	Body   string `json:"body"`
	Footer string `json:"footer,omitempty"`
	CTA    string `json:"cta"`
}

func (c SendCopy) Validate() error {
	if err := conversation.ValidateFlowCopy(c.Header, c.Body, c.Footer, c.CTA); err != nil {
		return fmt.Errorf("%w: %v", ErrCopyInvalid, err)
	}
	return nil
}

func sameContact(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	for _, left := range shared.NinthDigitVariants(a) {
		for _, right := range shared.NinthDigitVariants(b) {
			if left == right {
				return true
			}
		}
	}
	return false
}

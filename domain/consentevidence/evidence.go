package consentevidence

import (
	"fmt"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

var (
	ErrMessageUnknown       = fmt.Errorf("%w: the cited message does not exist", lead.ErrConsentEvidenceInvalid)
	ErrQuestionUnknown      = fmt.Errorf("%w: the cited question does not exist", lead.ErrConsentEvidenceInvalid)
	ErrOutsideConversation  = fmt.Errorf("%w: the cited message belongs to another conversation", lead.ErrConsentEvidenceInvalid)
	ErrNotFromLead          = fmt.Errorf("%w: the cited message was not sent by the lead", lead.ErrConsentEvidenceInvalid)
	ErrNotTheAgentsQuestion = fmt.Errorf("%w: the cited question was not sent by this agent", lead.ErrConsentEvidenceInvalid)
	ErrAnswerBeforeQuestion = fmt.Errorf("%w: the lead's answer is not newer than the agent's question", lead.ErrConsentEvidenceInvalid)
)

type Conversation struct {
	EntryID   string
	EntryType shared.EntryType
}

func (c Conversation) holds(m *conversation.Message) bool {
	return strings.TrimSpace(c.EntryID) != "" && m.EntryID == c.EntryID && m.EntryType == c.EntryType
}

func LeadMessage(c Conversation, cited *conversation.Message) (lead.ConsentEvidence, error) {
	if cited == nil {
		return lead.ConsentEvidence{}, ErrMessageUnknown
	}
	if !c.holds(cited) {
		return lead.ConsentEvidence{}, ErrOutsideConversation
	}
	if !cited.FromCustomer() {
		return lead.ConsentEvidence{}, ErrNotFromLead
	}
	return lead.ConsentEvidence{Type: lead.EvidenceMessage, Ref: cited.ID, Excerpt: Excerpt(cited.Text)}, nil
}

func AnswerToAgentQuestion(c Conversation, agentID string, question, answer *conversation.Message) (lead.ConsentEvidence, error) {
	if question == nil {
		return lead.ConsentEvidence{}, ErrQuestionUnknown
	}
	if !c.holds(question) {
		return lead.ConsentEvidence{}, ErrOutsideConversation
	}
	if !askedBy(question, agentID) {
		return lead.ConsentEvidence{}, ErrNotTheAgentsQuestion
	}
	evidence, err := LeadMessage(c, answer)
	if err != nil {
		return lead.ConsentEvidence{}, err
	}
	if !answer.CreatedAt.After(question.CreatedAt) {
		return lead.ConsentEvidence{}, ErrAnswerBeforeQuestion
	}
	return evidence, nil
}

func askedBy(question *conversation.Message, agentID string) bool {
	agentID = strings.TrimSpace(agentID)
	return agentID != "" && question.SentBy.Kind() == conversation.SenderAI && question.SentBy.Participant() == agentID
}

func Excerpt(text string) string {
	words := []rune(strings.TrimSpace(text))
	if len(words) > lead.MaxConsentExcerptLength {
		words = words[:lead.MaxConsentExcerptLength]
	}
	return strings.TrimSpace(string(words))
}

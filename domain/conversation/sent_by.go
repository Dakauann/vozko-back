package conversation

import (
	"encoding/json"
	"strings"

	"vozko/domain/actor"
)

type SenderKind string

const (
	SenderContact  SenderKind = "contact"
	SenderHuman    SenderKind = SenderKind(actor.KindHuman)
	SenderAI       SenderKind = SenderKind(actor.KindAI)
	SenderWorkflow SenderKind = SenderKind(actor.KindWorkflow)
	SenderCampaign SenderKind = SenderKind(actor.KindCampaign)
	SenderExternal SenderKind = "external"
	SenderSystem   SenderKind = SenderKind(actor.KindSystem)
)

type SentBy struct {
	kind SenderKind
	id   string
}

func SentByContact(handle string) SentBy {
	return SentBy{kind: SenderContact, id: strings.TrimSpace(handle)}
}

func SentByPerson(userID string) SentBy {
	return SentBy{kind: SenderHuman, id: strings.TrimSpace(userID)}
}

func SentByAI(agentID string) SentBy {
	return SentBy{kind: SenderAI, id: actor.FormatAI(agentID)}
}

func SentByWorkflow(workflowID string) SentBy {
	return SentBy{kind: SenderWorkflow, id: actor.FormatWorkflow(workflowID)}
}

func SentByCampaign(campaignID string) SentBy {
	return SentBy{kind: SenderCampaign, id: actor.FormatCampaign(campaignID)}
}

func SentExternally() SentBy {
	return SentBy{kind: SenderExternal}
}

func SentBySystem() SentBy {
	return SentBy{kind: SenderSystem}
}

func RestoreSentBy(kind, id string) SentBy {
	return SentBy{kind: SenderKind(strings.TrimSpace(kind)), id: strings.TrimSpace(id)}
}

func (s SentBy) Kind() SenderKind { return s.kind }

func (s SentBy) ID() string { return s.id }

func (s SentBy) Valid() bool {
	switch s.kind {
	case SenderContact, SenderAI, SenderExternal, SenderSystem:
		return true
	case SenderHuman:
		return s.id != "" && actor.KindOf(s.id) == actor.KindHuman
	case SenderWorkflow:
		return actor.ParseWorkflow(s.id) != ""
	case SenderCampaign:
		return actor.IsCampaign(s.id) && strings.TrimPrefix(s.id, actor.CampaignPrefix) != ""
	}
	return false
}

func (s SentBy) IsContact() bool {
	return s.Valid() && s.kind == SenderContact
}

func (s SentBy) Direction() MessageHistoryDirection {
	if !s.Valid() {
		return MessageDirectionUnknown
	}
	if s.kind == SenderContact {
		return MessageDirectionInbound
	}
	return MessageDirectionOutbound
}

func (s SentBy) Claims(existing SentBy) bool {
	return s.Valid() && s.kind != SenderExternal && existing.kind == SenderExternal
}

func (s SentBy) Participant() string {
	switch s.kind {
	case SenderAI:
		return actor.ParseAI(s.id)
	case SenderWorkflow:
		return actor.ParseWorkflow(s.id)
	case SenderCampaign:
		return strings.TrimPrefix(s.id, actor.CampaignPrefix)
	}
	return s.id
}

func ReplySenderKinds() []SenderKind {
	return []SenderKind{SenderHuman, SenderAI, SenderWorkflow, SenderExternal}
}

func OutboundSenderKinds() []SenderKind {
	return []SenderKind{SenderHuman, SenderAI, SenderWorkflow, SenderCampaign, SenderExternal}
}

func (s SentBy) FromTheBusiness() bool {
	if !s.Valid() {
		return false
	}
	for _, kind := range OutboundSenderKinds() {
		if s.kind == kind {
			return true
		}
	}
	return false
}

type sentByJSON struct {
	Kind SenderKind `json:"kind"`
	ID   string     `json:"id"`
}

func (s SentBy) MarshalJSON() ([]byte, error) {
	return json.Marshal(sentByJSON{Kind: s.kind, ID: s.id})
}

func (s *SentBy) UnmarshalJSON(data []byte) error {
	var decoded sentByJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*s = RestoreSentBy(string(decoded.Kind), decoded.ID)
	return nil
}

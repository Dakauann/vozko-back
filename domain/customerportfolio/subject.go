package customerportfolio

import (
	"strings"

	"vozko/domain/shared"
)

type SubjectKind string

const (
	SubjectLead    SubjectKind = "lead"
	SubjectContact SubjectKind = "contact"
)

var contactChannels = map[shared.EntryType]bool{
	shared.EntryTypeUnofficialWhatsApp: true,
	shared.EntryTypeTelegram:           true,
	shared.EntryTypeInstagram:          true,
	shared.EntryTypeFacebook:           true,
	shared.EntryTypeWebchat:            true,
}

type Subject struct {
	LeadID      string
	ContactType shared.EntryType
	ContactID   string
}

func LeadSubject(leadID string) Subject {
	return Subject{LeadID: leadID}.normalized()
}

func ContactSubject(channel shared.EntryType, contactID string) Subject {
	return Subject{ContactType: channel, ContactID: contactID}.normalized()
}

func (s Subject) Kind() SubjectKind {
	if s.LeadID != "" {
		return SubjectLead
	}
	return SubjectContact
}

func (s Subject) Validate() error {
	n := s.normalized()
	hasLead, hasContact := n.LeadID != "", n.ContactType != "" || n.ContactID != ""
	switch {
	case hasLead && !hasContact:
		return nil
	case !hasLead && contactChannels[n.ContactType] && n.ContactID != "":
		return nil
	default:
		return ErrSubjectInvalid
	}
}

func (s Subject) normalized() Subject {
	return Subject{LeadID: strings.TrimSpace(s.LeadID), ContactType: s.ContactType, ContactID: strings.TrimSpace(s.ContactID)}
}

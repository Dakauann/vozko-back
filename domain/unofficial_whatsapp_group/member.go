package unofficial_whatsapp_group

import (
	"strings"
	"time"

	"vozko/domain/shared"
	"vozko/domain/unofficial_whatsapp"
)

type MemberStatus string

const (
	MemberActive MemberStatus = "active"
	MemberLeft   MemberStatus = "left"
)

func (s MemberStatus) Valid() bool {
	return s == MemberActive || s == MemberLeft
}

type Member struct {
	ID          string                        `json:"id"`
	GroupID     string                        `json:"groupId"`
	WorkspaceID string                        `json:"workspaceId"`
	PersonKey   string                        `json:"personKey"`
	JID         string                        `json:"jid"`
	LID         string                        `json:"lid,omitempty"`
	Phone       string                        `json:"phone,omitempty"`
	DisplayName string                        `json:"displayName,omitempty"`
	Role        unofficial_whatsapp.GroupRole `json:"role"`
	Status      MemberStatus                  `json:"status"`
	JoinedAt    *time.Time                    `json:"joinedAt,omitempty"`
	LeftAt      *time.Time                    `json:"leftAt,omitempty"`
	LeadID      string                        `json:"leadId,omitempty"`
	OurNumber   bool                          `json:"ourNumber"`
}

func (m Member) Hidden() bool {
	return m.Phone == ""
}

func (m Member) IsAdmin() bool {
	return m.Role == unofficial_whatsapp.GroupRoleAdmin || m.Role == unofficial_whatsapp.GroupRoleSuperAdmin
}

func (m Member) Label() string {
	if name := strings.TrimSpace(m.DisplayName); name != "" {
		return name
	}
	if m.Phone != "" {
		return "+" + m.Phone
	}
	return m.JID
}

type MemberEventKind string

const (
	MemberEventJoin  MemberEventKind = "join"
	MemberEventLeave MemberEventKind = "leave"
)

type MemberEvent struct {
	GroupID     string          `json:"groupId"`
	WorkspaceID string          `json:"workspaceId"`
	PersonKey   string          `json:"personKey"`
	Kind        MemberEventKind `json:"kind"`
	OurNumber   bool            `json:"ourNumber"`
	OccurredAt  time.Time       `json:"occurredAt"`
}

func BareJID(jid string) string {
	jid = strings.TrimSpace(jid)
	at := strings.Index(jid, "@")
	if at < 0 {
		return jid
	}
	user, server := jid[:at], jid[at:]
	if colon := strings.Index(user, ":"); colon >= 0 {
		user = user[:colon]
	}
	return user + server
}

func PersonKey(phone, jid string) string {
	if digits := unofficial_whatsapp.NormalizePhone(phone); digits != "" {
		return digits
	}
	return BareJID(jid)
}

type OwnNumbers map[string]struct{}

func NewOwnNumbers(phones, jids []string) OwnNumbers {
	own := OwnNumbers{}
	for _, p := range phones {
		if key := PersonKey(p, ""); key != "" {
			own[key] = struct{}{}
		}
	}
	for _, j := range jids {
		if key := BareJID(j); key != "" {
			own[key] = struct{}{}
		}
		if key := unofficial_whatsapp.PhoneFromJID(j); key != "" {
			own[key] = struct{}{}
		}
	}
	return own
}

func (o OwnNumbers) Contains(key string) bool {
	_, ok := o[key]
	return ok
}

type MemberDiff struct {
	Upserts []Member
	Events  []MemberEvent
}

func (d MemberDiff) Empty() bool {
	return len(d.Upserts) == 0 && len(d.Events) == 0
}

func DiffMembers(
	groupID, workspaceID string,
	current []Member,
	roster []unofficial_whatsapp.GroupParticipant,
	own OwnNumbers,
	now time.Time,
	baseline bool,
) MemberDiff {
	var diff MemberDiff
	present := rosterByPerson(roster)
	if len(present.order) == 0 {
		return diff
	}

	known := make(map[string]Member, len(current))
	for _, m := range current {
		known[m.PersonKey] = m
	}

	event := func(key string, kind MemberEventKind) {
		diff.Events = append(diff.Events, MemberEvent{
			GroupID: groupID, WorkspaceID: workspaceID, PersonKey: key,
			Kind: kind, OurNumber: own.Contains(key), OccurredAt: now,
		})
	}

	for _, key := range present.order {
		p := present.byKey[key]
		existing, seen := known[key]
		next := existing
		if !seen {
			next = Member{GroupID: groupID, WorkspaceID: workspaceID, PersonKey: key}
		}
		changed := next.absorb(p)
		next.OurNumber = own.Contains(key)

		switch {
		case !seen:
			next.Status = MemberActive
			if !baseline {
				joined := now
				next.JoinedAt = &joined
				event(key, MemberEventJoin)
			}
			diff.Upserts = append(diff.Upserts, next)
		case existing.Status != MemberActive:
			joined := now
			next.Status, next.JoinedAt, next.LeftAt = MemberActive, &joined, nil
			if !baseline {
				event(key, MemberEventJoin)
			}
			diff.Upserts = append(diff.Upserts, next)
		case changed || existing.OurNumber != next.OurNumber:
			diff.Upserts = append(diff.Upserts, next)
		}
	}

	for _, m := range current {
		if m.Status != MemberActive {
			continue
		}
		if _, still := present.byKey[m.PersonKey]; still {
			continue
		}
		left := now
		m.Status, m.LeftAt = MemberLeft, &left
		diff.Upserts = append(diff.Upserts, m)
		if !baseline {
			event(m.PersonKey, MemberEventLeave)
		}
	}
	return diff
}

func (m *Member) absorb(p unofficial_whatsapp.GroupParticipant) bool {
	jid := BareJID(p.JID)
	lid := BareJID(p.LID)
	phone := unofficial_whatsapp.NormalizePhone(p.PhoneNumber)
	if phone == "" {
		phone = unofficial_whatsapp.PhoneFromJID(jid)
	}
	name := strings.TrimSpace(p.DisplayName)
	role := p.Role
	if role == "" {
		role = unofficial_whatsapp.GroupRoleMember
	}

	changed := false
	set := func(dst *string, v string) {
		if v != "" && *dst != v {
			*dst, changed = v, true
		}
	}
	set(&m.JID, jid)
	set(&m.LID, lid)
	set(&m.Phone, phone)
	set(&m.DisplayName, name)
	if m.Role != role {
		m.Role, changed = role, true
	}
	return changed
}

type rosterIndex struct {
	order []string
	byKey map[string]unofficial_whatsapp.GroupParticipant
}

func rosterByPerson(roster []unofficial_whatsapp.GroupParticipant) rosterIndex {
	idx := rosterIndex{byKey: make(map[string]unofficial_whatsapp.GroupParticipant, len(roster))}
	for _, p := range roster {
		key := PersonKey(p.PhoneNumber, p.JID)
		if key == "" {
			key = BareJID(p.LID)
		}
		if key == "" {
			continue
		}
		if _, dup := idx.byKey[key]; !dup {
			idx.order = append(idx.order, key)
		}
		idx.byKey[key] = p
	}
	return idx
}

func (m Member) LeadNumbers() []string {
	if m.Phone == "" {
		return nil
	}
	numbers := []string{m.Phone}
	for _, variant := range shared.NinthDigitVariants(m.Phone) {
		if variant != m.Phone {
			numbers = append(numbers, variant)
		}
	}
	return numbers
}

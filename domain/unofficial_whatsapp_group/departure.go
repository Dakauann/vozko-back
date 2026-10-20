package unofficial_whatsapp_group

import (
	"strings"

	uw "vozko/domain/unofficial_whatsapp"
)

type DepartureCause string

const (
	CauseUnknown DepartureCause = ""
	CauseLeft    DepartureCause = "left"
	CauseRemoved DepartureCause = "removed"
)

type DepartureCauses map[string]DepartureCause

func identityKeys(id string) []string {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	if !strings.Contains(id, "@") {
		if digits := uw.NormalizePhone(id); digits != "" {
			return []string{digits}
		}
		return nil
	}
	keys := []string{BareJID(id)}
	if phone := uw.PhoneFromJID(id); phone != "" {
		keys = append(keys, phone)
	}
	return keys
}

func (m Member) identities() map[string]struct{} {
	out := map[string]struct{}{}
	for _, raw := range []string{m.PersonKey, m.JID, m.LID, m.Phone} {
		for _, key := range identityKeys(raw) {
			out[key] = struct{}{}
		}
	}
	return out
}

func (m Member) AnswersTo(ids []string) bool {
	own := m.identities()
	for _, id := range ids {
		for _, key := range identityKeys(id) {
			if _, ok := own[key]; ok {
				return true
			}
		}
	}
	return false
}

func CausesFromChange(change uw.GroupParticipantChange, current []Member) DepartureCauses {
	causes := DepartureCauses{}
	if change.Empty() || len(change.Actors) == 0 {
		return causes
	}
	for _, leaver := range current {
		if leaver.Status != MemberActive || !leaver.AnswersTo(change.Left) {
			continue
		}
		if leaver.AnswersTo(change.Actors) {
			causes[leaver.PersonKey] = CauseLeft
			continue
		}
		for _, other := range current {
			if other.PersonKey != leaver.PersonKey && other.Status == MemberActive && other.AnswersTo(change.Actors) {
				causes[leaver.PersonKey] = CauseRemoved
				break
			}
		}
	}
	return causes
}

func RemovedByUs(participants []string, current []Member) DepartureCauses {
	causes := DepartureCauses{}
	for _, m := range current {
		if m.AnswersTo(participants) {
			causes[m.PersonKey] = CauseRemoved
		}
	}
	return causes
}

func (d *MemberDiff) Attribute(causes DepartureCauses) {
	for i := range d.Upserts {
		m := &d.Upserts[i]
		if m.Status == MemberLeft && causes[m.PersonKey] == CauseRemoved {
			m.Status = MemberRemoved
		}
	}
	for i := range d.Events {
		e := &d.Events[i]
		if e.Kind == MemberEventLeave {
			e.Cause = causes[e.PersonKey]
		}
	}
}

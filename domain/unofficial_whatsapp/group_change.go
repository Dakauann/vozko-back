package unofficial_whatsapp

import (
	"encoding/json"
	"strings"
)

type GroupParticipantChange struct {
	Actors []string
	Left   []string
}

func (c GroupParticipantChange) Empty() bool {
	return len(c.Left) == 0
}

var participantIdentityKeys = []string{"JID", "jid", "LID", "lid", "PhoneNumber", "phoneNumber", "PN", "pn"}

func ParseGroupParticipantChange(data json.RawMessage) GroupParticipantChange {
	root := objectOf(data)
	if root == nil {
		return GroupParticipantChange{}
	}
	event := objectOf(root["event"])
	if event == nil {
		event = root
	}
	var change GroupParticipantChange
	change.Actors = distinctIdentities(
		textOf(event["Sender"]), textOf(event["SenderPN"]),
		textOf(root["sender_pn"]), textOf(root["sender_lid"]),
	)
	var left []string
	var items []json.RawMessage
	if json.Unmarshal(event["Leave"], &items) == nil {
		for _, item := range items {
			if id := textOf(item); id != "" {
				left = append(left, id)
				continue
			}
			participant := objectOf(item)
			for _, key := range participantIdentityKeys {
				left = append(left, textOf(participant[key]))
			}
		}
	}
	change.Left = distinctIdentities(left...)
	return change
}

func groupJIDOfChange(data json.RawMessage) string {
	return textOf(objectOf(objectOf(data)["event"])["JID"])
}

func objectOf(data json.RawMessage) map[string]json.RawMessage {
	var out map[string]json.RawMessage
	if len(data) == 0 || json.Unmarshal(data, &out) != nil {
		return nil
	}
	return out
}

func textOf(data json.RawMessage) string {
	var s string
	if len(data) == 0 || json.Unmarshal(data, &s) != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

func distinctIdentities(values ...string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

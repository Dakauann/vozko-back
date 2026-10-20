package unofficial_whatsapp_group

import "sort"

type SenderBlock string

const (
	SenderReady      SenderBlock = ""
	SenderNotInGroup SenderBlock = "not_in_group"
	SenderCannotPost SenderBlock = "cannot_post"
	SenderOffline    SenderBlock = "offline"
)

func (b SenderBlock) Permanent() bool {
	return b == SenderNotInGroup || b == SenderCannotPost
}

func EligibleSenders(presences []Presence) ([]Presence, SenderBlock) {
	var eligible []Presence
	seated, permitted := false, false
	for _, p := range presences {
		if p.Seat == nil || p.Seat.InstanceID == "" {
			continue
		}
		seated = true
		if p.Seat.CanPost() {
			permitted = true
		}
		if p.CanPost() {
			eligible = append(eligible, p)
		}
	}
	switch {
	case len(eligible) > 0:
		sort.SliceStable(eligible, func(i, j int) bool { return eligible[i].InstanceID() < eligible[j].InstanceID() })
		return eligible, SenderReady
	case !seated:
		return nil, SenderNotInGroup
	case !permitted:
		return nil, SenderCannotPost
	}
	return nil, SenderOffline
}

func ChooseSender(presences []Presence, load func(instanceID string) int64) ([]string, SenderBlock) {
	eligible, block := EligibleSenders(presences)
	if block != SenderReady {
		return nil, block
	}
	ranked := make([]string, 0, len(eligible))
	loads := make(map[string]int64, len(eligible))
	for _, p := range eligible {
		ranked = append(ranked, p.InstanceID())
		loads[p.InstanceID()] = load(p.InstanceID())
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if loads[ranked[i]] != loads[ranked[j]] {
			return loads[ranked[i]] < loads[ranked[j]]
		}
		return ranked[i] < ranked[j]
	})
	return ranked, SenderReady
}

package conversation

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type ReactionDirection string

const (
	ReactionInbound  ReactionDirection = "inbound"
	ReactionOutbound ReactionDirection = "outbound"
)

func (d ReactionDirection) Valid() bool {
	return d == ReactionInbound || d == ReactionOutbound
}

const ReactionsMetadataKey = "reactions"

var ErrReactionDirectionInvalid = errors.New("conversation: reaction direction must be inbound or outbound")

var legacyReactionPrefixes = []string{"instagram", "facebook"}

var reactionMetadataMarker = []byte("reaction")

type Reaction struct {
	Emoji     string            `json:"emoji"`
	Direction ReactionDirection `json:"direction"`
	ActorID   string            `json:"actor_id,omitempty"`
	At        time.Time         `json:"at"`
}

func (r Reaction) sameReactor(other Reaction) bool {
	if r.Direction == ReactionOutbound && other.Direction == ReactionOutbound {
		return true
	}
	return r.Direction == other.Direction && r.ActorID == other.ActorID
}

func (r Reaction) echoes(current Reaction) bool {
	return r.ActorID == "" && r.Emoji != "" && r.Emoji == current.Emoji
}

func ApplyReaction(reactions []Reaction, next Reaction) []Reaction {
	out := make([]Reaction, 0, len(reactions)+1)
	replaced := false
	for _, current := range reactions {
		if !current.sameReactor(next) {
			out = append(out, current)
			continue
		}
		if next.Emoji != "" && !replaced {
			out = append(out, keepKnownActor(current, next))
			replaced = true
		}
	}
	if next.Emoji != "" && !replaced {
		out = append(out, next)
	}
	return out
}

func keepKnownActor(current, next Reaction) Reaction {
	if next.echoes(current) {
		return current
	}
	return next
}

func (m *Message) Reactions() []Reaction {
	if m == nil || !bytes.Contains(m.Metadata, reactionMetadataMarker) {
		return nil
	}
	fields := metadataFields(m.Metadata)
	if stored, ok := fields[ReactionsMetadataKey]; ok {
		var reactions []Reaction
		if err := json.Unmarshal(stored, &reactions); err != nil {
			return nil
		}
		return reactions
	}
	return legacyReactions(fields)
}

func legacyReactions(fields map[string]json.RawMessage) []Reaction {
	var out []Reaction
	for _, prefix := range legacyReactionPrefixes {
		emoji := strings.TrimSpace(metadataString(fields, prefix+"_reaction_emoji"))
		if emoji == "" {
			emoji = strings.TrimSpace(metadataString(fields, prefix+"_reaction"))
		}
		if emoji == "" {
			continue
		}
		at, _ := time.Parse(time.RFC3339, metadataString(fields, prefix+"_reaction_at"))
		out = append(out, Reaction{Emoji: emoji, Direction: ReactionInbound, At: at})
	}
	return out
}

func (m *Message) React(r Reaction) error {
	if !r.Direction.Valid() {
		return ErrReactionDirectionInvalid
	}
	r.Emoji = strings.TrimSpace(r.Emoji)
	updated, err := withMetadataValue(m.Metadata, ReactionsMetadataKey, ApplyReaction(m.Reactions(), r))
	if err != nil {
		return err
	}
	m.Metadata = updated
	return nil
}

package telegram

const reactionTypeEmoji = "emoji"

type MessageReactionUpdated struct {
	Chat        Chat           `json:"chat"`
	MessageID   int64          `json:"message_id"`
	User        *User          `json:"user,omitempty"`
	Date        int64          `json:"date"`
	OldReaction []ReactionType `json:"old_reaction"`
	NewReaction []ReactionType `json:"new_reaction"`
}

type ReactionType struct {
	Type          string `json:"type"`
	Emoji         string `json:"emoji,omitempty"`
	CustomEmojiID string `json:"custom_emoji_id,omitempty"`
}

func (r *MessageReactionUpdated) ShownEmoji() (emoji string, representable bool) {
	if r.User == nil || r.User.ID == 0 {
		return "", false
	}
	current := emojisOf(r.NewReaction)
	if len(current) == 0 {
		return "", len(r.NewReaction) == 0 || len(emojisOf(r.OldReaction)) > 0
	}
	return newestEmoji(current, emojisOf(r.OldReaction)), true
}

func newestEmoji(current, previous []string) string {
	had := make(map[string]bool, len(previous))
	for _, emoji := range previous {
		had[emoji] = true
	}
	for i := len(current) - 1; i >= 0; i-- {
		if !had[current[i]] {
			return current[i]
		}
	}
	return current[len(current)-1]
}

func emojisOf(reactions []ReactionType) []string {
	var out []string
	for _, reaction := range reactions {
		if reaction.Type == reactionTypeEmoji && reaction.Emoji != "" {
			out = append(out, reaction.Emoji)
		}
	}
	return out
}

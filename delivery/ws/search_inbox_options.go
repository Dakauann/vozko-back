package ws

import (
	"strings"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/personalinbox"
)

type searchInboxOptions struct {
	sortOrder string
	personal  personalinbox.View
	labelID   string
}

type searchRefusal struct {
	code    string
	message string
}

func parseSearchInboxOptions(p SearchInboxPayload) (searchInboxOptions, *searchRefusal) {
	sortOrder, err := conversation.ParseInboxSort(p.Sort)
	if err != nil {
		return searchInboxOptions{}, &searchRefusal{"invalid_sort", "sort must be 'recent' or 'waiting'"}
	}
	personal, err := personalinbox.ParseInboxView(p.Personal)
	if err != nil {
		return searchInboxOptions{}, &searchRefusal{"invalid_personal", "personal must be empty or 'pinned'"}
	}
	labelID := strings.TrimSpace(p.LabelID)
	if labelID != "" {
		if _, err := uuid.Parse(labelID); err != nil {
			return searchInboxOptions{}, &searchRefusal{"invalid_label", "label_id must be a label id"}
		}
	}
	return searchInboxOptions{sortOrder: sortOrder, personal: personal, labelID: labelID}, nil
}

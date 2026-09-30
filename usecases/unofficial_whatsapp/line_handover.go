package unofficial_whatsapp

import (
	"context"
	"fmt"
	"log"

	uw "vozko/domain/unofficial_whatsapp"
)

type LineHandover struct {
	lines uw.LineRepository
}

func NewLineHandover(lines uw.LineRepository) *LineHandover {
	return &LineHandover{lines: lines}
}

func (h *LineHandover) Adopt(ctx context.Context, successor *uw.Instance) error {
	if h == nil || h.lines == nil || successor == nil || !successor.SessionLive() {
		return nil
	}
	siblings, err := h.lines.ListSameNumber(ctx, successor)
	if err != nil {
		return fmt.Errorf("unofficial whatsapp: list links of %s: %w", successor.PhoneNumber, err)
	}

	for _, predecessor := range siblings {
		if !uw.InheritsLine(successor, predecessor) {
			if predecessor.Status != uw.StatusConnected {
				log.Printf("[unofficial-whatsapp][line] instance %s: number %s is also on instance %s (%s, department %s vs %s); "+
					"its conversations are not handed over",
					successor.ID, successor.PhoneNumber, predecessor.ID, predecessor.Status,
					departmentLabel(predecessor.DepartmentID), departmentLabel(successor.DepartmentID))
			}
			continue
		}

		moved, err := h.lines.Transfer(ctx, predecessor.ID, successor.ID)
		if err != nil {
			return fmt.Errorf("unofficial whatsapp: hand over %s -> %s: %w", predecessor.ID, successor.ID, err)
		}
		if moved.Moved() {
			log.Printf("[unofficial-whatsapp][line] instance %s took over number %s from instance %s: "+
				"conversations=%d contacts=%d merged_contacts=%d groups=%d kept_on_old=%d",
				successor.ID, successor.PhoneNumber, predecessor.ID,
				moved.Conversations, moved.Contacts, moved.ContactsMerged, moved.Groups, moved.ConversationsKept)
		}
	}
	return nil
}

func departmentLabel(id *string) string {
	if id == nil {
		return "workspace-wide"
	}
	return *id
}

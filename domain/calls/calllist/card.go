package calllist

import (
	"time"

	"vozko/domain/lead"
	"vozko/domain/shared"
)

type LeadCard struct {
	ID          string
	Name        string
	District    string
	City        string
	FamilyCount int
}

func CardOf(l *lead.Lead) LeadCard {
	if l == nil {
		return LeadCard{}
	}
	card := LeadCard{ID: l.ID, Name: l.RealName(), FamilyCount: l.RelativesCount}
	if primary := l.PrimaryAddress(); primary != nil {
		card.District, card.City = primary.Postal.District, primary.Postal.City
	}
	return card
}

type LastInteraction struct {
	EntryID   string
	EntryType shared.EntryType
	At        time.Time
}

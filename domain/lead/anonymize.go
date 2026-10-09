package lead

import (
	"context"
	"strings"
	"time"

	"vozko/domain/recordevent"
	"vozko/domain/shared"
)

const EventAnonymized = recordevent.Kind("anonymized")

type ErasureTarget string

const (
	ErasureRecord                    ErasureTarget = "lead_record"
	ErasurePhones                    ErasureTarget = "lead_phones"
	ErasureAddresses                 ErasureTarget = "lead_addresses"
	ErasureRelations                 ErasureTarget = "lead_relations"
	ErasureEvents                    ErasureTarget = "lead_events"
	ErasureMemories                  ErasureTarget = "lead_memories"
	ErasureCampaignEntries           ErasureTarget = "whatsapp_campaign_entries"
	ErasureUnofficialCampaignEntries ErasureTarget = "unofficial_whatsapp_campaign_entries"
	ErasureCallNumbers               ErasureTarget = "calls"
	ErasureTemplateSendNumbers       ErasureTarget = "whatsapp_template_sends"
	ErasureCallListItems             ErasureTarget = "call_list_items"
	ErasureGeocodeCache              ErasureTarget = "geocode_cache"
)

func ErasureTargets() []ErasureTarget {
	return []ErasureTarget{
		ErasureRecord, ErasurePhones, ErasureAddresses, ErasureRelations, ErasureEvents, ErasureMemories,
		ErasureCampaignEntries, ErasureUnofficialCampaignEntries, ErasureCallNumbers, ErasureTemplateSendNumbers, ErasureCallListItems, ErasureGeocodeCache,
	}
}

type Erasure struct {
	LeadID       string
	Version      int64
	At           time.Time
	Counterparts map[string]RelationTally
	Rows         map[ErasureTarget]int64
}

type Anonymizer interface {
	Anonymize(ctx context.Context, workspaceID, leadID, actorID string, at time.Time) (Erasure, error)
}

type NumberMask struct {
	Masked   string
	Forms    []string
	Identity bool
}

func (l *Lead) NumberMasks() []NumberMask {
	masks := make([]NumberMask, 0, len(l.Phones)+1)
	for _, number := range l.Numbers() {
		formats := NumberFormats(number)
		if len(formats) == 0 {
			continue
		}
		forms := make([]string, 0, 2*len(formats))
		forms = append(forms, formats...)
		for _, format := range formats {
			forms = append(forms, "+"+format)
		}
		masks = append(masks, NumberMask{Masked: shared.MaskContact(number), Forms: forms, Identity: number == l.Number})
	}
	return masks
}

func NumberForms(masks []NumberMask, identity bool) []string {
	var forms []string
	for _, m := range masks {
		if m.Identity == identity {
			forms = append(forms, m.Forms...)
		}
	}
	return forms
}

func ContactForms(masks []NumberMask) []string {
	return NumberForms(masks, false)
}

func ErasableMasks(masks []NumberMask, heldByOthers []string) []NumberMask {
	held := make(map[string]bool, len(heldByOthers))
	for _, form := range heldByOthers {
		held[form] = true
	}
	erasable := make([]NumberMask, 0, len(masks))
	for _, m := range masks {
		if m.Identity || !anyHeld(m.Forms, held) {
			erasable = append(erasable, m)
		}
	}
	return erasable
}

func anyHeld(forms []string, held map[string]bool) bool {
	for _, form := range forms {
		if held[form] || held[strings.TrimPrefix(form, "+")] {
			return true
		}
	}
	return false
}

func (l *Lead) Anonymized() *Lead {
	return &Lead{
		ID: l.ID, WorkspaceID: l.WorkspaceID, Version: l.Version, CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
		Phones: []ContactPhone{}, Addresses: []Address{}, Relations: []Relation{},
	}
}

func AnonymizationEvent(actorID string) recordevent.Event {
	return recordevent.Event{Actor: actorID, Kind: EventAnonymized, Changes: []recordevent.Change{{Field: FieldAnonymized, After: true}}}
}

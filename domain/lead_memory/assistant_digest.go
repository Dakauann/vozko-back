package lead_memory

import (
	"strings"
	"time"

	"vozko/domain/shared"
)

const DigestContentRunes = 300

type MemoryDigest struct {
	MemoryID  string `json:"memory_id"`
	Category  string `json:"category"`
	Content   string `json:"content"`
	By        string `json:"by,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

func DigestMemory(v MemoryView) MemoryDigest {
	d := MemoryDigest{By: v.ActorLabel}
	if v.LeadMemory == nil {
		return d
	}
	d.MemoryID = v.ID
	d.Category = string(v.Category)
	content, cut := shared.TruncateRunes(strings.TrimSpace(v.Content), DigestContentRunes)
	if cut {
		content += "…"
	}
	d.Content = content
	if !v.UpdatedAt.IsZero() {
		d.UpdatedAt = v.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return d
}

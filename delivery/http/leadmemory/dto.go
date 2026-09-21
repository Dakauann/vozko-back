package leadmemory

import (
	"time"

	leadmemory "vozko/domain/lead_memory"
)

type CreateLeadMemoryRequest struct {
	Content  string `json:"content" example:"Prefere boleto a PIX"`
	Category string `json:"category" example:"preference"`
}

type UpdateLeadMemoryRequest struct {
	Content  *string `json:"content,omitempty" example:"Prefere PIX desde ago/2026"`
	Category *string `json:"category,omitempty" example:"preference"`
}

type LeadMemoryResponse struct {
	ID       string `json:"id"`
	LeadID   string `json:"leadId"`
	Category string `json:"category"`
	Content  string `json:"content"`

	ActorKind  string `json:"actorKind"`
	ActorID    string `json:"actorId"`
	ActorLabel string `json:"actorLabel,omitempty"`

	SourceEntryID   *string `json:"sourceEntryId,omitempty"`
	SourceEntryType *string `json:"sourceEntryType,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type LeadMemoryEnvelope struct {
	Memory LeadMemoryResponse `json:"memory"`
}

type LeadMemoryListResponse struct {
	Memories   []LeadMemoryResponse `json:"memories"`
	Total      int64                `json:"total"`
	LeadLinked bool                 `json:"leadLinked"`
}

func toResponse(m *leadmemory.LeadMemory, actorLabel string) LeadMemoryResponse {
	return LeadMemoryResponse{
		ID:              m.ID,
		LeadID:          m.LeadID,
		Category:        string(m.Category),
		Content:         m.Content,
		ActorKind:       string(m.ActorKind),
		ActorID:         m.ActorID,
		ActorLabel:      actorLabel,
		SourceEntryID:   m.SourceEntryID,
		SourceEntryType: m.SourceEntryType,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
}

func toResponses(items []leadmemory.MemoryView) []LeadMemoryResponse {
	out := make([]LeadMemoryResponse, len(items))
	for i, v := range items {
		out[i] = toResponse(v.LeadMemory, v.ActorLabel)
	}
	return out
}

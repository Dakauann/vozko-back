package lead_memory_repository

import (
	"vozko/domain/actor"
	leadmemory "vozko/domain/lead_memory"
	"vozko/infra/database/schema"
)

func toDomain(row *schema.LeadMemory) *leadmemory.LeadMemory {
	if row == nil {
		return nil
	}
	return &leadmemory.LeadMemory{
		ID:              row.ID,
		WorkspaceID:     row.WorkspaceID,
		LeadID:          row.LeadID,
		Category:        leadmemory.Category(row.Category),
		Content:         row.Content,
		ActorKind:       actor.Kind(row.ActorKind),
		ActorID:         row.ActorID,
		SourceEntryID:   row.SourceEntryID,
		SourceEntryType: row.SourceEntryType,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func toDomainSlice(rows []schema.LeadMemory) []*leadmemory.LeadMemory {
	out := make([]*leadmemory.LeadMemory, len(rows))
	for i := range rows {
		out[i] = toDomain(&rows[i])
	}
	return out
}

func fromDomain(m *leadmemory.LeadMemory) schema.LeadMemory {
	return schema.LeadMemory{
		ID:              m.ID,
		WorkspaceID:     m.WorkspaceID,
		LeadID:          m.LeadID,
		Category:        string(m.Category),
		Content:         m.Content,
		ContentNorm:     leadmemory.NormalizeContent(m.Content),
		ActorKind:       string(m.ActorKind),
		ActorID:         m.ActorID,
		SourceEntryID:   m.SourceEntryID,
		SourceEntryType: m.SourceEntryType,
	}
}

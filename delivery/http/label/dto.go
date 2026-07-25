package label

import (
	"time"

	labeldomain "vozko/domain/label"
)

type CreateLabelRequest struct {
	Name  string `json:"name" example:"urgente"`
	Color string `json:"color,omitempty" example:"#ff5630"`
}

type UpdateLabelRequest struct {
	Name  *string `json:"name,omitempty" example:"urgente"`
	Color *string `json:"color,omitempty" example:"#ff5630"`
}

type AssignEntryLabelRequest struct {
	LabelID   string `json:"labelId" example:"lbl_a1b2c3"`
	EntryID   string `json:"entryId" example:"call_a1b2c3"`
	EntryType string `json:"entryType" example:"whatsapp"`
}

type RemoveEntryLabelRequest struct {
	LabelID   string `json:"labelId" example:"lbl_a1b2c3"`
	EntryID   string `json:"entryId" example:"call_a1b2c3"`
	EntryType string `json:"entryType" example:"whatsapp"`
}

type ReorderLabelsRequest struct {
	LabelIDs []string `json:"labelIds" example:"lbl_a1b2c3,lbl_d4e5f6"`
}

type LabelResponse struct {
	ID          string    `json:"id" example:"lbl_a1b2c3"`
	WorkspaceID string    `json:"workspaceId" example:"ws_a1b2c3"`
	Name        string    `json:"name" example:"urgente"`
	Color       string    `json:"color,omitempty" example:"#ff5630"`
	Position    int       `json:"position" example:"0"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type EntryLabelResponse struct {
	ID          string    `json:"id" example:"elb_a1b2c3"`
	LabelID     string    `json:"labelId" example:"lbl_a1b2c3"`
	EntryID     string    `json:"entryId" example:"call_a1b2c3"`
	EntryType   string    `json:"entryType" example:"whatsapp"`
	WorkspaceID string    `json:"workspaceId" example:"ws_a1b2c3"`
	CreatedAt   time.Time `json:"createdAt"`
	LabelName   string    `json:"labelName,omitempty" example:"urgente"`
	LabelColor  string    `json:"labelColor,omitempty" example:"#ff5630"`
}

type MessageResponse struct {
	Message string `json:"message" example:"label removed"`
}

func toLabelResponse(l *labeldomain.Label) *LabelResponse {
	if l == nil {
		return nil
	}
	return &LabelResponse{
		ID:          l.ID,
		WorkspaceID: l.WorkspaceID,
		Name:        l.Name,
		Color:       l.Color,
		Position:    l.Position,
		CreatedAt:   l.CreatedAt,
		UpdatedAt:   l.UpdatedAt,
	}
}

func toLabelResponses(items []*labeldomain.Label) []*LabelResponse {
	if items == nil {
		return nil
	}
	out := make([]*LabelResponse, len(items))
	for i, it := range items {
		out[i] = toLabelResponse(it)
	}
	return out
}

func toEntryLabelResponse(e *labeldomain.EntryLabel) *EntryLabelResponse {
	if e == nil {
		return nil
	}
	return &EntryLabelResponse{
		ID:          e.ID,
		LabelID:     e.LabelID,
		EntryID:     e.EntryID,
		EntryType:   e.EntryType,
		WorkspaceID: e.WorkspaceID,
		CreatedAt:   e.CreatedAt,
		LabelName:   e.LabelName,
		LabelColor:  e.LabelColor,
	}
}

func toEntryLabelResponses(items []*labeldomain.EntryLabel) []*EntryLabelResponse {
	if items == nil {
		return nil
	}
	out := make([]*EntryLabelResponse, len(items))
	for i, it := range items {
		out[i] = toEntryLabelResponse(it)
	}
	return out
}

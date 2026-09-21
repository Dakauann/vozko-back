package pipeline

import (
	"time"

	pipelinedomain "vozko/domain/pipeline"
)

type CreatePipelineRequest struct {
	Name         string `json:"name" example:"Funil de Vendas"`
	ObjectType   string `json:"objectType,omitempty" example:"conversation"`
	DepartmentID string `json:"departmentId,omitempty" example:"dep_a1b2c3"`
	Position     int    `json:"position,omitempty" example:"0"`
	IsDefault    bool   `json:"isDefault,omitempty" example:"false"`

	Stages []StageSeedRequest `json:"stages,omitempty"`

	CopyStagesFromPipelineID string `json:"copyStagesFromPipelineId,omitempty" example:"pipe_a1b2c3"`
}

type StageSeedRequest struct {
	Name        string `json:"name" example:"Triagem"`
	Description string `json:"description,omitempty" example:"Primeiro contato, ainda sem resposta"`
	Color       string `json:"color,omitempty" example:"#3B82F6"`
}

type UpdatePipelineRequest struct {
	Name         *string `json:"name,omitempty" example:"Funil de Suporte"`
	DepartmentID *string `json:"departmentId,omitempty" example:"dep_a1b2c3"`
	Position     *int    `json:"position,omitempty" example:"1"`
	IsDefault    *bool   `json:"isDefault,omitempty" example:"true"`
}

type PipelineResponse struct {
	ID           string    `json:"id" example:"pl_a1b2c3"`
	WorkspaceID  string    `json:"workspaceId" example:"ws_a1b2c3"`
	Name         string    `json:"name" example:"Funil de Vendas"`
	ObjectType   string    `json:"objectType" example:"conversation"`
	StageGroupID string    `json:"stageGroupId,omitempty" example:"sg_a1b2c3"`
	DepartmentID string    `json:"departmentId,omitempty" example:"dep_a1b2c3"`
	Position     int       `json:"position" example:"0"`
	IsDefault    bool      `json:"isDefault" example:"true"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

func toPipelineResponse(p *pipelinedomain.Pipeline) *PipelineResponse {
	if p == nil {
		return nil
	}
	return &PipelineResponse{
		ID:           p.ID,
		WorkspaceID:  p.WorkspaceID,
		Name:         p.Name,
		ObjectType:   string(p.ObjectType),
		StageGroupID: p.StageGroupID,
		DepartmentID: p.DepartmentID,
		Position:     p.Position,
		IsDefault:    p.IsDefault,
		CreatedAt:    p.CreatedAt,
		UpdatedAt:    p.UpdatedAt,
	}
}

func toPipelineResponses(items []*pipelinedomain.Pipeline) []*PipelineResponse {
	if items == nil {
		return nil
	}
	out := make([]*PipelineResponse, len(items))
	for i, it := range items {
		out[i] = toPipelineResponse(it)
	}
	return out
}

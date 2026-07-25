package savedview

import (
	"strings"

	"vozko/domain/crmfilter"
	savedviewdomain "vozko/domain/savedview"
)

type SavedViewRequest struct {
	ObjectType string           `json:"objectType" example:"conversation"`
	PipelineID string           `json:"pipelineId,omitempty" example:"pipe_a1b2c3"`
	Name       string           `json:"name" example:"Meus abertos"`
	Filter     crmfilter.Filter `json:"filter"`
	GroupBy    string           `json:"groupBy,omitempty" example:"stage"`
	GroupByKey string           `json:"groupByKey,omitempty"`
	SortField  string           `json:"sortField,omitempty" example:"createdAt"`
	SortDir    string           `json:"sortDir,omitempty" example:"desc"`
	Columns    []string         `json:"columns,omitempty"`
	Visibility string           `json:"visibility,omitempty" example:"private"`
	IsDefault  bool             `json:"isDefault,omitempty"`
	Position   int              `json:"position,omitempty"`
}

func (req SavedViewRequest) toEntity() *savedviewdomain.SavedView {
	return &savedviewdomain.SavedView{
		ObjectType: savedviewdomain.ObjectType(strings.TrimSpace(req.ObjectType)),
		PipelineID: strings.TrimSpace(req.PipelineID),
		Name:       req.Name,
		Filter:     req.Filter,
		GroupBy:    savedviewdomain.GroupBy(strings.TrimSpace(req.GroupBy)),
		GroupByKey: strings.TrimSpace(req.GroupByKey),
		SortField:  strings.TrimSpace(req.SortField),
		SortDir:    savedviewdomain.SortDir(strings.TrimSpace(req.SortDir)),
		Columns:    req.Columns,
		Visibility: savedviewdomain.Visibility(strings.TrimSpace(req.Visibility)),
		IsDefault:  req.IsDefault,
		Position:   req.Position,
	}
}

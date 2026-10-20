package workflow_repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/infra/cache"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const versionGraphCacheSize = 128

const nextWorkflowVersionSQL = `SELECT COALESCE(MAX(version), 0) + 1 FROM workflow_versions WHERE workflow_id = ?`

const workflowWithPublicationSelect = `workflows.*,
	(SELECT v.published_at FROM workflow_versions v WHERE v.workflow_id = workflows.id AND v.version = workflows.published_version) AS published_at,
	(SELECT v.graph_hash FROM workflow_versions v WHERE v.workflow_id = workflows.id AND v.version = workflows.published_version) AS published_graph_hash`

var versionListColumns = []string{"id", "workflow_id", "version", "type", "graph_hash", "published_at", "published_by", "created_at"}

type versionKey struct {
	workflowID string
	version    int
}

type versionGraphs = cache.LRU[versionKey, *workflow.WorkflowVersion]

func newVersionGraphs() *versionGraphs {
	return cache.NewLRU[versionKey, *workflow.WorkflowVersion](versionGraphCacheSize)
}

func (r *repository) PublishVersion(in workflow.PublishVersion) (*workflow.WorkflowVersion, error) {
	v, err := r.publishOnce(in)
	if errors.Is(err, workflow.ErrVersionNumberTaken) {
		v, err = r.publishOnce(in)
	}
	return v, err
}

func (r *repository) publishOnce(in workflow.PublishVersion) (*workflow.WorkflowVersion, error) {
	graphJSON, err := json.Marshal(in.Graph)
	if err != nil {
		return nil, err
	}
	var published *workflow.WorkflowVersion
	err = r.db.Transaction(func(tx *gorm.DB) error {
		var next int
		if err := tx.Raw(nextWorkflowVersionSQL, in.WorkflowID).Scan(&next).Error; err != nil {
			return err
		}
		row := &schema.WorkflowVersionSchema{
			WorkflowID:  in.WorkflowID,
			Version:     next,
			Type:        string(in.Type),
			Graph:       string(graphJSON),
			GraphHash:   in.GraphHash,
			PublishedAt: time.Now().UTC(),
			PublishedBy: nilIfEmpty(in.PublishedBy),
		}
		if err := tx.Create(row).Error; err != nil {
			if database.IsUniqueViolation(err) {
				return fmt.Errorf("%w: workflow %s version %d", workflow.ErrVersionNumberTaken, in.WorkflowID, next)
			}
			return err
		}
		res := tx.Model(&schema.WorkflowSchema{}).Where("id = ?", in.WorkflowID).
			Updates(map[string]interface{}{"published_version": next, "status": string(workflow.WorkflowStatusActive)})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return workflow.ErrWorkflowNotFound
		}
		if in.RewriteDraft {
			draftJSON, err := json.Marshal(in.Draft)
			if err != nil {
				return err
			}
			if err := tx.Model(&schema.WorkflowSchema{}).Where("id = ? AND version = ?", in.WorkflowID, in.DraftRevision).
				Updates(map[string]interface{}{"graph": string(draftJSON), "type": string(in.Type)}).Error; err != nil {
				return err
			}
		}
		published = mapVersionToDomain(row, nil)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return published, nil
}

func (r *repository) FindVersion(workflowID string, version int) (*workflow.WorkflowVersion, error) {
	key := versionKey{workflowID: workflowID, version: version}
	if cached, ok := r.versions.Get(key); ok {
		return withClonedGraph(cached), nil
	}
	var row schema.WorkflowVersionSchema
	err := r.db.Where("workflow_id = ? AND version = ?", workflowID, version).Limit(1).Find(&row).Error
	if err != nil {
		return nil, err
	}
	if row.ID == "" {
		return nil, nil
	}
	var graph workflow.Graph
	if err := json.Unmarshal([]byte(row.Graph), &graph); err != nil {
		return nil, fmt.Errorf("decode workflow %s version %d: %w", workflowID, version, err)
	}
	v := mapVersionToDomain(&row, &graph)
	r.versions.Put(key, v)
	return withClonedGraph(v), nil
}

func (r *repository) ListVersions(workflowID string, options shared.QueryOptions) (*shared.PaginatedResult[*workflow.WorkflowVersion], error) {
	pagination := shared.NormalizePagination(options.Pagination)
	query := r.db.Model(&schema.WorkflowVersionSchema{}).Where("workflow_id = ?", workflowID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var rows []schema.WorkflowVersionSchema
	if err := query.Select(versionListColumns).Order("version DESC").
		Offset(pagination.Offset()).Limit(pagination.PageSize).Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]*workflow.WorkflowVersion, 0, len(rows))
	for i := range rows {
		items = append(items, mapVersionToDomain(&rows[i], nil))
	}
	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *repository) UpdateStatus(workflowID string, status workflow.WorkflowStatus) error {
	res := r.db.Model(&schema.WorkflowSchema{}).Where("id = ?", workflowID).Update("status", string(status))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return workflow.ErrWorkflowNotFound
	}
	return nil
}

func withClonedGraph(v *workflow.WorkflowVersion) *workflow.WorkflowVersion {
	cp := *v
	if v.Graph != nil {
		g := v.Graph.Clone()
		cp.Graph = &g
	}
	return &cp
}

func mapVersionToDomain(row *schema.WorkflowVersionSchema, graph *workflow.Graph) *workflow.WorkflowVersion {
	return &workflow.WorkflowVersion{
		ID:          row.ID,
		WorkflowID:  row.WorkflowID,
		Version:     row.Version,
		Type:        workflow.WorkflowType(row.Type),
		Graph:       graph,
		GraphHash:   row.GraphHash,
		PublishedAt: row.PublishedAt,
		PublishedBy: derefString(row.PublishedBy),
		CreatedAt:   row.CreatedAt,
	}
}

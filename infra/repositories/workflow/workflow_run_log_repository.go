package workflow_repository

import (
	"encoding/json"

	"vozko/domain/workflow"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type logRepository struct {
	db *gorm.DB
}

func NewWorkflowRunLogRepository(db *gorm.DB) workflow.WorkflowRunLogRepository {
	return &logRepository{db: db}
}

func (r *logRepository) Create(logEntry *workflow.WorkflowRunLog) error {
	inputJSON, _ := json.Marshal(logEntry.Input)
	outputJSON, _ := json.Marshal(logEntry.Output)

	dbL := &schema.WorkflowRunLogSchema{
		ID:       logEntry.ID,
		RunID:    logEntry.RunID,
		NodeID:   logEntry.NodeID,
		NodeType: string(logEntry.NodeType),
		Status:   string(logEntry.Status),
		Input:    string(inputJSON),
		Output:   string(outputJSON),
		Error:    logEntry.Error,
	}

	if err := r.db.Create(dbL).Error; err != nil {
		return err
	}

	logEntry.ID = dbL.ID
	logEntry.ExecutedAt = dbL.ExecutedAt
	return nil
}

func (r *logRepository) FindByRunID(runID string) ([]*workflow.WorkflowRunLog, error) {
	var dbLs []schema.WorkflowRunLogSchema
	if err := r.db.Where("run_id = ?", runID).Order("executed_at ASC").Find(&dbLs).Error; err != nil {
		return nil, err
	}

	result := make([]*workflow.WorkflowRunLog, 0, len(dbLs))
	for i := range dbLs {
		l, err := mapLogToDomain(&dbLs[i])
		if err != nil {
			return nil, err
		}
		result = append(result, l)
	}
	return result, nil
}

func (r *logRepository) CountByRun(runID string) (int64, error) {
	var count int64
	err := r.db.Model(&schema.WorkflowRunLogSchema{}).Where("run_id = ?", runID).Count(&count).Error
	return count, err
}

func mapLogToDomain(dbL *schema.WorkflowRunLogSchema) (*workflow.WorkflowRunLog, error) {
	var input map[string]interface{}
	if dbL.Input != "" {
		if err := json.Unmarshal([]byte(dbL.Input), &input); err != nil {
			return nil, err
		}
	}

	var output map[string]interface{}
	if dbL.Output != "" {
		if err := json.Unmarshal([]byte(dbL.Output), &output); err != nil {
			return nil, err
		}
	}

	return &workflow.WorkflowRunLog{
		ID:         dbL.ID,
		RunID:      dbL.RunID,
		NodeID:     dbL.NodeID,
		NodeType:   workflow.NodeType(dbL.NodeType),
		Status:     workflow.LogStatus(dbL.Status),
		Input:      input,
		Output:     output,
		Error:      dbL.Error,
		ExecutedAt: dbL.ExecutedAt,
	}, nil
}

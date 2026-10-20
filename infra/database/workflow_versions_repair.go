package database

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/workflow"
)

const workflowVersionRepairBatch = 100

const workflowVersionRepairFirstKey = "00000000-0000-0000-0000-000000000000"

const unpublishedRunningWorkflowsSQL = `SELECT id, type, graph FROM workflows
WHERE deleted_at IS NULL AND status IN ('active', 'paused') AND published_version IS NULL AND id > ?
ORDER BY id LIMIT ?`

const insertFirstVersionSQL = `INSERT INTO workflow_versions (id, workflow_id, version, type, graph, graph_hash, published_at, created_at)
VALUES (?, ?, 1, ?, ?, ?, now(), now())
ON CONFLICT (workflow_id, version) DO NOTHING`

const markFirstVersionPublishedSQL = `UPDATE workflows SET published_version = 1 WHERE id = ? AND published_version IS NULL`

type unpublishedWorkflowRow struct {
	ID    string
	Type  string
	Graph *string
}

func publishRunningWorkflows(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("workflow_versions") || !tx.Migrator().HasColumn("workflows", "published_version") {
		return nil
	}
	return publishUnversionedWorkflows(tx)
}

func publishUnversionedWorkflows(tx *gorm.DB) error {
	after := workflowVersionRepairFirstKey
	for {
		var rows []unpublishedWorkflowRow
		if err := tx.Raw(unpublishedRunningWorkflowsSQL, after, workflowVersionRepairBatch).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			after = row.ID
			if err := publishFirstVersion(tx, row); err != nil {
				return fmt.Errorf("workflow %s: %w", row.ID, err)
			}
		}
	}
}

func publishFirstVersion(tx *gorm.DB, row unpublishedWorkflowRow) error {
	raw := "{}"
	if row.Graph != nil && *row.Graph != "" {
		raw = *row.Graph
	}
	var graph workflow.Graph
	if err := json.Unmarshal([]byte(raw), &graph); err != nil {
		slog.Error("workflow.version_repair.graph_unreadable", slog.String("workflow_id", row.ID), slog.String("error", err.Error()))
		return nil
	}
	if graph.HasPins() {
		published, err := json.Marshal(workflow.StripPins(graph))
		if err != nil {
			return err
		}
		raw = string(published)
	}
	if err := tx.Exec(insertFirstVersionSQL, uuid.NewString(), row.ID, row.Type, raw, workflow.GraphHash(&graph)).Error; err != nil {
		return err
	}
	return tx.Exec(markFirstVersionPublishedSQL, row.ID).Error
}

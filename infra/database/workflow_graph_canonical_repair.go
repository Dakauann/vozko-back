package database

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"vozko/domain/workflow"
)

const workflowGraphCanonicalBatch = 100

const draftGraphsWithStructuredConfigSQL = `SELECT id, graph::text AS graph FROM workflows
WHERE id > ? AND graph IS NOT NULL AND jsonb_path_exists(graph, ?::jsonpath)
ORDER BY id LIMIT ?`

const versionGraphsWithStructuredConfigSQL = `SELECT id, graph::text AS graph FROM workflow_versions
WHERE id > ? AND jsonb_path_exists(graph, ?::jsonpath)
ORDER BY id LIMIT ?`

const rewriteDraftGraphSQL = `UPDATE workflows SET graph = ?::jsonb WHERE id = ? AND graph = ?::jsonb`

const rewriteVersionGraphSQL = `UPDATE workflow_versions SET graph = ?::jsonb, graph_hash = ? WHERE id = ? AND graph = ?::jsonb`

type storedGraphRow struct {
	ID    string
	Graph string
}

type storedGraphTable struct {
	name      string
	selectSQL string
	rewrite   func(tx *gorm.DB, row storedGraphRow, canonical []byte) error
}

var storedGraphTables = []storedGraphTable{
	{name: "workflows", selectSQL: draftGraphsWithStructuredConfigSQL, rewrite: rewriteDraftGraph},
	{name: "workflow_versions", selectSQL: versionGraphsWithStructuredConfigSQL, rewrite: rewriteVersionGraph},
}

func canonicalizeWorkflowGraphs(tx *gorm.DB) error {
	for _, table := range storedGraphTables {
		if !tx.Migrator().HasTable(table.name) {
			return nil
		}
	}
	return canonicalizeStoredGraphs(tx)
}

func canonicalizeStoredGraphs(tx *gorm.DB) error {
	return canonicalizeGraphsMatching(tx, workflow.StructuredConfigGraphPath(), workflowGraphCanonicalBatch)
}

func canonicalizeGraphsMatching(tx *gorm.DB, path string, batch int) error {
	for _, table := range storedGraphTables {
		if err := canonicalizeGraphsOf(tx, table, path, batch); err != nil {
			return fmt.Errorf("%s: %w", table.name, err)
		}
	}
	return nil
}

func canonicalizeGraphsOf(tx *gorm.DB, table storedGraphTable, path string, batch int) error {
	after := workflowVersionRepairFirstKey
	for {
		var rows []storedGraphRow
		if err := tx.Raw(table.selectSQL, after, path, batch).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			after = row.ID
			canonical, changed, err := workflow.CanonicalizeGraphJSON([]byte(row.Graph))
			if err != nil {
				slog.Error("workflow.graph_canonical_repair.graph_unreadable", slog.String("table", table.name), slog.String("id", row.ID), slog.String("error", err.Error()))
				continue
			}
			if !changed {
				continue
			}
			if err := table.rewrite(tx, row, canonical); err != nil {
				return fmt.Errorf("%s: %w", row.ID, err)
			}
		}
	}
}

func rewriteDraftGraph(tx *gorm.DB, row storedGraphRow, canonical []byte) error {
	return tx.Exec(rewriteDraftGraphSQL, string(canonical), row.ID, row.Graph).Error
}

func rewriteVersionGraph(tx *gorm.DB, row storedGraphRow, canonical []byte) error {
	var graph workflow.Graph
	if err := json.Unmarshal(canonical, &graph); err != nil {
		return err
	}
	return tx.Exec(rewriteVersionGraphSQL, string(canonical), workflow.GraphHash(&graph), row.ID, row.Graph).Error
}

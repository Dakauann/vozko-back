package lead

import (
	"github.com/lib/pq"
	"gorm.io/gorm"
)

const (
	heldFingerprintsSQL = "SELECT DISTINCT fingerprint FROM lead_addresses WHERE workspace_id = ? AND lead_id = ?"

	lockStoredAnswersSQL = "SELECT c.fingerprint FROM geocode_cache c WHERE c.workspace_id = ? AND c.fingerprint = ANY(?::text[])" +
		" ORDER BY c.fingerprint FOR UPDATE"

	releaseStoredAnswersSQL = "DELETE FROM geocode_cache c WHERE c.workspace_id = ? AND c.fingerprint = ANY(?::text[])" +
		" AND NOT EXISTS (SELECT 1 FROM lead_addresses o JOIN leads holder ON holder.id = o.lead_id AND holder.deleted_at IS NULL" +
		" WHERE o.workspace_id = c.workspace_id AND o.fingerprint = c.fingerprint)"
)

func releaseStoredAnswers(tx *gorm.DB, workspaceID string, fingerprints []string) (int64, error) {
	prints := uniqueSorted(fingerprints)
	if len(prints) == 0 {
		return 0, nil
	}
	var stored []string
	if err := tx.Raw(lockStoredAnswersSQL, workspaceID, pq.StringArray(prints)).Scan(&stored).Error; err != nil {
		return 0, err
	}
	if len(stored) == 0 {
		return 0, nil
	}
	result := tx.Exec(releaseStoredAnswersSQL, workspaceID, pq.StringArray(stored))
	return result.RowsAffected, result.Error
}

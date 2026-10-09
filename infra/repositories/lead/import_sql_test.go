package lead

import "testing"

func TestImportStatementsBindEveryPlaceholder(t *testing.T) {
	cases := map[string]int{
		importIdentitySQL: 2, importHoldersSQL: 6, importIdentityIDsSQL: 2,
		importGetSQL: 2, importSaveSQL: 19, importSaveSQL + importClaimGuardSQL: 20, importClaimSQL: 6, importClaimableSQL: 3,
		importFailStalledSQL: 4, importExpiredSQL: 2, importDeleteSQL: 1, importDeleteIssuesSQL: 1, importDeleteAllLinksSQL: 1, importIssuesSQL: 3,
		importLockSQL: 2, importReloadSQL: 2, importInsertedSQL: 2, importEnrichSQL: 15, importCheckpointSQL: 6,
		importPendingLinksSQL: 2, importDeleteLinksSQL: 2, importInsertRelationsSQL: 8, importUnusedSQL: 3, importMineSQL: 4, importPlacementSQL: len(importPlacementArgs) + 2, importStampFilledSQL: 3,
	}
	for sql, want := range cases {
		if got := countPlaceholders(sql); got != want {
			t.Errorf("%d placeholders, want %d:\n%s", got, want, sql)
		}
	}
}

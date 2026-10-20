package mercadolivre_repository

import "testing"

func TestCatalogRawSQLBindsEveryPlaceholder(t *testing.T) {
	assertPlaceholders(t, "upsert question", upsertQuestionSQL, 16)
	assertPlaceholders(t, "claim listing sync", claimListingSyncSQL, 3)
	assertPlaceholders(t, "delete question batch", deleteQuestionBatchSQL, 2)
}

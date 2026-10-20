package schemascan

import (
	"path/filepath"
	"testing"
)

func TestModelsReadsTableNamesAndFieldsOfTheSchemaPackage(t *testing.T) {
	models, err := Models(filepath.Join("..", "schema"))
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]Model{}
	for _, m := range models {
		found[m.Table] = m
	}
	messages, ok := found["conversation_messages"]
	if !ok || !messages.Has("EntryID", "EntryType") {
		t.Fatalf("conversation_messages = %+v", messages)
	}
	if tiktok, ok := found["tiktok_comments"]; !ok || !tiktok.Has("AccountID", "CommentID") || tiktok.Has("EntryID") {
		t.Fatalf("tiktok_comments = %+v", tiktok)
	}
	if len(models) < 100 {
		t.Fatalf("only %d models found, the scan is broken", len(models))
	}
}

func TestModelsRefusesADirectoryWithoutSources(t *testing.T) {
	if _, err := Models(t.TempDir()); err == nil {
		t.Fatal("an empty directory must be an error, never an empty model list")
	}
}

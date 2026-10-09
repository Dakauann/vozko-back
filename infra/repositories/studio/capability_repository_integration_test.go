package studio_repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/studio"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func sessionReport(workspaceID, userID, sessionID string, frames int64) studio.CapabilityReport {
	return studio.CapabilityReport{
		SessionID: sessionID, WorkspaceID: workspaceID, UserID: userID, Kind: studio.KindVideo, UserAgent: "Mozilla/5.0",
		Capabilities: studio.Capabilities{Backend: studio.BackendWebGL, GPURenderer: "ANGLE (Intel)", Decode: true, EncodeVideo: true, PixelRatio: 2, Cores: 8},
		Usage:        studio.Usage{Frames: frames, BrowserExports: 1, ExportFailures: map[studio.ExportFailure]int64{studio.FailureNoEncoder: 2}},
	}
}

func TestACapabilitySessionKeepsOneRowWithTheLatestCounters(t *testing.T) {
	db := repotest.IsolatedDB(t, "studio_capabilities_upsert", &schema.StudioCapabilityReport{})
	repo := NewCapabilities(db)
	ctx := context.Background()
	ws, user, session := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err := repo.Upsert(ctx, sessionReport(ws, user, session, 10)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Upsert(ctx, sessionReport(ws, user, session, 250)); err != nil {
		t.Fatal(err)
	}
	var rows []schema.StudioCapabilityReport
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Frames != 250 || rows[0].Backend != "webgl" || rows[0].GPURenderer != "ANGLE (Intel)" {
		t.Fatalf("rows = %+v", rows)
	}
	var failures map[string]int64
	if err := json.Unmarshal(rows[0].ExportFailures, &failures); err != nil || failures["no_encoder"] != 2 {
		t.Fatalf("failures = %s", rows[0].ExportFailures)
	}
}

func TestACapabilitySessionCannotBeOverwrittenByAnotherUser(t *testing.T) {
	db := repotest.IsolatedDB(t, "studio_capabilities_owner", &schema.StudioCapabilityReport{})
	repo := NewCapabilities(db)
	ctx := context.Background()
	ws, session := uuid.NewString(), uuid.NewString()
	if err := repo.Upsert(ctx, sessionReport(ws, uuid.NewString(), session, 10)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Upsert(ctx, sessionReport(ws, uuid.NewString(), session, 999)); !errors.Is(err, studio.ErrSessionTaken) {
		t.Fatalf("err = %v", err)
	}
	var row schema.StudioCapabilityReport
	if err := db.First(&row, "session_id = ?", session).Error; err != nil || row.Frames != 10 {
		t.Fatalf("row = %+v err %v", row, err)
	}
}

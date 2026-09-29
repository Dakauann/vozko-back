package sip_trunk_repository

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/sip_trunk"
	"vozko/infra/crypto/pii"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestMain(m *testing.M) {
	key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("S", 32)))
	env := func(k string) string {
		if k == "VOZKO_PII_KEK_V1" || k == "VOZKO_PII_BLIND_INDEX_KEY" {
			return key
		}
		return ""
	}
	svc, err := pii.LoadFromEnviron([]string{"VOZKO_PII_KEK_V1=" + key, "VOZKO_PII_BLIND_INDEX_KEY=" + key}, env)
	if err != nil {
		panic(err)
	}
	piigorm.SetService(svc)
	os.Exit(m.Run())
}

func newTrunk(workspaceID string) *sip_trunk.SIPTrunk {
	return &sip_trunk.SIPTrunk{
		WorkspaceID: workspaceID,
		Name:        "Main",
		TrunkType:   sip_trunk.TrunkTypeBidirectional,
		Host:        "sip.provider.com",
		Port:        5080,
		Transport:   sip_trunk.TransportUDP,
		Username:    "1001",
		Password:    "s3cret",
		Enabled:     true,
		Settings: sip_trunk.Settings{
			Codecs:                []sip_trunk.Codec{sip_trunk.CodecPCMA},
			DialPlan:              sip_trunk.DialPlan{StripPrefix: "+55"},
			InboundAllowedSources: []string{"203.0.113.0/24"},
		},
	}
}

func TestRepositoryRoundTripsTrunksAndEncryptsThePassword(t *testing.T) {
	db := repotest.IsolatedDB(t, "sip_trunk", &schema.SIPTrunk{})
	repo := NewRepository(db)
	ctx := context.Background()
	workspaceID := uuid.NewString()

	trunk := newTrunk(workspaceID)
	if err := repo.Create(ctx, trunk); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if trunk.ID == "" || trunk.RegistrationStatus != sip_trunk.RegistrationStatusUnregistered {
		t.Fatalf("Create() left ID %q status %q", trunk.ID, trunk.RegistrationStatus)
	}

	var raw []byte
	if err := db.Raw("SELECT password FROM sip_trunks WHERE id = ?", trunk.ID).Row().Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "s3cret") {
		t.Fatal("password stored in plaintext")
	}

	found, err := repo.FindInWorkspace(ctx, workspaceID, trunk.ID)
	if err != nil {
		t.Fatalf("FindInWorkspace() error = %v", err)
	}
	if found.Password != "s3cret" || found.Port != 5080 || found.Settings.DialPlan.StripPrefix != "+55" || found.Settings.Codecs[0] != sip_trunk.CodecPCMA {
		t.Fatalf("FindInWorkspace() = %+v", found)
	}
}

func TestRepositoryScopesEveryReadAndWriteToTheWorkspace(t *testing.T) {
	db := repotest.IsolatedDB(t, "sip_trunk", &schema.SIPTrunk{})
	repo := NewRepository(db)
	ctx := context.Background()
	owner, stranger := uuid.NewString(), uuid.NewString()

	trunk := newTrunk(owner)
	if err := repo.Create(ctx, trunk); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindInWorkspace(ctx, stranger, trunk.ID); !errors.Is(err, sip_trunk.ErrTrunkNotFound) {
		t.Fatalf("FindInWorkspace() from another workspace = %v, want ErrTrunkNotFound", err)
	}
	listed, err := repo.ListByWorkspace(ctx, stranger)
	if err != nil || len(listed) != 0 {
		t.Fatalf("ListByWorkspace() from another workspace = %v, %v, want empty", listed, err)
	}
	hijack := *trunk
	hijack.WorkspaceID = stranger
	hijack.Name = "Hijacked"
	if err := repo.Update(ctx, &hijack); !errors.Is(err, sip_trunk.ErrTrunkNotFound) {
		t.Fatalf("Update() from another workspace = %v, want ErrTrunkNotFound", err)
	}
	if err := repo.Delete(ctx, stranger, trunk.ID); !errors.Is(err, sip_trunk.ErrTrunkNotFound) {
		t.Fatalf("Delete() from another workspace = %v, want ErrTrunkNotFound", err)
	}
	still, err := repo.FindInWorkspace(ctx, owner, trunk.ID)
	if err != nil || still.Name != "Main" {
		t.Fatalf("owner's trunk after foreign writes = %+v, %v", still, err)
	}
}

func TestRepositoryUpdateKeepsThePasswordWhenOmitted(t *testing.T) {
	db := repotest.IsolatedDB(t, "sip_trunk", &schema.SIPTrunk{})
	repo := NewRepository(db)
	ctx := context.Background()
	trunk := newTrunk(uuid.NewString())
	if err := repo.Create(ctx, trunk); err != nil {
		t.Fatal(err)
	}
	trunk.Password = ""
	trunk.Name = "Renamed"
	trunk.Enabled = false
	if err := repo.Update(ctx, trunk); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	found, err := repo.FindInWorkspace(ctx, trunk.WorkspaceID, trunk.ID)
	if err != nil || found.Password != "s3cret" || found.Name != "Renamed" || found.Enabled {
		t.Fatalf("after Update() = %+v, %v", found, err)
	}
}

func TestRepositoryEngineQueriesAndStatus(t *testing.T) {
	db := repotest.IsolatedDB(t, "sip_trunk", &schema.SIPTrunk{})
	repo := NewRepository(db)
	ctx := context.Background()
	enabled := newTrunk(uuid.NewString())
	disabled := newTrunk(uuid.NewString())
	disabled.Enabled = false
	for _, trunk := range []*sip_trunk.SIPTrunk{enabled, disabled} {
		if err := repo.Create(ctx, trunk); err != nil {
			t.Fatal(err)
		}
	}
	found, err := repo.FindEnabled(ctx)
	if err != nil || len(found) != 1 || found[0].ID != enabled.ID {
		t.Fatalf("FindEnabled() = %v, %v, want only the enabled trunk", found, err)
	}
	longError := strings.Repeat("x", 900)
	if err := repo.UpdateStatus(ctx, enabled.ID, sip_trunk.RegistrationStatusFailed, longError); err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}
	after, _ := repo.FindInWorkspace(ctx, enabled.WorkspaceID, enabled.ID)
	if after.RegistrationStatus != sip_trunk.RegistrationStatusFailed || len(after.LastError) != 500 {
		t.Fatalf("status = %s, error length %d, want FAILED and a truncated error", after.RegistrationStatus, len(after.LastError))
	}
	if err := repo.Delete(ctx, enabled.WorkspaceID, enabled.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if found, _ := repo.FindEnabled(ctx); len(found) != 0 {
		t.Fatalf("FindEnabled() after delete = %v, want none", found)
	}
}

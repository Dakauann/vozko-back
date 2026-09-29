package sip_trunk_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/sip_trunk"
	"vozko/domain/sip_trunk/siptrunktest"
)

const (
	ownerWorkspace    = "ws-owner"
	strangerWorkspace = "ws-stranger"
)

func ownedTrunk() *sip_trunk.SIPTrunk {
	return &sip_trunk.SIPTrunk{
		ID:          "trunk-owned",
		WorkspaceID: ownerWorkspace,
		Name:        "Main",
		TrunkType:   sip_trunk.TrunkTypeBidirectional,
		Host:        "sip.provider.com",
		Transport:   sip_trunk.TransportUDP,
		Username:    "1001",
		Password:    "s3cret",
		Enabled:     true,
	}
}

func createInput() CreateTrunkInput {
	return CreateTrunkInput{
		WorkspaceID: ownerWorkspace,
		Name:        " Main line ",
		Host:        " sip.provider.com ",
		Username:    "1001",
		Password:    "s3cret",
		Enabled:     true,
	}
}

func TestCreateTrunkPersistsAndRegistersAnEnabledTrunk(t *testing.T) {
	repo, engine := siptrunktest.NewMemoryRepository(), newFakeEngine()
	uc := NewCreateTrunkUseCase(repo, engine)

	trunk, err := uc.Execute(context.Background(), createInput())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	stored, ok := repo.Stored(trunk.ID)
	if !ok || stored.Name != "Main line" || stored.Host != "sip.provider.com" {
		t.Fatalf("stored trunk = %+v, want trimmed fields", stored)
	}
	if stored.Transport != sip_trunk.TransportUDP || stored.TrunkType != sip_trunk.TrunkTypeBidirectional {
		t.Fatalf("defaults not applied: transport %q type %q", stored.Transport, stored.TrunkType)
	}
	if len(engine.registered) != 1 || engine.registered[0] != trunk.ID {
		t.Fatalf("engine registered %v, want the new trunk", engine.registered)
	}
	if trunk.RegistrationStatus != sip_trunk.RegistrationStatusRegistering {
		t.Fatalf("returned status = %s, want the live engine status", trunk.RegistrationStatus)
	}
}

func TestCreateTrunkDoesNotRegisterADisabledTrunk(t *testing.T) {
	repo, engine := siptrunktest.NewMemoryRepository(), newFakeEngine()
	input := createInput()
	input.Enabled = false
	if _, err := NewCreateTrunkUseCase(repo, engine).Execute(context.Background(), input); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(engine.registered) != 0 {
		t.Fatalf("engine registered %v, want nothing for a disabled trunk", engine.registered)
	}
}

func TestCreateTrunkRejectsInvalidInputBeforePersisting(t *testing.T) {
	repo, engine := siptrunktest.NewMemoryRepository(), newFakeEngine()
	input := createInput()
	input.Password = ""
	if _, err := NewCreateTrunkUseCase(repo, engine).Execute(context.Background(), input); !errors.Is(err, sip_trunk.ErrCredentialsRequired) {
		t.Fatalf("Execute() error = %v, want ErrCredentialsRequired", err)
	}
	if listed, _ := repo.ListByWorkspace(context.Background(), ownerWorkspace); len(listed) != 0 || len(engine.registered) != 0 {
		t.Fatal("an invalid trunk was persisted or registered")
	}
}

func TestUpdateTrunkAppliesChangesKeepsThePasswordAndRefreshesTheEngine(t *testing.T) {
	repo, engine := siptrunktest.NewMemoryRepository(ownedTrunk()), newFakeEngine()
	name, port := "Renamed", 5080
	trunk, err := NewUpdateTrunkUseCase(repo, engine).Execute(context.Background(), UpdateTrunkInput{
		WorkspaceID: ownerWorkspace,
		ID:          "trunk-owned",
		Name:        &name,
		Port:        &port,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if trunk.Name != "Renamed" || trunk.Port != 5080 {
		t.Fatalf("updated trunk = %+v", trunk)
	}
	if len(engine.refreshed) != 1 || engine.refreshed[0].Password != "s3cret" {
		t.Fatalf("engine refreshed %v, want the trunk with its stored password", engine.refreshed)
	}
}

func TestUpdateTrunkRejectsInvalidChanges(t *testing.T) {
	repo, engine := siptrunktest.NewMemoryRepository(ownedTrunk()), newFakeEngine()
	transport := sip_trunk.Transport("TLS")
	_, err := NewUpdateTrunkUseCase(repo, engine).Execute(context.Background(), UpdateTrunkInput{WorkspaceID: ownerWorkspace, ID: "trunk-owned", Transport: &transport})
	if !errors.Is(err, sip_trunk.ErrUnsupportedTransport) {
		t.Fatalf("Execute() error = %v, want ErrUnsupportedTransport", err)
	}
	if len(engine.refreshed) != 0 {
		t.Fatal("engine refreshed with an invalid trunk")
	}
}

func TestDeleteTrunkUnregistersThenDeletes(t *testing.T) {
	repo, engine := siptrunktest.NewMemoryRepository(ownedTrunk()), newFakeEngine()
	if err := NewDeleteTrunkUseCase(repo, engine).Execute(context.Background(), ownerWorkspace, "trunk-owned"); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if _, ok := repo.Stored("trunk-owned"); ok {
		t.Fatal("trunk still stored")
	}
	if len(engine.unregisterd) != 1 {
		t.Fatalf("engine unregistered %v, want the trunk", engine.unregisterd)
	}
}

func TestListAndGetOverlayTheLiveStatus(t *testing.T) {
	repo, engine := siptrunktest.NewMemoryRepository(ownedTrunk()), newFakeEngine()
	engine.statuses["trunk-owned"] = sip_trunk.SIPTrunkStatusUpdate{TrunkID: "trunk-owned", Status: sip_trunk.RegistrationStatusRegistered}

	trunks, err := NewListTrunksUseCase(repo, engine).Execute(context.Background(), ownerWorkspace)
	if err != nil || len(trunks) != 1 || trunks[0].RegistrationStatus != sip_trunk.RegistrationStatusRegistered {
		t.Fatalf("List() = %v, %v, want the trunk with the live status", trunks, err)
	}
	trunk, err := NewGetTrunkUseCase(repo, engine).Execute(context.Background(), ownerWorkspace, "trunk-owned")
	if err != nil || trunk.RegistrationStatus != sip_trunk.RegistrationStatusRegistered {
		t.Fatalf("Get() = %+v, %v", trunk, err)
	}
}

func TestCallOperationsOnlyReachTrunksOfTheCallerWorkspace(t *testing.T) {
	repo, engine := siptrunktest.NewMemoryRepository(ownedTrunk()), newFakeEngine()
	engine.calls["trunk-owned"] = []sip_trunk.ActiveCall{{ID: "call-1", TrunkID: "trunk-owned"}}
	ctx := context.Background()

	if err := NewHangupCallUseCase(repo, engine).Execute(ctx, strangerWorkspace, "trunk-owned", "call-1"); !errors.Is(err, sip_trunk.ErrTrunkNotFound) {
		t.Fatalf("Hangup() from another workspace = %v, want ErrTrunkNotFound", err)
	}
	if len(engine.hangups) != 0 {
		t.Fatal("a foreign workspace reached the engine")
	}
	if _, err := NewListCallsUseCase(repo, engine).Execute(ctx, strangerWorkspace, "trunk-owned"); !errors.Is(err, sip_trunk.ErrTrunkNotFound) {
		t.Fatalf("ListCalls() from another workspace = %v, want ErrTrunkNotFound", err)
	}
	for _, uc := range []func() error{
		func() error {
			return NewDeleteTrunkUseCase(repo, engine).Execute(ctx, strangerWorkspace, "trunk-owned")
		},
		func() error {
			_, err := NewGetTrunkUseCase(repo, engine).Execute(ctx, strangerWorkspace, "trunk-owned")
			return err
		},
		func() error {
			name := "x"
			_, err := NewUpdateTrunkUseCase(repo, engine).Execute(ctx, UpdateTrunkInput{WorkspaceID: strangerWorkspace, ID: "trunk-owned", Name: &name})
			return err
		},
	} {
		if err := uc(); !errors.Is(err, sip_trunk.ErrTrunkNotFound) {
			t.Fatalf("foreign trunk operation = %v, want ErrTrunkNotFound", err)
		}
	}
	if len(engine.unregisterd) != 0 || len(engine.refreshed) != 0 {
		t.Fatal("a foreign workspace changed the engine state")
	}

	if err := NewHangupCallUseCase(repo, engine).Execute(ctx, ownerWorkspace, "trunk-owned", "call-1"); err != nil {
		t.Fatalf("Hangup() by the owner = %v", err)
	}
	if len(engine.hangups) != 1 || engine.hangups[0] != "trunk-owned/call-1" {
		t.Fatalf("engine hangups = %v", engine.hangups)
	}
	calls, err := NewListCallsUseCase(repo, engine).Execute(ctx, ownerWorkspace, "trunk-owned")
	if err != nil || len(calls) != 1 {
		t.Fatalf("ListCalls() by the owner = %v, %v", calls, err)
	}
}

func TestCreateTrunkReportsAnEngineFailureOnTheSavedTrunk(t *testing.T) {
	repo, engine := siptrunktest.NewMemoryRepository(), newFakeEngine()
	engine.registerErr = sip_trunk.ErrEngineNotRunning
	trunk, err := NewCreateTrunkUseCase(repo, engine).Execute(context.Background(), createInput())
	if err != nil {
		t.Fatalf("Execute() error = %v, want the saved trunk", err)
	}
	if trunk.RegistrationStatus != sip_trunk.RegistrationStatusFailed || trunk.LastError != sip_trunk.ErrEngineNotRunning.Error() {
		t.Fatalf("returned trunk status %s %q, want FAILED with the engine error", trunk.RegistrationStatus, trunk.LastError)
	}
}

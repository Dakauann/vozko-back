package stage_usecase

import (
	"errors"
	"testing"

	"vozko/domain/shared"
	"vozko/domain/stage"
)

type entryAccessStub struct {
	allow bool
	asked []string
}

func (a *entryAccessStub) CanAccessEntry(userID, workspaceID, entryID, entryType string, isAdmin bool) bool {
	who := userID
	if isAdmin {
		who += "(admin)"
	}
	a.asked = append(a.asked, who+"|"+workspaceID+"|"+entryID+"|"+entryType)
	return a.allow
}

type assignStub struct {
	calls []stage.AssignEntryStageInput
}

func (a *assignStub) Execute(workspaceID string, in stage.AssignEntryStageInput) (*stage.EntryStage, error) {
	a.calls = append(a.calls, in)
	return &stage.EntryStage{StageID: in.StageID, EntryID: in.EntryID}, nil
}

func moveInput(entryType string) stage.AssignEntryStageInput {
	return stage.AssignEntryStageInput{StageID: "s1", EntryID: "e1", EntryType: entryType, ActorID: "someone-else"}
}

func TestMoveRefusesAConversationTheMoverCannotSee(t *testing.T) {
	access, assign := &entryAccessStub{allow: false}, &assignStub{}
	_, err := NewMoveEntryStageUseCase(access, assign).Execute("ws1", shared.Person{UserID: "u1"}, moveInput("whatsapp"))
	if !errors.Is(err, stage.ErrEntryAccess) || len(assign.calls) != 0 {
		t.Fatalf("err = %v after %d moves", err, len(assign.calls))
	}
	if access.asked[0] != "u1|ws1|e1|whatsapp" {
		t.Fatalf("asked %v", access.asked)
	}
}

func TestMoveRecordsTheRealMoverAsTheActor(t *testing.T) {
	access, assign := &entryAccessStub{allow: true}, &assignStub{}
	if _, err := NewMoveEntryStageUseCase(access, assign).Execute("ws1", shared.Person{UserID: "u1", SystemAdmin: true}, moveInput("instagram")); err != nil {
		t.Fatal(err)
	}
	if assign.calls[0].ActorID != "u1" || access.asked[0] != "u1(admin)|ws1|e1|instagram" {
		t.Fatalf("calls %+v asked %v", assign.calls, access.asked)
	}
}

func TestMoveFailsClosedWithoutAMoverOrAnAccessCheck(t *testing.T) {
	assign := &assignStub{}
	if _, err := NewMoveEntryStageUseCase(&entryAccessStub{allow: true}, assign).Execute("ws1", shared.Person{}, moveInput("whatsapp")); !errors.Is(err, stage.ErrEntryAccess) {
		t.Fatalf("no mover: err = %v", err)
	}
	if _, err := NewMoveEntryStageUseCase(nil, assign).Execute("ws1", shared.Person{UserID: "u1"}, moveInput("whatsapp")); !errors.Is(err, stage.ErrEntryAccess) {
		t.Fatalf("no access check: err = %v", err)
	}
	if len(assign.calls) != 0 {
		t.Fatal("moved without authorization")
	}
}

func TestMoveRefusesAnUnknownEntryType(t *testing.T) {
	access, assign := &entryAccessStub{allow: true}, &assignStub{}
	if _, err := NewMoveEntryStageUseCase(access, assign).Execute("ws1", shared.Person{UserID: "u1"}, moveInput("support")); !errors.Is(err, stage.ErrEntryAccess) {
		t.Fatalf("err = %v", err)
	}
	if len(assign.calls) != 0 {
		t.Fatal("moved an unknown entry type")
	}
}

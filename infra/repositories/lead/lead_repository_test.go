package lead

import (
	"errors"
	"testing"

	"vozko/domain/lead"
	"vozko/domain/shared"
)

func newNilRepo() *repository {
	return &repository{db: nil}
}

func TestRepository_WorkspaceRequiredGuards(t *testing.T) {
	r := newNilRepo()

	if _, err := r.FindByID("", "id"); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Errorf("FindByID empty ws = %v", err)
	}
	if _, err := r.FindByID("ws", ""); !errors.Is(err, lead.ErrLeadRequired) {
		t.Errorf("FindByID empty id = %v", err)
	}

	if _, err := r.FindByNumber("", "5511987654321"); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Errorf("FindByNumber empty ws = %v", err)
	}
	if _, err := r.FindByNumber("ws", "abc"); !errors.Is(err, lead.ErrLeadInvalid) {
		t.Errorf("FindByNumber invalid number = %v", err)
	}

	if _, err := r.FindByIDs("", []string{"a"}); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Errorf("FindByIDs empty ws = %v", err)
	}
	if got, err := r.FindByIDs("ws", nil); err != nil || len(got) != 0 {
		t.Errorf("FindByIDs empty ids = %v, %v", got, err)
	}

	channel := lead.LeadUpdate{Source: lead.SourceChannel}
	if _, _, err := r.FindOrCreate("", "5511987654321", channel); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Errorf("FindOrCreate empty ws = %v", err)
	}
	if _, _, err := r.FindOrCreate("ws", "abc", channel); !errors.Is(err, lead.ErrLeadInvalid) {
		t.Errorf("FindOrCreate invalid number = %v", err)
	}

	if _, err := r.FindOrCreateMany("", []lead.BulkLeadInput{{Source: lead.SourceImport, Number: "5511987654321"}}); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Errorf("FindOrCreateMany empty ws = %v", err)
	}
	if got, err := r.FindOrCreateMany("ws", nil); err != nil || len(got) != 0 {
		t.Errorf("FindOrCreateMany empty input = %v, %v", got, err)
	}
	if got, err := r.FindOrCreateMany("ws", []lead.BulkLeadInput{{Source: lead.SourceImport, Number: "abc"}}); err != nil || len(got) != 0 {
		t.Errorf("FindOrCreateMany invalid only = %v, %v", got, err)
	}

	if err := r.Delete("", "id"); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Errorf("Delete empty ws = %v", err)
	}
	if err := r.Delete("ws", ""); !errors.Is(err, lead.ErrLeadRequired) {
		t.Errorf("Delete empty id = %v", err)
	}

	if _, err := r.List(lead.ListLeadsInput{}); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Errorf("List empty ws = %v", err)
	}
}

func TestNewRepository(t *testing.T) {
	if NewRepository(nil) == nil {
		t.Fatal("NewRepository returned nil")
	}
}

func TestList_NormalizesPaginationBeforeQueryError(t *testing.T) {
	r := newNilRepo()

	_, err := r.List(lead.ListLeadsInput{Options: shared.QueryOptions{}})
	if !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Errorf("List = %v, want ErrLeadWorkspaceRequired", err)
	}
}

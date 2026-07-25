package lead

import (
	"errors"
	"testing"

	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

func newNilRepo() *repository {
	return &repository{db: nil}
}

func TestRepository_WorkspaceRequiredGuards(t *testing.T) {
	r := newNilRepo()

	if err := r.Create(nil); !errors.Is(err, lead.ErrLeadRequired) {
		t.Errorf("Create(nil) = %v, want ErrLeadRequired", err)
	}

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

	if _, _, err := r.FindOrCreate("", "5511987654321", lead.LeadUpdate{}); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Errorf("FindOrCreate empty ws = %v", err)
	}
	if _, _, err := r.FindOrCreate("ws", "abc", lead.LeadUpdate{}); !errors.Is(err, lead.ErrLeadInvalid) {
		t.Errorf("FindOrCreate invalid number = %v", err)
	}

	if _, err := r.FindOrCreateMany("", []lead.BulkLeadInput{{Number: "5511987654321"}}); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Errorf("FindOrCreateMany empty ws = %v", err)
	}
	if got, err := r.FindOrCreateMany("ws", nil); err != nil || len(got) != 0 {
		t.Errorf("FindOrCreateMany empty input = %v, %v", got, err)
	}
	if got, err := r.FindOrCreateMany("ws", []lead.BulkLeadInput{{Number: "abc"}}); err != nil || len(got) != 0 {
		t.Errorf("FindOrCreateMany invalid only = %v, %v", got, err)
	}

	if err := r.Update("", "id", lead.LeadUpdate{}); !errors.Is(err, lead.ErrLeadWorkspaceRequired) {
		t.Errorf("Update empty ws = %v", err)
	}
	if err := r.Update("ws", "", lead.LeadUpdate{}); !errors.Is(err, lead.ErrLeadRequired) {
		t.Errorf("Update empty id = %v", err)
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

func TestSchemaMapping_PreservesWorkspaceID(t *testing.T) {
	age := 30
	src := &lead.Lead{
		ID:                "id-1",
		WorkspaceID:       "ws-1",
		Number:            "5511987654321",
		Name:              "Jane",
		ProfilePictureURL: "pic.png",
		Age:               &age,
	}
	s := toSchema(src)
	if s.WorkspaceID != "ws-1" || s.ID != "id-1" || s.Number != "5511987654321" || s.Name != "Jane" || s.ProfilePictureURL != "pic.png" || s.Age == nil || *s.Age != 30 {
		t.Errorf("toSchema lost data: %+v", s)
	}

	back := toDomain(&schema.Lead{
		ID:                s.ID,
		WorkspaceID:       s.WorkspaceID,
		Number:            s.Number,
		Name:              s.Name,
		ProfilePictureURL: s.ProfilePictureURL,
		Age:               s.Age,
	})
	if back.WorkspaceID != "ws-1" || back.Number != "5511987654321" {
		t.Errorf("toDomain lost data: %+v", back)
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

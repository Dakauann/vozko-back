package leadcompany

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/media"
	"vozko/domain/workspace"
)

const (
	workspaceID = "0b6f9c1e-7d1a-4c61-9a0e-2f8d4c1b2a10"
	creatorID   = "7d3e1f00-1111-4c2b-8f00-aa00bb00cc01"
	mediaID     = "5a1b2c3d-0000-4e5f-8a9b-0c1d2e3f4a5b"
)

var now = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func text(s string) *string { return &s }

func flag(b bool) *bool { return &b }

func acme(t *testing.T) Company {
	t.Helper()
	c, err := New(workspaceID, creatorID, Draft{Name: "Acme", AcceptsLeads: true}, now)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNewTrimsAndCollapsesTheNameAndStampsTheCreator(t *testing.T) {
	c, err := New(" "+workspaceID+" ", creatorID, Draft{Name: "  Acme   Indústria  ", AcceptsLeads: true}, now)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.Name != "Acme Indústria" || c.WorkspaceID != workspaceID || c.CreatedBy != creatorID || !c.AcceptsLeads {
		t.Fatalf("company = %+v", c)
	}
	if !c.CreatedAt.Equal(now) || !c.UpdatedAt.Equal(now) {
		t.Fatalf("timestamps = %v %v", c.CreatedAt, c.UpdatedAt)
	}
}

func TestNewRefusesWhatCannotBeACompany(t *testing.T) {
	cases := map[string]struct {
		workspace string
		draft     Draft
		want      error
	}{
		"no workspace":     {"", Draft{Name: "Acme"}, ErrWorkspaceRequired},
		"blank name":       {workspaceID, Draft{Name: "   "}, ErrNameRequired},
		"name too long":    {workspaceID, Draft{Name: strings.Repeat("a", MaxNameLength+1)}, ErrNameTooLong},
		"image not a uuid": {workspaceID, Draft{Name: "Acme", ImageMediaID: "logo.png"}, ErrImageInvalid},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New(tc.workspace, creatorID, tc.draft, now); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestANameOfExactlyTheLimitIsKept(t *testing.T) {
	name := strings.Repeat("é", MaxNameLength)
	c, err := New(workspaceID, creatorID, Draft{Name: name}, now)
	if err != nil || c.Name != name {
		t.Fatalf("name kept = %q, err = %v", c.Name, err)
	}
}

func TestApplyChangesOnlyWhatThePatchCarries(t *testing.T) {
	c := acme(t)
	later := now.Add(time.Hour)
	next, err := c.Apply(Patch{AcceptsLeads: flag(false)}, later)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if next.Name != "Acme" || next.AcceptsLeads || !next.UpdatedAt.Equal(later) || !next.CreatedAt.Equal(now) {
		t.Fatalf("next = %+v", next)
	}
	if !c.AcceptsLeads {
		t.Fatal("Apply must not change the company it was called on")
	}
}

func TestApplyValidatesTheNewName(t *testing.T) {
	if _, err := acme(t).Apply(Patch{Name: text(" ")}, now); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("err = %v, want ErrNameRequired", err)
	}
}

func TestApplySetsAndClearsTheImage(t *testing.T) {
	withImage, err := acme(t).Apply(Patch{ImageMediaID: text(mediaID)}, now)
	if err != nil || withImage.ImageMediaID != mediaID {
		t.Fatalf("image = %q, err = %v", withImage.ImageMediaID, err)
	}
	cleared, err := withImage.Apply(Patch{ImageMediaID: text("")}, now)
	if err != nil || cleared.ImageMediaID != "" || cleared.Image != nil {
		t.Fatalf("cleared = %+v, err = %v", cleared, err)
	}
}

func TestPatchTellsWhichImageItAsksFor(t *testing.T) {
	if _, sets := (Patch{Name: text("Acme")}).NewImage(); sets {
		t.Fatal("a patch without an image asks for none")
	}
	if _, sets := (Patch{ImageMediaID: text("")}).NewImage(); sets {
		t.Fatal("clearing the image asks for no media")
	}
	if id, sets := (Patch{ImageMediaID: text(" " + mediaID + " ")}).NewImage(); !sets || id != mediaID {
		t.Fatalf("image = %q, sets = %v", id, sets)
	}
}

func TestAdmitsOnlyLeadsOfItsWorkspaceWhileOpen(t *testing.T) {
	open := acme(t)
	closed, _ := open.Apply(Patch{AcceptsLeads: flag(false)}, now)
	cases := map[string]struct {
		company   Company
		workspace string
		want      error
	}{
		"open, same workspace":   {open, workspaceID, nil},
		"closed":                 {closed, workspaceID, ErrClosed},
		"other workspace":        {open, "11111111-1111-4111-8111-111111111111", ErrNotFound},
		"closed, other workpace": {closed, "11111111-1111-4111-8111-111111111111", ErrNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := tc.company.Admits(tc.workspace); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestImageOfAcceptsOnlyAnImageOfTheSameWorkspace(t *testing.T) {
	image := &media.Media{ID: mediaID, WorkspaceID: workspaceID, Type: media.MediaTypeProductImage, URL: "https://cdn/x.png", PreviewURL: "https://cdn/x-preview.jpg"}
	got, err := ImageOf(workspaceID, image)
	if err != nil || got != (Image{MediaID: mediaID, URL: "https://cdn/x.png", PreviewURL: "https://cdn/x-preview.jpg"}) {
		t.Fatalf("image = %+v, err = %v", got, err)
	}
	refused := map[string]*media.Media{
		"missing":         nil,
		"other workspace": {ID: mediaID, WorkspaceID: "11111111-1111-4111-8111-111111111111", Type: media.MediaTypeProductImage},
		"not an image":    {ID: mediaID, WorkspaceID: workspaceID, Type: media.MediaTypeDocumentPdf},
	}
	for name, m := range refused {
		t.Run(name, func(t *testing.T) {
			if _, err := ImageOf(workspaceID, m); !errors.Is(err, ErrImageInvalid) {
				t.Fatalf("err = %v, want ErrImageInvalid", err)
			}
		})
	}
}

func TestListQueryNormalizesThePageAndTheSearch(t *testing.T) {
	q, err := ListQuery{Search: "  acme  "}.Normalize()
	if err != nil || q.Search != "acme" || q.Page != 1 || q.PageSize != DefaultPageSize {
		t.Fatalf("query = %+v, err = %v", q, err)
	}
	big, _ := ListQuery{Page: 3, PageSize: MaxPageSize + 50}.Normalize()
	if big.Page != 3 || big.PageSize != MaxPageSize {
		t.Fatalf("query = %+v", big)
	}
	if _, err := (ListQuery{Search: strings.Repeat("a", MaxNameLength+1)}).Normalize(); !errors.Is(err, ErrListQueryInvalid) {
		t.Fatalf("err = %v, want ErrListQueryInvalid", err)
	}
	if _, err := (ListQuery{Page: -1}).Normalize(); !errors.Is(err, ErrListQueryInvalid) {
		t.Fatalf("err = %v, want ErrListQueryInvalid", err)
	}
}

func TestEveryRefusalHasACode(t *testing.T) {
	for _, err := range []error{ErrWorkspaceRequired, ErrNameRequired, ErrNameTooLong, ErrNotFound, ErrClosed, ErrForbidden, ErrImageInvalid, ErrListQueryInvalid} {
		if ErrorCode(err) == "" {
			t.Fatalf("%v has no code", err)
		}
	}
	for _, err := range []error{ErrNameRequired, ErrNameTooLong, ErrImageInvalid, ErrListQueryInvalid} {
		if !IsInputRefusal(err) {
			t.Fatalf("%v must be an input refusal", err)
		}
	}
	if IsInputRefusal(ErrClosed) || IsInputRefusal(ErrNotFound) {
		t.Fatal("a closed or missing company is not a malformed request")
	}
}

func TestEveryCapabilityIsInTheAccessCatalog(t *testing.T) {
	for _, key := range []workspace.CapabilityKey{CapabilityView, CapabilityCreate, CapabilityEdit, CapabilityDelete, CapabilityMembership} {
		if _, ok := workspace.CapabilityRequires(key); !ok {
			t.Errorf("%s is missing from the access catalog", key)
		}
	}
}

package audience_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	ca "vozko/domain/audience"
	"vozko/domain/shared"
)

// The scope of the query comes from the AUTHOR ROW, never from the caller.
// A caller passes an author id and a page; if the scope came from the request
// instead, one workspace's author id plus another workspace's account would
// read someone else's posts.
func TestListAuthorContainersScopesFromTheAuthorRow(t *testing.T) {
	authors := &fakeAuthors{rows: []*ca.AuthorStats{{
		ID: "a-1", WorkspaceID: "ws-1", Source: ca.SourceInstagram,
		AccountID: "acc-1", AuthorExternalID: "ig-99", AuthorHandle: "fulano",
	}}}
	repo := newFakeRepo()
	var seen ca.AuthorContainersInput
	repo.ListAuthorContainersFn = func(in ca.AuthorContainersInput) (*shared.PaginatedResult[*ca.AuthorContainer], error) {
		seen = in
		return shared.NewPaginatedResult([]*ca.AuthorContainer{{
			ContainerID: "media-1", Comments: 3, Stances: ca.StanceMix{Hostile: 3}, SeverityHighCount: 2,
		}}, in.Options.Pagination, 1), nil
	}

	uc := NewListAuthorContainersUseCase(authors, repo)
	out, err := uc.Execute(context.Background(), ca.AuthorContainersRequest{
		WorkspaceID: "ws-1", AuthorID: "a-1", Page: shared.Pagination{Page: 1, PageSize: 10},
	})
	if err != nil {
		t.Fatal(err)
	}

	if seen.WorkspaceID != "ws-1" || seen.Source != ca.SourceInstagram ||
		seen.AccountID != "acc-1" || seen.AuthorExternalID != "ig-99" {
		t.Fatalf("scope = %+v, must come from the author row", seen)
	}
	if out.Author == nil || out.Author.AuthorHandle != "fulano" {
		t.Fatalf("the author must travel with the page: %+v", out.Author)
	}
	if out.Containers == nil || len(out.Containers.Items) != 1 {
		t.Fatalf("containers = %+v", out.Containers)
	}
}

// An author id belonging to another workspace is not found, not empty: an
// empty page would read as "this person has commented on nothing".
func TestListAuthorContainersRefusesAnotherWorkspacesAuthor(t *testing.T) {
	authors := &fakeAuthors{rows: []*ca.AuthorStats{{ID: "a-1", WorkspaceID: "ws-1", AuthorExternalID: "ig-99"}}}
	repo := newFakeRepo()
	repo.ListAuthorContainersFn = func(ca.AuthorContainersInput) (*shared.PaginatedResult[*ca.AuthorContainer], error) {
		t.Fatal("the repository must not be reached for an author of another workspace")
		return nil, nil
	}

	uc := NewListAuthorContainersUseCase(authors, repo)
	if _, err := uc.Execute(context.Background(), ca.AuthorContainersRequest{WorkspaceID: "ws-2", AuthorID: "a-1"}); !errors.Is(err, ca.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// An author row with no external id cannot be queried by external id, and the
// use case must say so rather than asking the repository for every post ever
// commented on by nobody.
func TestListAuthorContainersRefusesAnAuthorWithoutAnExternalID(t *testing.T) {
	authors := &fakeAuthors{rows: []*ca.AuthorStats{{ID: "a-1", WorkspaceID: "ws-1", AuthorExternalID: ""}}}
	repo := newFakeRepo()
	repo.ListAuthorContainersFn = func(ca.AuthorContainersInput) (*shared.PaginatedResult[*ca.AuthorContainer], error) {
		t.Fatal("an unidentifiable author must not reach the repository")
		return nil, nil
	}

	uc := NewListAuthorContainersUseCase(authors, repo)
	if _, err := uc.Execute(context.Background(), ca.AuthorContainersRequest{WorkspaceID: "ws-1", AuthorID: "a-1"}); !errors.Is(err, ca.ErrInvalidFilter) {
		t.Fatalf("err = %v, want ErrInvalidFilter", err)
	}
}

// The page reaches the repository normalised: a caller who sends no pagination
// must not turn into an unbounded read.
func TestListAuthorContainersNormalisesThePage(t *testing.T) {
	authors := &fakeAuthors{rows: []*ca.AuthorStats{{ID: "a-1", WorkspaceID: "ws-1", AuthorExternalID: "ig-99"}}}
	repo := newFakeRepo()
	var seen ca.AuthorContainersInput
	repo.ListAuthorContainersFn = func(in ca.AuthorContainersInput) (*shared.PaginatedResult[*ca.AuthorContainer], error) {
		seen = in
		return shared.NewPaginatedResult([]*ca.AuthorContainer{}, in.Options.Pagination, 0), nil
	}

	uc := NewListAuthorContainersUseCase(authors, repo)
	if _, err := uc.Execute(context.Background(), ca.AuthorContainersRequest{WorkspaceID: "ws-1", AuthorID: "a-1"}); err != nil {
		t.Fatal(err)
	}
	if seen.Options.Pagination.PageSize <= 0 || seen.Options.Pagination.Page <= 0 {
		t.Fatalf("pagination = %+v", seen.Options.Pagination)
	}
}

// The window travels with the request: "em quais posts esta pessoa comentou
// este mês" is the same read with a narrower predicate, not a second endpoint.
func TestListAuthorContainersPassesTheWindowThrough(t *testing.T) {
	authors := &fakeAuthors{rows: []*ca.AuthorStats{{ID: "a-1", WorkspaceID: "ws-1", AuthorExternalID: "ig-99"}}}
	repo := newFakeRepo()
	var seen ca.AuthorContainersInput
	repo.ListAuthorContainersFn = func(in ca.AuthorContainersInput) (*shared.PaginatedResult[*ca.AuthorContainer], error) {
		seen = in
		return shared.NewPaginatedResult([]*ca.AuthorContainer{}, in.Options.Pagination, 0), nil
	}

	from := now.Add(-30 * 24 * time.Hour)
	uc := NewListAuthorContainersUseCase(authors, repo)
	if _, err := uc.Execute(context.Background(), ca.AuthorContainersRequest{
		WorkspaceID: "ws-1", AuthorID: "a-1", From: &from, To: &now,
	}); err != nil {
		t.Fatal(err)
	}
	if seen.From == nil || !seen.From.Equal(from) || seen.To == nil || !seen.To.Equal(now) {
		t.Fatalf("window = %v..%v, want %v..%v", seen.From, seen.To, from, now)
	}
}

// An inverted window is refused before the repository is asked for anything.
func TestListAuthorContainersRefusesAnInvertedWindow(t *testing.T) {
	authors := &fakeAuthors{rows: []*ca.AuthorStats{{ID: "a-1", WorkspaceID: "ws-1", AuthorExternalID: "ig-99"}}}
	repo := newFakeRepo()
	repo.ListAuthorContainersFn = func(ca.AuthorContainersInput) (*shared.PaginatedResult[*ca.AuthorContainer], error) {
		t.Fatal("an inverted window must not reach the repository")
		return nil, nil
	}

	earlier := now.Add(-time.Hour)
	uc := NewListAuthorContainersUseCase(authors, repo)
	if _, err := uc.Execute(context.Background(), ca.AuthorContainersRequest{
		WorkspaceID: "ws-1", AuthorID: "a-1", From: &now, To: &earlier,
	}); !errors.Is(err, ca.ErrInvalidFilter) {
		t.Fatalf("err = %v, want ErrInvalidFilter", err)
	}
}

package audience

import (
	"errors"
	"testing"

	"vozko/domain/shared"
)

func TestAuthorContainersInputValidate(t *testing.T) {
	cases := map[string]struct {
		in      AuthorContainersInput
		wantErr bool
	}{
		"complete":        {in: AuthorContainersInput{WorkspaceID: "ws", AuthorExternalID: "a1"}},
		"no workspace":    {in: AuthorContainersInput{AuthorExternalID: "a1"}, wantErr: true},
		"no author":       {in: AuthorContainersInput{WorkspaceID: "ws"}, wantErr: true},
		"bad source":      {in: AuthorContainersInput{WorkspaceID: "ws", AuthorExternalID: "a1", Source: "tiktok"}, wantErr: true},
		"blank source":    {in: AuthorContainersInput{WorkspaceID: "ws", AuthorExternalID: "a1", Source: ""}},
		"known source":    {in: AuthorContainersInput{WorkspaceID: "ws", AuthorExternalID: "a1", Source: SourceInstagram}},
		"only whitespace": {in: AuthorContainersInput{WorkspaceID: "ws", AuthorExternalID: "   "}, wantErr: true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			in := c.in
			in.Normalize()
			err := in.Validate()
			if c.wantErr {
				if !errors.Is(err, ErrInvalidFilter) {
					t.Fatalf("err = %v, want ErrInvalidFilter", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

// Normalize must give the query a page even when the caller sent none, or the
// repository would be asked for an unbounded read of someone's whole history.
func TestAuthorContainersInputNormalizePages(t *testing.T) {
	in := AuthorContainersInput{WorkspaceID: " ws ", AuthorExternalID: " a1 ", Options: shared.QueryOptions{}}
	in.Normalize()
	if in.WorkspaceID != "ws" || in.AuthorExternalID != "a1" {
		t.Fatalf("not trimmed: %+v", in)
	}
	if in.Options.Pagination.PageSize <= 0 || in.Options.Pagination.Page <= 0 {
		t.Fatalf("pagination = %+v, must be defaulted", in.Options.Pagination)
	}
}

// A post's standing uses the same functions as the author's overall standing:
// "hostile on this post" and "hostile overall" must not be two different rules.
func TestAuthorContainerDeriveMatchesTheAuthorRules(t *testing.T) {
	c := &AuthorContainer{
		Comments:          4,
		Stances:           StanceMix{Hostile: 4},
		SeverityHighCount: 3,
	}
	c.Derive()

	mix := StanceMix{Hostile: 4}
	if want := DerivedStance(mix); c.DerivedStance != want {
		t.Fatalf("stance = %q, want %q", c.DerivedStance, want)
	}
	if want := AuthorReputation(mix, 3); c.Reputation != want {
		t.Fatalf("reputation = %d, want %d", c.Reputation, want)
	}
	if c.Reputation >= 0 {
		t.Fatalf("four hostile comments, three of them severe, must not score %d", c.Reputation)
	}
}

// A post someone commented on neutrally is neutral, not "no data": an empty
// mix and a neutral mix must not collapse into the same answer by accident.
func TestAuthorContainerDeriveNeutral(t *testing.T) {
	c := &AuthorContainer{Comments: 2, Stances: StanceMix{Neutral: 2}}
	c.Derive()
	if c.DerivedStance != StanceNeutral {
		t.Fatalf("stance = %q", c.DerivedStance)
	}
	if c.Reputation != 0 {
		t.Fatalf("reputation = %d, want 0", c.Reputation)
	}
}

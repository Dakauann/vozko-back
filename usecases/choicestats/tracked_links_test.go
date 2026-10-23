package choicestats_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/shortlink"
)

type fakeLinkCreator struct {
	in  shortlink.CreateShortLinkInput
	err error
}

func (f *fakeLinkCreator) Execute(_ context.Context, in shortlink.CreateShortLinkInput) (*shortlink.ShortLink, error) {
	f.in = in
	if f.err != nil {
		return nil, f.err
	}
	return &shortlink.ShortLink{ID: "sl-1", Code: "Ab3x", TargetURL: in.TargetURL}, nil
}

func TestATrackedLinkIsAShortLinkToTheRealDestination(t *testing.T) {
	creator := &fakeLinkCreator{}
	links, err := NewTrackedLinks(creator, "https://vz.co/r")
	if err != nil {
		t.Fatal(err)
	}
	link, err := links.Create(context.Background(), "ws", "user-1", " https://loja.com/promo ")
	if err != nil {
		t.Fatal(err)
	}
	if creator.in.WorkspaceID != "ws" || creator.in.CreatedBy != "user-1" || creator.in.TargetURL != "https://loja.com/promo" || creator.in.Title == "" {
		t.Fatalf("short link input = %+v", creator.in)
	}
	if link.ButtonURL != "https://vz.co/r/Ab3x/{{1}}" || link.Destination != "https://loja.com/promo" {
		t.Fatalf("link = %+v", link)
	}

	creator.err = shortlink.ErrTargetURLBlocked
	if _, err := links.Create(context.Background(), "ws", "user-1", "http://10.0.0.1"); !errors.Is(err, shortlink.ErrTargetURLBlocked) {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewTrackedLinks(creator, ""); !errors.Is(err, ErrTrackedLinksUnwired) {
		t.Fatalf("without the short link base = %v", err)
	}
}

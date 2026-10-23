package choicestats_usecase

import (
	"context"
	"errors"
	"strings"

	"vozko/domain/choicestats"
	"vozko/domain/shortlink"
)

var ErrTrackedLinksUnwired = errors.New("choice stats: tracked links need the short link service and its base url")

const trackedLinkTitle = "Botão de template com cliques rastreados"

type LinkCreator interface {
	Execute(ctx context.Context, input shortlink.CreateShortLinkInput) (*shortlink.ShortLink, error)
}

type TrackedLinks struct {
	creator LinkCreator
	base    string
}

func NewTrackedLinks(creator LinkCreator, base string) (*TrackedLinks, error) {
	if creator == nil || strings.TrimSpace(base) == "" {
		return nil, ErrTrackedLinksUnwired
	}
	return &TrackedLinks{creator: creator, base: strings.TrimSpace(base)}, nil
}

func (t *TrackedLinks) Create(ctx context.Context, workspaceID, userID, destination string) (*choicestats.TrackedLink, error) {
	link, err := t.creator.Execute(ctx, shortlink.CreateShortLinkInput{
		WorkspaceID: workspaceID,
		CreatedBy:   userID,
		TargetURL:   strings.TrimSpace(destination),
		Title:       trackedLinkTitle,
	})
	if err != nil {
		return nil, err
	}
	tracked := choicestats.NewTrackedLink(t.base, link.Code, link.ID, link.TargetURL)
	return &tracked, nil
}

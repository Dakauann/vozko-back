package choicestats_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vozko/domain/choicestats"
	"vozko/domain/shortlink"
	"vozko/domain/whatsapp/template"
)

var ErrButtonTrackingUnwired = errors.New("choice stats: button tracking needs the tracked links and the short link lookup")

type CodeFinder interface {
	FindByCode(ctx context.Context, code string) (*shortlink.ShortLink, error)
}

type ButtonTracking struct {
	links *TrackedLinks
	codes CodeFinder
}

func NewButtonTracking(links *TrackedLinks, codes CodeFinder) (*ButtonTracking, error) {
	if links == nil || codes == nil {
		return nil, ErrButtonTrackingUnwired
	}
	return &ButtonTracking{links: links, codes: codes}, nil
}

func (b *ButtonTracking) Apply(ctx context.Context, workspaceID, userID string, components []template.TemplateComponent, tracked []int) ([]template.TemplateComponent, error) {
	out := make([]template.TemplateComponent, len(components))
	copy(out, components)
	wanted := map[int]bool{}
	for _, i := range tracked {
		wanted[i] = true
	}
	for c := range out {
		if strings.ToUpper(strings.TrimSpace(out[c].Type)) != "BUTTONS" {
			continue
		}
		out[c].Buttons = append([]template.TemplateButton(nil), out[c].Buttons...)
		for i := range out[c].Buttons {
			btn, err := b.applyOne(ctx, workspaceID, userID, out[c].Buttons[i], wanted[i])
			if err != nil {
				return nil, template.TrackedButtonRefusal(i, err)
			}
			out[c].Buttons[i] = btn
			delete(wanted, i)
		}
	}
	if len(wanted) > 0 {
		return nil, template.TrackedButtonRefusal(firstOf(wanted), template.ErrTrackedButtonNotURL)
	}
	return out, nil
}

func firstOf(indexes map[int]bool) int {
	first := -1
	for i := range indexes {
		if first < 0 || i < first {
			first = i
		}
	}
	return first
}

func (b *ButtonTracking) applyOne(ctx context.Context, workspaceID, userID string, btn template.TemplateButton, wanted bool) (template.TemplateButton, error) {
	if !btn.IsURL() {
		if wanted {
			return btn, template.ErrTrackedButtonNotURL
		}
		return btn, nil
	}
	code, isTracked := choicestats.TrackedLinkCode(b.links.base, btn.URL)
	switch {
	case wanted && isTracked, !wanted && !isTracked:
		return btn, nil
	case wanted:
		if strings.Contains(btn.URL, "{{") {
			return btn, template.ErrTrackedButtonDynamic
		}
		link, err := b.links.Create(ctx, workspaceID, userID, btn.URL)
		if err != nil {
			return btn, destinationError(err)
		}
		btn.URL, btn.Example = link.ButtonURL, link.ExampleURL
		return btn, nil
	}
	link, err := b.codes.FindByCode(ctx, code)
	if err != nil || link == nil || link.WorkspaceID != workspaceID || strings.TrimSpace(link.TargetURL) == "" {
		return btn, fmt.Errorf("%w: %v", template.ErrTrackedLinkUnreadable, err)
	}
	btn.URL, btn.Example = link.TargetURL, ""
	return btn, nil
}

func destinationError(err error) error {
	for _, refused := range []error{
		shortlink.ErrTargetURLRequired, shortlink.ErrTargetURLTooLong, shortlink.ErrTargetURLInvalid, shortlink.ErrTargetURLScheme,
		shortlink.ErrTargetURLCredentials, shortlink.ErrTargetURLBlocked, shortlink.ErrTargetURLLoop, shortlink.ErrThreatDetected,
	} {
		if errors.Is(err, refused) {
			return fmt.Errorf("%w: %w", template.ErrTrackedButtonDestination, err)
		}
	}
	return err
}

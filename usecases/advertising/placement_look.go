package advertising

import (
	"context"
	"errors"
	"fmt"
	"strings"

	ads "vozko/domain/advertising"
	"vozko/domain/media"
)

var (
	ErrPlacementImageUnreadable = errors.New("ads: the image could not be read for the placement preview")
	ErrNoImagePlacement         = errors.New("ads: none of the chosen placements shows an image")
)

type PanelLabels struct {
	Title  string
	Top    string
	Bottom string
}

type PlacementPicture interface {
	Size() ads.PixelSize
	Detail() ads.DetailMap
	Panel(frame ads.PlacementFrame, layout ads.FrameLayout, labels PanelLabels) (string, error)
}

type PlacementPainter interface {
	Open(data []byte) (PlacementPicture, error)
}

type PlacementLookInput struct {
	WorkspaceID string
	MediaID     string
	Placements  ads.Placements
	Panels      bool
	Labels      func(frameID string) PanelLabels
}

type PlacementLookFrame struct {
	Planned ads.PlannedFrame
	Layout  ads.FrameLayout
	Detail  ads.FrameDetail
	Issues  []string
	Panel   string
}

type PlacementLook struct {
	Size      ads.PixelSize
	Frames    []PlacementLookFrame
	VideoOnly []ads.PlacementPosition
}

type PlacementLookUseCase struct {
	media   MediaSource
	painter PlacementPainter
}

func NewPlacementLookUseCase(source MediaSource, painter PlacementPainter) *PlacementLookUseCase {
	return &PlacementLookUseCase{media: source, painter: painter}
}

func (u *PlacementLookUseCase) Look(ctx context.Context, in PlacementLookInput) (*PlacementLook, error) {
	plan := ads.PlanPlacementFrames(in.Placements)
	switch {
	case len(plan.Unknown) > 0:
		return nil, ads.FieldError("placements", "invalid")
	case len(plan.Frames) == 0:
		return nil, ErrNoImagePlacement
	}
	mediaID := strings.TrimSpace(in.MediaID)
	if mediaID == "" {
		return nil, media.ErrMediaNotFound
	}
	file, err := u.media.Load(ctx, in.WorkspaceID, ads.MediaRef{Kind: ads.MediaImage, MediaID: mediaID})
	if err != nil {
		return nil, err
	}
	picture, err := u.painter.Open(file.Bytes)
	if err != nil {
		return nil, errors.Join(ErrPlacementImageUnreadable, err)
	}
	size, detail := picture.Size(), picture.Detail()
	look := &PlacementLook{Size: size, VideoOnly: plan.VideoOnly}
	for _, planned := range plan.Frames {
		layout, err := planned.Frame.Layout(size.Width, size.Height)
		if err != nil {
			return nil, errors.Join(ErrPlacementImageUnreadable, err)
		}
		measured := layout.Detail(detail)
		frame := PlacementLookFrame{Planned: planned, Layout: layout, Detail: measured, Issues: layout.Issues(measured)}
		if in.Panels {
			if frame.Panel, err = picture.Panel(planned.Frame, layout, in.Labels(planned.Frame.ID)); err != nil {
				return nil, errors.Join(ErrPlacementImageUnreadable, fmt.Errorf("panel %s: %w", planned.Frame.ID, err))
			}
		}
		look.Frames = append(look.Frames, frame)
	}
	return look, nil
}

package advertising

import (
	"context"
	"errors"
	"testing"

	ads "vozko/domain/advertising"
	"vozko/domain/media"
)

type fakePicture struct {
	size   ads.PixelSize
	detail ads.DetailMap
	panels []string
	err    error
}

func (p *fakePicture) Size() ads.PixelSize   { return p.size }
func (p *fakePicture) Detail() ads.DetailMap { return p.detail }
func (p *fakePicture) Panel(frame ads.PlacementFrame, layout ads.FrameLayout, labels PanelLabels) (string, error) {
	if p.err != nil {
		return "", p.err
	}
	p.panels = append(p.panels, frame.ID+":"+labels.Title)
	return "data:image/jpeg;base64,AA==", nil
}

type fakePainter struct {
	picture *fakePicture
	err     error
	opened  [][]byte
}

func (p *fakePainter) Open(data []byte) (PlacementPicture, error) {
	p.opened = append(p.opened, data)
	if p.err != nil {
		return nil, p.err
	}
	return p.picture, nil
}

func bottomText() ads.DetailMap {
	m := ads.DetailMap{Cols: 4, Rows: 10, Cells: make([]float64, 40)}
	for i := 32; i < 40; i++ {
		m.Cells[i] = 1
	}
	return m
}

func titles(id string) PanelLabels { return PanelLabels{Title: "t-" + id} }

func TestThePlacementLookMeasuresEveryFrameOfThePlacements(t *testing.T) {
	picture := &fakePicture{size: ads.PixelSize{Width: 1080, Height: 1920}, detail: bottomText()}
	uc := NewPlacementLookUseCase(&fakeMedia{}, &fakePainter{picture: picture})
	look, err := uc.Look(context.Background(), PlacementLookInput{
		WorkspaceID: "ws", MediaID: "m1", Placements: ads.Placements{Platforms: []string{"instagram", "audience_network"}}, Panels: true, Labels: titles,
	})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, f := range look.Frames {
		ids = append(ids, f.Planned.Frame.ID)
		if f.Panel == "" {
			t.Errorf("%s has no panel", f.Planned.Frame.ID)
		}
	}
	if len(ids) != 3 || ids[0] != "feed" || ids[1] != "vertical" || ids[2] != "audience_network" {
		t.Fatalf("frames %v", ids)
	}
	if look.Size.Width != 1080 || len(look.VideoOnly) != 1 {
		t.Fatalf("got %+v", look)
	}
	vertical := look.Frames[1]
	if vertical.Detail.UnderUI < 0.9 || len(vertical.Issues) == 0 || vertical.Issues[0] != ads.IssueDetailUnderUI {
		t.Fatalf("text at the bottom of a 9:16 image sits under the caption, got %+v", vertical)
	}
	if picture.panels[0] != "feed:t-feed" {
		t.Fatalf("labels must reach the painter, got %v", picture.panels)
	}
}

func TestThePlacementLookSkipsPanelsWhenTheModelCannotSee(t *testing.T) {
	picture := &fakePicture{size: ads.PixelSize{Width: 1080, Height: 1080}}
	look, err := NewPlacementLookUseCase(&fakeMedia{}, &fakePainter{picture: picture}).Look(context.Background(), PlacementLookInput{
		WorkspaceID: "ws", MediaID: "m1", Placements: ads.Placements{Automatic: true}, Labels: titles,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(picture.panels) != 0 || look.Frames[0].Panel != "" || len(look.Frames) == 0 {
		t.Fatalf("no panel is drawn without vision, got %v", picture.panels)
	}
}

func TestThePlacementLookFailsClosed(t *testing.T) {
	picture := &fakePicture{size: ads.PixelSize{Width: 1080, Height: 1080}}
	cases := map[string]struct {
		media   *fakeMedia
		painter *fakePainter
		in      PlacementLookInput
		want    error
	}{
		"missing media":    {media: &fakeMedia{err: media.ErrMediaNotFound}, painter: &fakePainter{picture: picture}, want: media.ErrMediaNotFound},
		"not an image":     {media: &fakeMedia{err: ads.ErrMediaNotImage}, painter: &fakePainter{picture: picture}, want: ads.ErrMediaNotImage},
		"unreadable":       {media: &fakeMedia{}, painter: &fakePainter{err: errors.New("bad png")}, want: ErrPlacementImageUnreadable},
		"broken panel":     {media: &fakeMedia{}, painter: &fakePainter{picture: &fakePicture{size: ads.PixelSize{Width: 10, Height: 10}, err: errors.New("encode")}}, in: PlacementLookInput{Panels: true}, want: ErrPlacementImageUnreadable},
		"unknown platform": {media: &fakeMedia{}, painter: &fakePainter{picture: picture}, in: PlacementLookInput{Placements: ads.Placements{Platforms: []string{"tiktok"}}}},
		"only video slots": {media: &fakeMedia{}, painter: &fakePainter{picture: picture}, in: PlacementLookInput{Placements: ads.Placements{Platforms: []string{"facebook"}, Positions: map[string][]string{"facebook": {"instream_video"}}}}, want: ErrNoImagePlacement},
		"blank media":      {media: &fakeMedia{}, painter: &fakePainter{picture: picture}, in: PlacementLookInput{MediaID: " "}, want: media.ErrMediaNotFound},
	}
	for name, c := range cases {
		in := c.in
		in.WorkspaceID = "ws"
		if in.MediaID == "" {
			in.MediaID = "m1"
		}
		if !in.Placements.Automatic && len(in.Placements.Platforms) == 0 {
			in.Placements = ads.Placements{Automatic: true}
		}
		in.Labels = titles
		look, err := NewPlacementLookUseCase(c.media, c.painter).Look(context.Background(), in)
		if err == nil || look != nil {
			t.Errorf("%s: must refuse, got %+v", name, look)
			continue
		}
		var invalid *ads.ValidationError
		if c.want == nil && !errors.As(err, &invalid) {
			t.Errorf("%s: want a field error, got %v", name, err)
		}
		if c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("%s: want %v, got %v", name, c.want, err)
		}
	}
}

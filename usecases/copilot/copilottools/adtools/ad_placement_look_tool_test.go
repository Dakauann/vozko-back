package adtools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	"vozko/domain/media"
	"vozko/domain/workspace"
	adsuc "vozko/usecases/advertising"
	"vozko/usecases/copilot/copilottools/tooltest"
)

const lookMedia = "6a1f0c3e-2b4d-4c8e-9f10-1a2b3c4d5e6f"

type fakePlacementLook struct {
	got adsuc.PlacementLookInput
	err error
}

func (f *fakePlacementLook) Look(_ context.Context, in adsuc.PlacementLookInput) (*adsuc.PlacementLook, error) {
	f.got = in
	if f.err != nil {
		return nil, f.err
	}
	plan := advertising.PlanPlacementFrames(in.Placements)
	look := &adsuc.PlacementLook{Size: advertising.PixelSize{Width: 1080, Height: 1080}, VideoOnly: plan.VideoOnly}
	for _, planned := range plan.Frames {
		layout, _ := planned.Frame.Layout(1080, 1080)
		detail := advertising.FrameDetail{UnderUI: 0.4, Bounds: &advertising.FrameRect{X0: 0.1, Y0: 0.6, X1: 0.9, Y1: 0.75}}
		frame := adsuc.PlacementLookFrame{Planned: planned, Layout: layout, Detail: detail, Issues: layout.Issues(detail)}
		if in.Panels {
			frame.Panel = "data:image/jpeg;base64," + planned.Frame.ID
			_ = in.Labels(planned.Frame.ID)
		}
		look.Frames = append(look.Frames, frame)
	}
	return look, nil
}

func placementLookTool(f *fakePlacementLook) copilot.Tool {
	return NewAdPlacementLookTool(AdsDeps{Placements: f})
}

func seeing() copilot.Context {
	cc := tooltest.AdContext
	cc.SeesImages = true
	return cc
}

func TestThePlacementLookShowsOnePreviewPerFrameToAModelThatSees(t *testing.T) {
	f := &fakePlacementLook{}
	res := placementLookTool(f).Execute(context.Background(), seeing(), map[string]interface{}{"media_id": lookMedia, "placements": []interface{}{"instagram"}})
	if res.Status != copilot.StatusOK {
		t.Fatalf("got %+v", res)
	}
	if !f.got.Panels || f.got.MediaID != lookMedia || f.got.WorkspaceID != "ws-1" || f.got.Placements.Automatic || f.got.Placements.Platforms[0] != "instagram" {
		t.Fatalf("input %+v", f.got)
	}
	if len(res.Images) != 2 || res.Images[0] != "data:image/jpeg;base64,feed" || res.Images[1] != "data:image/jpeg;base64,vertical" {
		t.Fatalf("images %v", res.Images)
	}
	data := tooltest.DataOf(res)
	if data["seen"] != true {
		t.Fatalf("data %+v", data)
	}
	frames := data["frames"].([]map[string]interface{})
	vertical := frames[1]
	if vertical["preview"] != 2 || !strings.Contains(vertical["shown"].(string), "fundo") || vertical["recommended_px"] != "1440x2560" {
		t.Fatalf("vertical %+v", vertical)
	}
	warnings := strings.Join(vertical["warnings"].([]string), " ")
	if !strings.Contains(warnings, "40%") || !strings.Contains(warnings, "interface") {
		t.Fatalf("warnings %q", warnings)
	}
	if !strings.Contains(vertical["placements"].(string), "Stories do Instagram") {
		t.Fatalf("placements %q", vertical["placements"])
	}
}

func TestAModelWithoutVisionGetsOnlyNumbersAndIsToldItDidNotLook(t *testing.T) {
	f := &fakePlacementLook{}
	res := placementLookTool(f).Execute(context.Background(), tooltest.AdContext, map[string]interface{}{"media_id": lookMedia})
	if res.Status != copilot.StatusOK || len(res.Images) != 0 || f.got.Panels {
		t.Fatalf("no image may travel to a blind model, got %+v and %+v", res, f.got)
	}
	data := tooltest.DataOf(res)
	if data["seen"] != false || !strings.Contains(data["note"].(string), "não vê imagens") {
		t.Fatalf("the reply must say plainly that nothing was seen, got %+v", data)
	}
	if !f.got.Placements.Automatic || len(data["frames"].([]map[string]interface{})) == 0 {
		t.Fatalf("automatic placements are the default, got %+v", f.got.Placements)
	}
	if data["video_only"] == nil {
		t.Fatal("placements that never show an image must be named")
	}
}

func TestThePlacementLookFailsClosedWithAReason(t *testing.T) {
	cases := map[string]struct {
		args map[string]interface{}
		err  error
		want string
	}{
		"invented id":     {args: map[string]interface{}{"media_id": "foto.png"}, want: "nunca invente"},
		"missing id":      {args: map[string]interface{}{}, want: "media_id"},
		"not in library":  {args: map[string]interface{}{"media_id": lookMedia}, err: media.ErrMediaNotFound, want: "biblioteca"},
		"not an image":    {args: map[string]interface{}{"media_id": lookMedia}, err: advertising.ErrMediaNotImage, want: "biblioteca"},
		"unreadable":      {args: map[string]interface{}{"media_id": lookMedia}, err: adsuc.ErrPlacementImageUnreadable, want: "nada foi conferido"},
		"only video":      {args: map[string]interface{}{"media_id": lookMedia}, err: adsuc.ErrNoImagePlacement, want: "só vídeo"},
		"bad platform":    {args: map[string]interface{}{"media_id": lookMedia}, err: advertising.FieldError("placements", "invalid"), want: "placements"},
		"download failed": {args: map[string]interface{}{"media_id": lookMedia}, err: errors.New("timeout"), want: "nada foi conferido"},
		"mixed":           {args: map[string]interface{}{"media_id": lookMedia, "placements": []interface{}{"automatic", "facebook"}}, want: "automatic"},
	}
	for name, c := range cases {
		res := placementLookTool(&fakePlacementLook{err: c.err}).Execute(context.Background(), seeing(), c.args)
		if res.Status != copilot.StatusError || len(res.Images) != 0 || !strings.Contains(res.Message, c.want) {
			t.Errorf("%s: got %+v", name, res)
		}
	}
}

func TestThePlacementLookIsAFreeReadOfferedWhereAdsAreMade(t *testing.T) {
	tool := placementLookTool(&fakePlacementLook{})
	if m := tool.Meta(); m.Mutating || m.Resource != workspace.ResourceAds || m.Action != workspace.ActionRead {
		t.Fatalf("meta %+v", m)
	}
	studio := copilot.View{Surface: copilot.SurfaceStudio, ProjectID: "p1", ProjectKind: copilot.StudioImage}
	if !studio.Offers(tool) || !(copilot.View{}).Offers(tool) {
		t.Fatal("the look must sit next to create_ad and save_ad_draft")
	}
	if size := len(tool.Definition().Description); size > 700 {
		t.Fatalf("description grew to %d characters", size)
	}
}

func TestEveryPlacementIssueAndFrameHasWordsForTheModel(t *testing.T) {
	for _, code := range advertising.PlacementIssueCodes() {
		if _, ok := placementIssueTexts[code]; !ok {
			t.Errorf("issue %s has no text", code)
		}
	}
	for _, frame := range advertising.PlacementFrames() {
		if frameTitles[frame.ID] == "" {
			t.Errorf("frame %s has no title", frame.ID)
		}
		for _, p := range frame.Positions {
			if positionNames[p] == "" {
				t.Errorf("%v has no name", p)
			}
		}
	}
	for platform, positions := range advertising.VideoOnlyPositions() {
		for _, position := range positions {
			if positionNames[advertising.PlacementPosition{Platform: platform, Position: position}] == "" {
				t.Errorf("%s %s has no name", platform, position)
			}
		}
	}
}

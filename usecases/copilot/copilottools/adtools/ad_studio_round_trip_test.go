package adtools

import (
	"context"
	"slices"
	"testing"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	"vozko/domain/media"
	"vozko/usecases/copilot/copilottools/studiotools"
	"vozko/usecases/copilot/copilottools/tooltest"
)

const (
	sentPhoto     = "5c2d3e4f-6a7b-4c8d-9e0f-1a2b3c4d5e6f"
	exportedArt   = "6d3e4f5a-7b8c-4d9e-8f10-2b3c4d5e6f70"
	roundTripDeck = "7e4f5a6b-8c9d-4eaf-9021-3c4d5e6f7081"
)

type sentMedia struct{}

func (sentMedia) GetMedia(workspaceID, mediaID string) (*media.Media, error) {
	if workspaceID == "ws-1" && mediaID == sentPhoto {
		return &media.Media{ID: sentPhoto, URL: "https://cdn/sent.jpg", Type: media.MediaTypeProductImage}, nil
	}
	return nil, media.ErrMediaNotFound
}

func TestAnAdCreativeGoesToTheStudioAndComesBackIntoTheDraft(t *testing.T) {
	panel := tooltest.OwnerWithScreen(copilot.View{}, &tooltest.ProjectScreen{ProjectID: roundTripDeck, Opened: true})
	panel.WorkspaceID = "ws-1"
	opener := studiotools.NewNewStudioProjectTool(studiotools.StudioDeps{Library: sentMedia{}})
	res := opener.Execute(context.Background(), panel, map[string]interface{}{"kind": "image", "media_ids": []interface{}{sentPhoto}, "sizes": []interface{}{"feed", "vertical"}})
	if res.Status != copilot.StatusOK || res.Card == nil || res.Card.StudioProject == nil {
		t.Fatalf("the ad flow must hand the photo to a new project, got %+v", res)
	}
	order := res.Card.StudioProject

	studio := copilot.View{Surface: copilot.SurfaceStudio, ProjectID: roundTripDeck, ProjectKind: copilot.StudioImage}
	if res.Opening == nil || res.Opening.View != studio {
		t.Fatalf("the created project is the studio screen the rest of the round trip runs on, got %+v", res.Opening)
	}
	deps, _, drafts, _ := fullAdDeps()
	deps.Placements = &fakePlacementLook{}
	offered := []string{}
	for _, tool := range append(studiotools.StudioTools(studiotools.StudioDeps{}), append(AllAdTools(deps, AdManageDeps{}, AdGrowthDeps{}), opener)...) {
		if studio.Offers(tool) {
			offered = append(offered, tool.Definition().Name)
		}
	}
	for _, need := range []string{"studio_edit_image", "studio_export", "ad_placement_look", "save_ad_draft", "create_ad"} {
		if !slices.Contains(offered, need) {
			t.Fatalf("inside the project Elo needs %s to finish the round trip, offered %v", need, offered)
		}
	}
	if slices.Contains(offered, "new_studio_project") {
		t.Fatal("inside a project the tool that opens one stays out of the list")
	}

	plan := advertising.PlanPlacementFrames(advertising.Placements{Platforms: []string{"instagram"}})
	for i, planned := range plan.Frames {
		size := order.Sizes[i]
		layout, err := planned.Frame.Layout(size.Width, size.Height)
		if err != nil || layout.CropLoss != 0 || layout.Background != 0 {
			t.Fatalf("an artboard made for %s must fit it without crop or fill, got %+v", planned.Frame.ID, layout)
		}
	}

	inStudio := tooltest.AdContext
	inStudio.View, inStudio.SeesImages = studio, true
	look := NewAdPlacementLookTool(deps).Execute(context.Background(), inStudio, map[string]interface{}{"media_id": exportedArt, "placements": []interface{}{"instagram"}})
	if look.Status != copilot.StatusOK || len(look.Images) != 2 {
		t.Fatalf("the exported art is checked per placement before the ad, got %+v", look)
	}

	args := createAdArgsMap()
	args["media_id"] = exportedArt
	args["placements"] = []interface{}{"instagram"}
	saved := NewSaveAdDraftTool(deps).Execute(context.Background(), inStudio, args)
	if saved.Status != copilot.StatusOK || drafts.created == nil || drafts.created.Ads[0].Creative.Media.MediaID != exportedArt {
		t.Fatalf("the exported art must become the draft's creative, got %+v", saved)
	}
}

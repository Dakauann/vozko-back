package adtools

import (
	"slices"
	"strings"
	"testing"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	"vozko/domain/workspace"
)

func offeredNames(view copilot.View, tools []copilot.Tool) []string {
	var names []string
	for _, tool := range tools {
		if view.Offers(tool) {
			names = append(names, tool.Definition().Name)
		}
	}
	slices.Sort(names)
	return names
}

func TestTheStudioOffersOnlyTheRouteThatTurnsAnExportIntoAnAd(t *testing.T) {
	studio := copilot.View{Surface: copilot.SurfaceStudio, ProjectID: "p1", ProjectKind: copilot.StudioImage}
	want := []string{"ad_account_readiness", "ad_placement_look", "connect_ad_account", "create_ad", "list_ad_accounts", "list_ad_pages", "list_lead_forms", "save_ad_draft", "search_ad_locations"}
	all := AllAdTools(AdsDeps{}, AdManageDeps{}, AdGrowthDeps{})
	if got := offeredNames(studio, all); !slices.Equal(got, want) {
		t.Fatalf("studio offers %v, want %v", got, want)
	}
	if got := offeredNames(copilot.View{}, all); len(got) != len(all) {
		t.Fatalf("outside the studio every ad tool stays offered, got %d of %d", len(got), len(all))
	}
}

func TestTheStudioRouteKeepsTheAdsPermission(t *testing.T) {
	studio := copilot.View{Surface: copilot.SurfaceStudio, ProjectID: "p1", ProjectKind: copilot.StudioVideo}
	for _, tool := range AllAdTools(AdsDeps{}, AdManageDeps{}, AdGrowthDeps{}) {
		if studio.Offers(tool) && tool.Meta().Resource != workspace.ResourceAds {
			t.Errorf("%s left the ads permission", tool.Definition().Name)
		}
	}
}

func TestAnOffSpecCreativeIsExplainedWithTheLimit(t *testing.T) {
	cases := map[string]string{
		advertising.CodeMediaRatio:      "9:16",
		advertising.CodeMediaTooSmall:   "600",
		advertising.CodeMediaTooLarge:   "30 MB",
		advertising.CodeVideoTooShort:   "1 segundo",
		advertising.CodeVideoTooLong:    "241 minutos",
		advertising.CodeMediaFormat:     "MP4",
		advertising.CodeMediaUnreadable: "não pôde ser lido",
	}
	for code, want := range cases {
		message := adsMessage(advertising.FieldError("ads[0].creative.media", code))
		if !strings.Contains(message, want) {
			t.Errorf("%s: %q does not mention %q", code, message, want)
		}
	}
	for _, code := range advertising.MediaSpecCodes() {
		if _, ok := cases[code]; !ok {
			t.Errorf("code %s has no explanation test", code)
		}
	}
}

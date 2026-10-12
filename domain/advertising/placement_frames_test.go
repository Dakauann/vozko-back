package advertising

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

type placementFixture struct {
	LegibleTextPx int              `json:"legibleTextPx"`
	Frames        []PlacementFrame `json:"frames"`
	Layouts       []struct {
		Frame  string      `json:"frame"`
		Width  int         `json:"width"`
		Height int         `json:"height"`
		Expect FrameLayout `json:"expect"`
	} `json:"layouts"`
	Plans []struct {
		Placements Placements          `json:"placements"`
		Frames     []PlannedFrameIDs   `json:"frames"`
		VideoOnly  []PlacementPosition `json:"videoOnly"`
	} `json:"plans"`
}

type PlannedFrameIDs struct {
	ID        string              `json:"id"`
	Positions []PlacementPosition `json:"positions"`
}

func loadPlacementFixture(t *testing.T) placementFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "placement_frames.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f placementFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func frameByID(t *testing.T, id string) PlacementFrame {
	t.Helper()
	for _, f := range PlacementFrames() {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("no frame %q", id)
	return PlacementFrame{}
}

func TestThePlacementFrameTableMatchesTheSharedFixture(t *testing.T) {
	f := loadPlacementFixture(t)
	if f.LegibleTextPx != LegibleTextPx {
		t.Fatalf("legible text %d, fixture %d", LegibleTextPx, f.LegibleTextPx)
	}
	if !reflect.DeepEqual(PlacementFrames(), f.Frames) {
		got, _ := json.Marshal(PlacementFrames())
		t.Fatalf("frames drifted from the fixture the editor also reads:\n%s", got)
	}
}

func TestEveryImagePositionBelongsToExactlyOneFrame(t *testing.T) {
	seen := map[PlacementPosition]string{}
	for _, f := range PlacementFrames() {
		for _, p := range f.Positions {
			if other, ok := seen[p]; ok {
				t.Errorf("%v sits in %s and %s", p, other, f.ID)
			}
			seen[p] = f.ID
		}
	}
	videoOnly := VideoOnlyPositions()
	for platform, positions := range PlatformPositions() {
		for _, position := range positions {
			p := PlacementPosition{Platform: platform, Position: position}
			_, framed := seen[p]
			only := slices.Contains(videoOnly[platform], position)
			if framed == only {
				t.Errorf("%v must be in a frame unless it only runs video (framed %v, video only %v)", p, framed, only)
			}
		}
	}
	for p := range seen {
		if !slices.Contains(PlatformPositions()[p.Platform], p.Position) {
			t.Errorf("%v is not a placement Meta accepts", p)
		}
	}
}

func TestFrameLayoutsMatchTheSharedFixture(t *testing.T) {
	for _, c := range loadPlacementFixture(t).Layouts {
		got, err := frameByID(t, c.Frame).Layout(c.Width, c.Height)
		if err != nil {
			t.Fatalf("%s %dx%d: %v", c.Frame, c.Width, c.Height, err)
		}
		c.Expect.Frame = c.Frame
		if !reflect.DeepEqual(got, c.Expect) {
			t.Errorf("%s %dx%d:\n got %+v\nwant %+v", c.Frame, c.Width, c.Height, got, c.Expect)
		}
	}
}

func TestALayoutNeedsARealImage(t *testing.T) {
	for _, size := range [][2]int{{0, 100}, {100, 0}, {-5, 100}} {
		if _, err := frameByID(t, "feed").Layout(size[0], size[1]); err == nil {
			t.Errorf("%v must be refused", size)
		}
	}
}

func TestPlacementPlansMatchTheSharedFixture(t *testing.T) {
	for _, c := range loadPlacementFixture(t).Plans {
		plan := PlanPlacementFrames(c.Placements)
		ids := make([]PlannedFrameIDs, 0, len(plan.Frames))
		for _, pf := range plan.Frames {
			ids = append(ids, PlannedFrameIDs{ID: pf.Frame.ID, Positions: pf.Positions})
		}
		if !reflect.DeepEqual(ids, c.Frames) {
			t.Errorf("%+v: frames %+v, want %+v", c.Placements, ids, c.Frames)
		}
		if !reflect.DeepEqual(append([]PlacementPosition{}, plan.VideoOnly...), append([]PlacementPosition{}, c.VideoOnly...)) {
			t.Errorf("%+v: video only %+v, want %+v", c.Placements, plan.VideoOnly, c.VideoOnly)
		}
	}
}

func uniformDetail(cols, rows int) DetailMap {
	cells := make([]float64, cols*rows)
	for i := range cells {
		cells[i] = 1
	}
	return DetailMap{Cols: cols, Rows: rows, Cells: cells}
}

func bandDetail(cols, rows, fromRow, toRow int) DetailMap {
	return blockDetail(cols, rows, 0, cols, fromRow, toRow)
}

func blockDetail(cols, rows, fromCol, toCol, fromRow, toRow int) DetailMap {
	m := DetailMap{Cols: cols, Rows: rows, Cells: make([]float64, cols*rows)}
	for r := fromRow; r < toRow; r++ {
		for c := fromCol; c < toCol; c++ {
			m.Cells[r*cols+c] = 1
		}
	}
	return m
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.02 }

func TestDetailCutByTheCropIsTheShareOutsideIt(t *testing.T) {
	layout, _ := frameByID(t, "square").Layout(1080, 1350)
	d := layout.Detail(uniformDetail(20, 25))
	if !near(d.Cut, 0.2) || d.UnderUI != 0 {
		t.Fatalf("got %+v", d)
	}
}

func TestTextAtTheBottomOfAStoryFallsUnderTheCaption(t *testing.T) {
	layout, _ := frameByID(t, "vertical").Layout(1080, 1920)
	d := layout.Detail(bandDetail(10, 40, 32, 38))
	if d.Cut != 0 || !near(d.UnderUI, 1) {
		t.Fatalf("text in the bottom fifth must sit under the caption, got %+v", d)
	}
	if d.Bounds == nil || d.Bounds.Y0 < 0.65 {
		t.Fatalf("bounds must report where the detail sits, got %+v", d.Bounds)
	}
	codes := layout.Issues(d)
	if !slices.Contains(codes, IssueDetailUnderUI) {
		t.Fatalf("issues %v", codes)
	}
}

func TestDetailInsideTheSafeAreaRaisesNothing(t *testing.T) {
	layout, _ := frameByID(t, "vertical").Layout(1080, 1920)
	d := layout.Detail(blockDetail(10, 40, 2, 8, 12, 20))
	if d.UnderUI != 0 || len(layout.Issues(d)) != 0 {
		t.Fatalf("got %+v and %v", d, layout.Issues(d))
	}
}

func TestASquareImageInAStoryReportsTheBackgroundAndMapsDetailIntoTheFrame(t *testing.T) {
	layout, _ := frameByID(t, "vertical").Layout(1080, 1080)
	d := layout.Detail(bandDetail(10, 10, 8, 10))
	if d.Bounds == nil || !near(d.Bounds.Y1, 0.78) || !near(d.Bounds.Y0, 0.67) {
		t.Fatalf("detail at the bottom of a square must land just above the frame's lower band, got %+v", d.Bounds)
	}
	if !near(d.UnderUI, 1) {
		t.Fatalf("got %+v", d)
	}
	if !slices.Contains(layout.Issues(d), IssueBackground) {
		t.Fatalf("a large filled background must be reported, got %v", layout.Issues(d))
	}
}

func TestAFeedCropOfAVerticalImageCutsTheEdges(t *testing.T) {
	layout, _ := frameByID(t, "feed").Layout(1080, 1920)
	d := layout.Detail(bandDetail(10, 40, 0, 4))
	if !near(d.Cut, 1) || d.Bounds != nil {
		t.Fatalf("detail only at the very top is cut away, got %+v", d)
	}
	if !slices.Contains(layout.Issues(d), IssueDetailCut) {
		t.Fatalf("got %v", layout.Issues(d))
	}
}

func TestAnEmptyOrMalformedDetailMapMeasuresNothing(t *testing.T) {
	layout, _ := frameByID(t, "feed").Layout(1080, 1350)
	for _, m := range []DetailMap{{}, {Cols: 2, Rows: 2, Cells: []float64{1}}, bandDetail(4, 4, 0, 0)} {
		if d := layout.Detail(m); d.Cut != 0 || d.UnderUI != 0 || d.Bounds != nil {
			t.Errorf("%+v measured %+v", m, d)
		}
	}
}

func TestATooSmallImageIsReported(t *testing.T) {
	layout, _ := frameByID(t, "right_column").Layout(240, 240)
	if !slices.Contains(layout.Issues(FrameDetail{}), IssueBelowMinimum) {
		t.Fatal("below the minimum must be reported")
	}
}

func TestEveryIssueCodeIsListed(t *testing.T) {
	want := []string{IssueDetailCut, IssueDetailUnderUI, IssueBackground, IssueBelowMinimum}
	if !reflect.DeepEqual(PlacementIssueCodes(), want) {
		t.Fatalf("got %v", PlacementIssueCodes())
	}
}

func TestAnUnknownPlatformIsNamedInThePlan(t *testing.T) {
	plan := PlanPlacementFrames(Placements{Platforms: []string{"instagram", "tiktok"}})
	if !reflect.DeepEqual(plan.Unknown, []string{"tiktok"}) || len(plan.Frames) == 0 {
		t.Fatalf("got %+v", plan)
	}
}

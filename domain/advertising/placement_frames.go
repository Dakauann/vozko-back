package advertising

import (
	"errors"
	"math"
	"slices"
	"sort"
)

type FrameFit string

const (
	FitCrop FrameFit = "crop"
	FitFill FrameFit = "fill"
)

const (
	LegibleTextPx = 12

	IssueDetailCut     = "detail_cut"
	IssueDetailUnderUI = "detail_under_ui"
	IssueBackground    = "background"
	IssueBelowMinimum  = "below_minimum"

	detailCutLimit     = 0.10
	detailUnderUILimit = 0.10
	backgroundLimit    = 0.25
	boundsTrim         = 0.03
	layoutPrecision    = 10_000
)

var ErrNoImageSize = errors.New("ads: the image has no size")

type PixelSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type SafeInsets struct {
	Top    float64 `json:"top"`
	Bottom float64 `json:"bottom"`
	Side   float64 `json:"side"`
}

type PlacementPosition struct {
	Platform string `json:"platform"`
	Position string `json:"position"`
}

type PlacementFrame struct {
	ID           string              `json:"id"`
	Fit          FrameFit            `json:"fit"`
	MinRatio     float64             `json:"minRatio"`
	MaxRatio     float64             `json:"maxRatio"`
	Tolerance    float64             `json:"tolerance"`
	DisplayWidth int                 `json:"displayWidth"`
	Recommended  PixelSize           `json:"recommended"`
	Minimum      PixelSize           `json:"minimum"`
	Safe         *SafeInsets         `json:"safe"`
	Positions    []PlacementPosition `json:"positions"`
}

type FrameRect struct {
	X0 float64 `json:"x0"`
	Y0 float64 `json:"y0"`
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
}

type FrameLayout struct {
	Frame        string    `json:"frame"`
	Ratio        float64   `json:"ratio"`
	Crop         FrameRect `json:"crop"`
	Image        FrameRect `json:"image"`
	Safe         FrameRect `json:"safe"`
	CropLoss     float64   `json:"cropLoss"`
	Background   float64   `json:"background"`
	Visible      PixelSize `json:"visible"`
	MinTextPx    int       `json:"minTextPx"`
	BelowMinimum bool      `json:"belowMinimum"`
}

type DetailMap struct {
	Cols  int
	Rows  int
	Cells []float64
}

type FrameDetail struct {
	Cut     float64
	UnderUI float64
	Bounds  *FrameRect
}

type PlannedFrame struct {
	Frame     PlacementFrame
	Positions []PlacementPosition
}

type PlacementPlan struct {
	Frames    []PlannedFrame
	VideoOnly []PlacementPosition
	Unknown   []string
}

var wholeRect = FrameRect{X0: 0, Y0: 0, X1: 1, Y1: 1}

func at(platform string, positions ...string) []PlacementPosition {
	out := make([]PlacementPosition, 0, len(positions))
	for _, p := range positions {
		out = append(out, PlacementPosition{Platform: platform, Position: p})
	}
	return out
}

func joined(groups ...[]PlacementPosition) []PlacementPosition {
	var out []PlacementPosition
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

var placementFrames = []PlacementFrame{
	{ID: "feed", Fit: FitCrop, MinRatio: 0.8, MaxRatio: 1.91, Tolerance: 0.01, DisplayWidth: 390,
		Recommended: PixelSize{1440, 1800}, Minimum: PixelSize{600, 0},
		Positions: joined(at(PlatformFacebook, "feed", "profile_feed"), at(PlatformInstagram, "stream", "profile_feed", "explore_home", "ig_search"), at(PlatformThreads, "threads_stream"))},
	{ID: "vertical", Fit: FitFill, MinRatio: 0.5625, MaxRatio: 0.5625, Tolerance: 0.01, DisplayWidth: 390,
		Recommended: PixelSize{1440, 2560}, Minimum: PixelSize{500, 0}, Safe: &SafeInsets{Top: 0.14, Bottom: 0.35, Side: 0.06},
		Positions: joined(at(PlatformFacebook, "story", "facebook_reels"), at(PlatformInstagram, "story", "reels", "profile_reels"))},
	{ID: "square", Fit: FitCrop, MinRatio: 1, MaxRatio: 1, Tolerance: 0.01, DisplayWidth: 180,
		Recommended: PixelSize{1080, 1080}, Positions: at(PlatformFacebook, "marketplace")},
	{ID: "right_column", Fit: FitCrop, MinRatio: 1, MaxRatio: 1, Tolerance: 0.01, DisplayWidth: 254,
		Recommended: PixelSize{1080, 1080}, Minimum: PixelSize{254, 133}, Positions: at(PlatformFacebook, "right_hand_column")},
	{ID: "search", Fit: FitCrop, MinRatio: 1, MaxRatio: 1.91, Tolerance: 0.03, DisplayWidth: 390,
		Recommended: PixelSize{1080, 1080}, Minimum: PixelSize{600, 600}, Positions: at(PlatformFacebook, "search")},
	{ID: "messenger", Fit: FitCrop, MinRatio: 1, MaxRatio: 1.91, Tolerance: 0.03, DisplayWidth: 260,
		Recommended: PixelSize{1200, 628}, Positions: at(PlatformMessenger, "sponsored_messages")},
	{ID: "audience_network", Fit: FitCrop, MinRatio: 1, MaxRatio: 1.91, Tolerance: 0.03, DisplayWidth: 320,
		Recommended: PixelSize{1200, 628}, Minimum: PixelSize{398, 208}, Positions: at(PlatformAudienceNetwork, "classic")},
	{ID: "thumbnail", Fit: FitCrop, MinRatio: 1, MaxRatio: 1, Tolerance: 0.01, DisplayWidth: 96,
		Recommended: PixelSize{1080, 1080}, Positions: at(PlatformFacebook, "notification", "facebook_reels_overlay")},
}

func PlacementFrames() []PlacementFrame {
	out := make([]PlacementFrame, 0, len(placementFrames))
	for _, f := range placementFrames {
		f.Positions = slices.Clone(f.Positions)
		if f.Safe != nil {
			safe := *f.Safe
			f.Safe = &safe
		}
		out = append(out, f)
	}
	return out
}

func PlacementFrameByID(id string) (PlacementFrame, bool) {
	for _, f := range PlacementFrames() {
		if f.ID == id {
			return f, true
		}
	}
	return PlacementFrame{}, false
}

func PlacementIssueCodes() []string {
	return []string{IssueDetailCut, IssueDetailUnderUI, IssueBackground, IssueBelowMinimum}
}

func (p Placements) chosenPositions() []PlacementPosition {
	platforms := p.Platforms
	if p.Automatic {
		platforms = automaticPlatforms
	}
	var out []PlacementPosition
	for _, platform := range platforms {
		chosen := platformPositions[platform]
		if !p.Automatic && len(p.Positions[platform]) > 0 {
			chosen = p.Positions[platform]
		}
		out = append(out, at(platform, chosen...)...)
	}
	return out
}

func PlanPlacementFrames(p Placements) PlacementPlan {
	chosen := p.chosenPositions()
	plan := PlacementPlan{}
	for _, platform := range p.Platforms {
		if _, known := platformPositions[platform]; !known && !p.Automatic {
			plan.Unknown = append(plan.Unknown, platform)
		}
	}
	for _, position := range chosen {
		if slices.Contains(videoOnlyPositions[position.Platform], position.Position) {
			plan.VideoOnly = append(plan.VideoOnly, position)
		}
	}
	for _, frame := range PlacementFrames() {
		var positions []PlacementPosition
		for _, position := range frame.Positions {
			if slices.Contains(chosen, position) {
				positions = append(positions, position)
			}
		}
		if len(positions) > 0 {
			plan.Frames = append(plan.Frames, PlannedFrame{Frame: frame, Positions: positions})
		}
	}
	return plan
}

func rounded(v float64) float64 {
	return math.Round(v*layoutPrecision) / layoutPrecision
}

func (r FrameRect) rounded() FrameRect {
	return FrameRect{X0: rounded(r.X0), Y0: rounded(r.Y0), X1: rounded(r.X1), Y1: rounded(r.Y1)}
}

func (r FrameRect) width() float64  { return r.X1 - r.X0 }
func (r FrameRect) height() float64 { return r.Y1 - r.Y0 }
func (r FrameRect) area() float64   { return max(r.width(), 0) * max(r.height(), 0) }

func (r FrameRect) intersect(o FrameRect) FrameRect {
	return FrameRect{X0: max(r.X0, o.X0), Y0: max(r.Y0, o.Y0), X1: min(r.X1, o.X1), Y1: min(r.Y1, o.Y1)}
}

func centered(share float64, horizontal bool) FrameRect {
	lo, hi := (1-share)/2, (1+share)/2
	if horizontal {
		return FrameRect{X0: lo, Y0: 0, X1: hi, Y1: 1}
	}
	return FrameRect{X0: 0, Y0: lo, X1: 1, Y1: hi}
}

func (f PlacementFrame) safeRect() FrameRect {
	if f.Safe == nil {
		return wholeRect
	}
	return FrameRect{X0: f.Safe.Side, Y0: f.Safe.Top, X1: 1 - f.Safe.Side, Y1: 1 - f.Safe.Bottom}
}

func coverCrop(ratio, target float64) FrameRect {
	switch {
	case target < ratio:
		return centered(target/ratio, true)
	case target > ratio:
		return centered(ratio/target, false)
	}
	return wholeRect
}

func (f PlacementFrame) Layout(width, height int) (FrameLayout, error) {
	if width <= 0 || height <= 0 {
		return FrameLayout{}, ErrNoImageSize
	}
	ratio := float64(width) / float64(height)
	low, high := f.MinRatio*(1-f.Tolerance), f.MaxRatio*(1+f.Tolerance)
	target := ratio
	switch {
	case ratio < low:
		target = f.MinRatio
	case ratio > high:
		target = f.MaxRatio
	}
	crop, image, frameRatio, background := coverCrop(ratio, target), wholeRect, target, 0.0
	if f.Fit == FitFill {
		frameRatio = f.MinRatio
		if ratio > high {
			share := f.MinRatio / ratio
			crop, image, background = wholeRect, centered(share, false), 1-share
		}
	}
	visible := PixelSize{Width: int(math.Round(float64(width) * crop.width())), Height: int(math.Round(float64(height) * crop.height()))}
	return FrameLayout{
		Frame: f.ID, Ratio: rounded(frameRatio), Crop: crop.rounded(), Image: image.rounded(), Safe: f.safeRect(),
		CropLoss: rounded(1 - crop.area()), Background: rounded(background), Visible: visible,
		MinTextPx:    int(math.Ceil(float64(LegibleTextPx*visible.Width)/float64(f.DisplayWidth) - 1e-9)),
		BelowMinimum: (f.Minimum.Width > 0 && visible.Width < f.Minimum.Width) || (f.Minimum.Height > 0 && visible.Height < f.Minimum.Height),
	}, nil
}

type detailPiece struct {
	rect FrameRect
	mass float64
}

func (l FrameLayout) toFrame(r FrameRect) FrameRect {
	sx := l.Image.width() / l.Crop.width()
	sy := l.Image.height() / l.Crop.height()
	return FrameRect{
		X0: l.Image.X0 + (r.X0-l.Crop.X0)*sx, Y0: l.Image.Y0 + (r.Y0-l.Crop.Y0)*sy,
		X1: l.Image.X0 + (r.X1-l.Crop.X0)*sx, Y1: l.Image.Y0 + (r.Y1-l.Crop.Y0)*sy,
	}
}

func (m DetailMap) valid() bool {
	return m.Cols > 0 && m.Rows > 0 && len(m.Cells) == m.Cols*m.Rows
}

func (m DetailMap) cell(col, row int) FrameRect {
	return FrameRect{
		X0: float64(col) / float64(m.Cols), Y0: float64(row) / float64(m.Rows),
		X1: float64(col+1) / float64(m.Cols), Y1: float64(row+1) / float64(m.Rows),
	}
}

func (l FrameLayout) Detail(m DetailMap) FrameDetail {
	if !m.valid() {
		return FrameDetail{}
	}
	var total, cut, under float64
	var pieces []detailPiece
	for row := 0; row < m.Rows; row++ {
		for col := 0; col < m.Cols; col++ {
			mass := m.Cells[row*m.Cols+col]
			if mass <= 0 {
				continue
			}
			cell := m.cell(col, row)
			total += mass
			shown := cell.intersect(l.Crop)
			share := shown.area() / cell.area()
			cut += mass * (1 - share)
			if share <= 0 {
				continue
			}
			placed := l.toFrame(shown)
			kept := mass * share
			under += kept * (1 - placed.intersect(l.Safe).area()/placed.area())
			pieces = append(pieces, detailPiece{rect: placed, mass: kept})
		}
	}
	if total <= 0 {
		return FrameDetail{}
	}
	d := FrameDetail{Cut: rounded(cut / total)}
	if shown := total - cut; shown > 0 {
		d.UnderUI = rounded(under / shown)
		bounds := trimmedBounds(pieces, shown).rounded()
		d.Bounds = &bounds
	}
	return d
}

func trimmedEdge(pieces []detailPiece, total float64, edge func(FrameRect) float64, ascending bool) float64 {
	sorted := slices.Clone(pieces)
	sort.SliceStable(sorted, func(i, j int) bool {
		if ascending {
			return edge(sorted[i].rect) < edge(sorted[j].rect)
		}
		return edge(sorted[i].rect) > edge(sorted[j].rect)
	})
	skipped := 0.0
	for _, p := range sorted {
		if skipped+p.mass > boundsTrim*total {
			return edge(p.rect)
		}
		skipped += p.mass
	}
	return edge(sorted[len(sorted)-1].rect)
}

func trimmedBounds(pieces []detailPiece, total float64) FrameRect {
	return FrameRect{
		X0: trimmedEdge(pieces, total, func(r FrameRect) float64 { return r.X0 }, true),
		Y0: trimmedEdge(pieces, total, func(r FrameRect) float64 { return r.Y0 }, true),
		X1: trimmedEdge(pieces, total, func(r FrameRect) float64 { return r.X1 }, false),
		Y1: trimmedEdge(pieces, total, func(r FrameRect) float64 { return r.Y1 }, false),
	}
}

func (l FrameLayout) Issues(d FrameDetail) []string {
	var out []string
	if d.Cut >= detailCutLimit {
		out = append(out, IssueDetailCut)
	}
	if d.UnderUI >= detailUnderUILimit {
		out = append(out, IssueDetailUnderUI)
	}
	if l.Background >= backgroundLimit {
		out = append(out, IssueBackground)
	}
	if l.BelowMinimum {
		out = append(out, IssueBelowMinimum)
	}
	return out
}

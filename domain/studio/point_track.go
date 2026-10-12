package studio

import (
	"sort"
	"unicode/utf8"
)

const (
	MinTrackerSize   = 0.01
	MaxTrackerSize   = 0.5
	MaxTrackerSide   = 16384
	MinStabilizeZoom = 1.0
	MaxStabilizeZoom = 4.0
	MinSmoothMS      = 100
	MaxSmoothMS      = 10_000
)

type TrackSample struct {
	AtMS       int64   `json:"atMs"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Confidence float64 `json:"confidence"`
}

type PointTracker struct {
	ID          string        `json:"id"`
	Name        string        `json:"name,omitempty"`
	FrameWidth  int64         `json:"frameWidth"`
	FrameHeight int64         `json:"frameHeight"`
	Size        float64       `json:"size"`
	ReferenceMS int64         `json:"referenceMs"`
	Samples     []TrackSample `json:"samples"`
}

type Stabilize struct {
	TrackerID       string  `json:"trackerId"`
	SecondTrackerID string  `json:"secondTrackerId,omitempty"`
	Rotation        bool    `json:"rotation,omitempty"`
	Scale           bool    `json:"scale,omitempty"`
	SmoothMS        int64   `json:"smoothMs,omitempty"`
	Zoom            float64 `json:"zoom"`
}

func (c Clip) tracker(id string) (PointTracker, bool) {
	for _, t := range c.Trackers {
		if t.ID == id {
			return t, true
		}
	}
	return PointTracker{}, false
}

func (t PointTracker) samplesIssue() string {
	if len(t.Samples) == 0 {
		return CodeInvalid
	}
	for i, s := range t.Samples {
		if s.AtMS < 0 || (i > 0 && s.AtMS <= t.Samples[i-1].AtMS) {
			return CodeInvalid
		}
		if !within(s.X, 0, 1) || !within(s.Y, 0, 1) || !within(s.Confidence, 0, 1) {
			return CodeOutOfRange
		}
	}
	at := sort.Search(len(t.Samples), func(i int) bool { return t.Samples[i].AtMS >= t.ReferenceMS })
	if at == len(t.Samples) || t.Samples[at].AtMS != t.ReferenceMS {
		return CodeInvalid
	}
	return ""
}

func (t PointTracker) issue() string {
	switch {
	case !validToken(t.ID) || utf8.RuneCountInString(t.Name) > maxTokenRunes*2:
		return CodeInvalid
	case t.FrameWidth < 1 || t.FrameWidth > MaxTrackerSide || t.FrameHeight < 1 || t.FrameHeight > MaxTrackerSide:
		return CodeInvalid
	case !within(t.Size, MinTrackerSize, MaxTrackerSize):
		return CodeOutOfRange
	}
	return t.samplesIssue()
}

func (c Clip) trackersIssue() string {
	if len(c.Trackers) == 0 {
		return ""
	}
	if c.Type != ClipVideo {
		return CodeInvalid
	}
	seen := map[string]bool{}
	for _, t := range c.Trackers {
		if seen[t.ID] {
			return CodeInvalid
		}
		seen[t.ID] = true
		if code := t.issue(); code != "" {
			return code
		}
	}
	return ""
}

func (c Clip) stabilizeIssue() string {
	s := c.Stabilize
	if s == nil {
		return ""
	}
	if c.Type != ClipVideo || c.Freeze {
		return CodeInvalid
	}
	first, ok := c.tracker(s.TrackerID)
	if !ok {
		return CodeInvalid
	}
	if s.SecondTrackerID != "" {
		second, found := c.tracker(s.SecondTrackerID)
		if !found || second.ID == first.ID || second.FrameWidth != first.FrameWidth || second.FrameHeight != first.FrameHeight {
			return CodeInvalid
		}
	} else if s.Rotation || s.Scale {
		return CodeInvalid
	}
	if s.SmoothMS != 0 && (s.SmoothMS < MinSmoothMS || s.SmoothMS > MaxSmoothMS) {
		return CodeOutOfRange
	}
	if !within(s.Zoom, MinStabilizeZoom, MaxStabilizeZoom) {
		return CodeOutOfRange
	}
	return ""
}

func (c Clip) trackingIssue() string {
	if code := c.trackersIssue(); code != "" {
		return code
	}
	return c.stabilizeIssue()
}

func parentTrackerBroken(lookup map[string]Clip, c Clip) bool {
	if c.ParentTracker == "" {
		return false
	}
	parent, ok := lookup[c.ParentID]
	if c.ParentID == "" || !ok {
		return true
	}
	_, found := parent.tracker(c.ParentTracker)
	return !found
}

package mediagen

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

const (
	MaxVideoMS       = 90_000
	MinClipMS        = 100
	MaxVisualTracks  = 8
	MaxAudioTracks   = 6
	MaxTimelineClips = 80
	MaxVolume        = 2.0
	MaxScenes        = 10
	MinSceneSeconds  = 1.0
	MaxSceneSeconds  = 15.0
	musicUnderVoice  = 0.25
	musicFadeOutMS   = 1500
)

type Fit string

const (
	FitCover   Fit = "cover"
	FitContain Fit = "contain"
)

var backgroundColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type Transform struct {
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	W        float64 `json:"w"`
	H        float64 `json:"h"`
	Rotation float64 `json:"rotation"`
	Opacity  float64 `json:"opacity"`
}

func FullFrame() Transform { return Transform{X: 0.5, Y: 0.5, W: 1, H: 1, Opacity: 1} }

type Clip struct {
	MediaID    string     `json:"mediaId"`
	StartMS    int64      `json:"startMs"`
	DurationMS int64      `json:"durationMs"`
	TrimInMS   int64      `json:"trimInMs"`
	Fit        Fit        `json:"fit,omitempty"`
	Transform  Transform  `json:"transform"`
	Volume     float64    `json:"volume"`
	FadeInMS   int64      `json:"fadeInMs"`
	FadeOutMS  int64      `json:"fadeOutMs"`
	MotionIn   *Motion    `json:"motionIn,omitempty"`
	MotionOut  *Motion    `json:"motionOut,omitempty"`
	Keyframes  *Keyframes `json:"keyframes,omitempty"`
}

func (c Clip) EndMS() int64 { return c.StartMS + c.DurationMS }

type Track struct {
	Clips []Clip `json:"clips"`
}

type Timeline struct {
	DurationMS int64   `json:"durationMs"`
	Background string  `json:"background"`
	Visual     []Track `json:"visual"`
	Audio      []Track `json:"audio"`
}

type Scene struct {
	MediaID string
	Seconds float64
}

func SlideshowTimeline(scenes []Scene, musicMediaID, voiceMediaID string) Timeline {
	visual := Track{}
	var at int64
	for _, s := range scenes {
		length := int64(math.Round(s.Seconds * 1000))
		visual.Clips = append(visual.Clips, Clip{MediaID: strings.TrimSpace(s.MediaID), StartMS: at, DurationMS: length, Fit: FitCover, Transform: FullFrame()})
		at += length
	}
	timeline := Timeline{DurationMS: at, Background: "#000000", Visual: []Track{visual}}
	music, voice := strings.TrimSpace(musicMediaID), strings.TrimSpace(voiceMediaID)
	if music != "" {
		volume := 1.0
		if voice != "" {
			volume = musicUnderVoice
		}
		timeline.Audio = append(timeline.Audio, Track{Clips: []Clip{{MediaID: music, DurationMS: at, Volume: volume, FadeOutMS: min(musicFadeOutMS, at)}}})
	}
	if voice != "" {
		timeline.Audio = append(timeline.Audio, Track{Clips: []Clip{{MediaID: voice, DurationMS: at, Volume: 1}}})
	}
	return timeline
}

func SlideshowIssues(scenes []Scene) string {
	switch {
	case len(scenes) == 0:
		return CodeRequired
	case len(scenes) > MaxScenes:
		return CodeTooMany
	}
	for _, s := range scenes {
		if strings.TrimSpace(s.MediaID) == "" {
			return CodeRequired
		}
		if s.Seconds < MinSceneSeconds || s.Seconds > MaxSceneSeconds {
			return CodeOutOfRange
		}
	}
	return ""
}

func (t Timeline) MediaIDs() []string {
	var ids []string
	for _, tracks := range [][]Track{t.Visual, t.Audio} {
		for _, track := range tracks {
			for _, c := range track.Clips {
				ids = append(ids, c.MediaID)
			}
		}
	}
	return ids
}

func (t Timeline) normalized() Timeline {
	out := Timeline{DurationMS: t.DurationMS, Background: strings.ToLower(strings.TrimSpace(t.Background))}
	trim := func(tracks []Track) []Track {
		result := make([]Track, 0, len(tracks))
		for _, track := range tracks {
			clips := make([]Clip, 0, len(track.Clips))
			for _, c := range track.Clips {
				c.MediaID = strings.TrimSpace(c.MediaID)
				clips = append(clips, c)
			}
			result = append(result, Track{Clips: clips})
		}
		return result
	}
	out.Visual, out.Audio = trim(t.Visual), trim(t.Audio)
	return out
}

func (t Timeline) fingerprintParts() []string {
	if t.DurationMS == 0 && len(t.Visual) == 0 && len(t.Audio) == 0 {
		return nil
	}
	parts := []string{strconv.FormatInt(t.DurationMS, 10), t.Background}
	add := func(prefix string, tracks []Track) {
		for i, track := range tracks {
			for _, c := range track.Clips {
				parts = append(parts, prefix+strconv.Itoa(i)+":"+c.MediaID+"@"+clipNumbers(c))
			}
		}
	}
	add("v", t.Visual)
	add("a", t.Audio)
	return parts
}

func clipNumbers(c Clip) string {
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', 4, 64) }
	i := func(v int64) string { return strconv.FormatInt(v, 10) }
	return strings.Join([]string{
		i(c.StartMS), i(c.DurationMS), i(c.TrimInMS), string(c.Fit),
		f(c.Transform.X), f(c.Transform.Y), f(c.Transform.W), f(c.Transform.H), f(c.Transform.Rotation), f(c.Transform.Opacity),
		f(c.Volume), i(c.FadeInMS), i(c.FadeOutMS), motionPart(c.MotionIn), motionPart(c.MotionOut), keyframesPart(c.Keyframes),
	}, ",")
}

func videoIssues(r Request) []FieldIssue {
	issues := aspectIssues(r.Aspect)
	if code := r.Video.normalized().issue(); code != "" {
		issues = append(issues, FieldIssue{Field: FieldTimeline, Code: code})
	}
	return issues
}

func (t Timeline) issue() string {
	switch {
	case t.DurationMS < MinClipMS:
		return CodeRequired
	case t.DurationMS > MaxVideoMS:
		return CodeTooLong
	case !backgroundColor.MatchString(t.Background):
		return CodeUnknown
	case len(t.Visual) == 0 || len(t.Visual) > MaxVisualTracks || len(t.Audio) > MaxAudioTracks:
		return CodeTooMany
	}
	total, keyframes := 0, 0
	for _, track := range t.Visual {
		total += len(track.Clips)
		for _, c := range track.Clips {
			keyframes += c.Keyframes.Count()
		}
		if code := track.issue(t.DurationMS, true); code != "" {
			return code
		}
	}
	for _, track := range t.Audio {
		total += len(track.Clips)
		if code := track.issue(t.DurationMS, false); code != "" {
			return code
		}
	}
	if total == 0 {
		return CodeRequired
	}
	if total > MaxTimelineClips || keyframes > MaxTimelineKeyframes {
		return CodeTooMany
	}
	return ""
}

func (tr Track) issue(durationMS int64, visual bool) string {
	var lastEnd int64
	for i, c := range tr.Clips {
		if code := c.issue(durationMS, visual); code != "" {
			return code
		}
		if i > 0 && c.StartMS < lastEnd {
			return CodeOverlap
		}
		lastEnd = c.EndMS()
	}
	return ""
}

func (c Clip) issue(durationMS int64, visual bool) string {
	switch {
	case c.MediaID == "":
		return CodeRequired
	case c.StartMS < 0 || c.TrimInMS < 0 || c.DurationMS < MinClipMS || c.EndMS() > durationMS:
		return CodeOutOfRange
	case c.FadeInMS < 0 || c.FadeOutMS < 0 || c.FadeInMS+c.FadeOutMS > c.DurationMS:
		return CodeOutOfRange
	case c.Volume < 0 || c.Volume > MaxVolume || math.IsNaN(c.Volume):
		return CodeOutOfRange
	}
	if !visual {
		if c.MotionIn != nil || c.MotionOut != nil || c.Keyframes != nil {
			return CodeUnexpected
		}
		return ""
	}
	if code := MotionIssue(c.MotionIn, c.MotionOut, c.DurationMS); code != "" {
		return code
	}
	if c.Fit != FitCover && c.Fit != FitContain {
		return CodeUnknown
	}
	if code := KeyframesIssue(c.Keyframes, c.Transform); code != "" {
		return code
	}
	return c.Transform.issue()
}

func (t Transform) issue() string {
	inUnit := func(v float64) bool { return v >= 0 && v <= 1 && !math.IsNaN(v) }
	switch {
	case !inUnit(t.X) || !inUnit(t.Y) || !inUnit(t.Opacity):
		return CodeOutOfRange
	case t.W <= 0 || t.H <= 0 || t.W > 4 || t.H > 4 || math.IsNaN(t.W) || math.IsNaN(t.H):
		return CodeOutOfRange
	case t.Rotation < -360 || t.Rotation > 360 || math.IsNaN(t.Rotation):
		return CodeOutOfRange
	}
	return ""
}

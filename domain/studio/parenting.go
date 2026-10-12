package studio

import "vozko/domain/mediagen"

const ClipNull ClipType = "null"

var nullClipFields = withClipTags(map[string]bool{
	"id": true, "type": true, "startMs": true, "durationMs": true, "trimInMs": true, "volume": true,
	"fadeInMs": true, "fadeOutMs": true, "transform": true, "keyframes": true, "anchor": true,
	"autoOrient": true, "parentId": true, "parentTracker": true, "behaviors": true, "disabled": true,
}, false)

var nullLanes = map[string]bool{
	"x": true, "y": true, "position": true, "scale": true, "scaleX": true, "scaleY": true, "rotation": true,
}

func takesParent(t ClipType) bool {
	return t == ClipVideo || t == ClipImage || t == ClipOverlay || t == ClipNull || t == ClipGroup
}

func (c Clip) positionRange() (float64, float64) {
	if c.ParentID != "" {
		return mediagen.MinKeyframePosition, mediagen.MaxKeyframePosition
	}
	return 0, 1
}

func (c Clip) nullIssue() string {
	if !carriesOnly(c, nullClipFields) || c.TrimInMS != 0 || c.FadeInMS != 0 || c.FadeOutMS != 0 || c.Transform.Opacity != 1 {
		return CodeInvalid
	}
	if !carriesOnly(c.Keyframes, nullLanes) {
		return CodeInvalid
	}
	return ""
}

func loopsOrBreaks(lookup map[string]Clip, c Clip) bool {
	seen := map[string]bool{}
	for at := c.ParentID; at != "" && !seen[at]; {
		parent, ok := lookup[at]
		if !ok || at == c.ID {
			return true
		}
		seen[at] = true
		at = parent.ParentID
	}
	return false
}

func (d VideoDocument) parentsIssue() string {
	lookup := map[string]Clip{}
	for _, track := range d.Tracks {
		for _, c := range track.Clips {
			lookup[c.ID] = c
		}
	}
	for _, track := range d.Tracks {
		for _, c := range track.Clips {
			if parentTrackerBroken(lookup, c) {
				return CodeInvalid
			}
			if c.ParentID == "" {
				continue
			}
			parent, ok := lookup[c.ParentID]
			if !takesParent(c.Type) || !ok || !takesParent(parent.Type) || loopsOrBreaks(lookup, c) {
				return CodeInvalid
			}
		}
	}
	return ""
}

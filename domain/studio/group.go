package studio

const ClipGroup ClipType = "group"

var groupClipFields = withClipTags(map[string]bool{
	"id": true, "type": true, "name": true, "startMs": true, "durationMs": true, "trimInMs": true, "volume": true,
	"fadeInMs": true, "fadeOutMs": true, "transform": true, "keyframes": true, "linkId": true,
	"motionIn": true, "motionOut": true, "transitionIn": true, "transitionOut": true, "disabled": true,
	"blur": true, "speed": true, "color": true, "masks": true, "matte": true, "matteChannel": true,
	"matteInvert": true, "blendMode": true, "vfx": true, "anchor": true, "flipX": true, "flipY": true,
	"autoOrient": true, "parentId": true, "tracks": true, "rotationX": true, "rotationY": true,
}, true)

func (c Clip) nestingIssue() string {
	if c.Type != ClipGroup && len(c.Tracks) > 0 {
		return CodeInvalid
	}
	return ""
}

func (c Clip) groupIssue() string {
	if !carriesOnly(c, groupClipFields) || (c.Keyframes != nil && len(c.Keyframes.Speed) > 0) {
		return CodeInvalid
	}
	return ""
}

func (d VideoDocument) groupTimeline(group Clip) VideoDocument {
	return VideoDocument{Schema: d.Schema, Version: d.Version, Canvas: d.Canvas, DurationMS: MaxVideoMS, Tracks: group.Tracks}
}

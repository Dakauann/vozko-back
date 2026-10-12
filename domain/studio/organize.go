package studio

import (
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	MaxMarkerNoteRunes = 1000
	MaxClipMarkerMS    = int64(MaxClipSpeed) * MaxVideoMS
)

var labelColors = []string{"red", "orange", "yellow", "green", "cyan", "blue", "purple", "pink"}

func LabelColors() []string {
	return slices.Clone(labelColors)
}

type ClipMarker struct {
	Marker
	Beat bool `json:"beat,omitempty"`
}

type markerLike interface {
	marker() Marker
}

func (m Marker) marker() Marker { return m }

func (m ClipMarker) marker() Marker { return m.Marker }

func withClipTags(fields map[string]bool, guide bool) map[string]bool {
	fields["name"], fields["labelColor"], fields["markers"] = true, true, true
	if guide {
		fields["guide"] = true
	}
	return fields
}

func labelColorIssue(value string) string {
	if value == "" || slices.Contains(labelColors, value) {
		return ""
	}
	return CodeUnknown
}

func markerFieldsIssue(m Marker, maxAtMS int64) string {
	switch {
	case m.AtMS < 0 || m.AtMS > maxAtMS || m.DurationMS < 0 || m.AtMS+m.DurationMS > maxAtMS:
		return CodeOutOfRange
	case labelColorIssue(m.Color) != "":
		return CodeUnknown
	case utf8.RuneCountInString(m.Label) > MaxMarkerLabelRunes || utf8.RuneCountInString(m.Note) > MaxMarkerNoteRunes:
		return CodeTooLarge
	}
	return ""
}

func markerListIssue[M markerLike](markers []M, maxAtMS int64) string {
	seen := make(map[string]bool, len(markers))
	for _, item := range markers {
		m := item.marker()
		switch {
		case !validToken(m.ID):
			return CodeInvalid
		case seen[m.ID]:
			return CodeDuplicate
		}
		seen[m.ID] = true
		if code := markerFieldsIssue(m, maxAtMS); code != "" {
			return code
		}
	}
	return ""
}

func (c Clip) nameIssue() string {
	switch {
	case c.Name == "":
		return ""
	case strings.TrimSpace(c.Name) == "":
		return CodeInvalid
	case utf8.RuneCountInString(c.Name) > maxTokenRunes*2:
		return CodeTooLarge
	}
	return ""
}

func (c Clip) organizeIssue() string {
	if c.Guide && c.Type == ClipNull {
		return CodeInvalid
	}
	if code := c.nameIssue(); code != "" {
		return code
	}
	if code := labelColorIssue(c.LabelColor); code != "" {
		return code
	}
	return markerListIssue(c.Markers, MaxClipMarkerMS)
}

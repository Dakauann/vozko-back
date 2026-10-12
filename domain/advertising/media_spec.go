package advertising

import (
	"io"
	"slices"
)

type MediaFile interface {
	io.ReaderAt
	Size() int64
}

const (
	ContainerJPEG = "jpeg"
	ContainerPNG  = "png"
	ContainerMP4  = "mp4"
	ContainerMOV  = "mov"

	MaxImageBytes = 30 << 20
	MinImageSide  = 600
	MaxVideoBytes = 4 << 30
	MinVideoSide  = 120
	MinVideoMS    = 1_000
	MaxVideoMS    = 241 * 60 * 1_000

	MinImageRatio  = 9.0 / 16.0
	MaxImageRatio  = 1.91
	MinVideoRatio  = 9.0 / 16.0
	MaxVideoRatio  = 16.0 / 9.0
	RatioTolerance = 0.03

	CodeMediaFormat     = "media_format"
	CodeMediaTooLarge   = "media_too_large"
	CodeMediaTooSmall   = "media_too_small"
	CodeMediaRatio      = "media_ratio"
	CodeVideoTooShort   = "video_too_short"
	CodeVideoTooLong    = "video_too_long"
	CodeMediaUnreadable = "media_unreadable"
)

type MediaFacts struct {
	Kind       MediaKind
	Container  string
	Bytes      int64
	Width      int
	Height     int
	DurationMS int64
}

type mediaSpec struct {
	containers []string
	maxBytes   int64
	minSide    int
	minRatio   float64
	maxRatio   float64
	timed      bool
}

var mediaSpecs = map[MediaKind]mediaSpec{
	MediaImage: {containers: []string{ContainerJPEG, ContainerPNG}, maxBytes: MaxImageBytes, minSide: MinImageSide, minRatio: MinImageRatio, maxRatio: MaxImageRatio},
	MediaVideo: {containers: []string{ContainerMP4, ContainerMOV}, maxBytes: MaxVideoBytes, minSide: MinVideoSide, minRatio: MinVideoRatio, maxRatio: MaxVideoRatio, timed: true},
}

func MediaSpecCodes() []string {
	return []string{CodeMediaFormat, CodeMediaTooLarge, CodeMediaTooSmall, CodeMediaRatio, CodeVideoTooShort, CodeVideoTooLong, CodeMediaUnreadable}
}

func (f MediaFacts) Issue() string {
	spec, known := mediaSpecs[f.Kind]
	switch {
	case !known || !slices.Contains(spec.containers, f.Container):
		return CodeMediaFormat
	case f.Width <= 0 || f.Height <= 0 || f.Bytes <= 0 || (spec.timed && f.DurationMS <= 0):
		return CodeMediaUnreadable
	case f.Bytes > spec.maxBytes:
		return CodeMediaTooLarge
	case min(f.Width, f.Height) < spec.minSide:
		return CodeMediaTooSmall
	case !spec.fits(float64(f.Width) / float64(f.Height)):
		return CodeMediaRatio
	case spec.timed && f.DurationMS < MinVideoMS:
		return CodeVideoTooShort
	case spec.timed && f.DurationMS > MaxVideoMS:
		return CodeVideoTooLong
	}
	return ""
}

func (s mediaSpec) fits(ratio float64) bool {
	return ratio >= s.minRatio*(1-RatioTolerance) && ratio <= s.maxRatio*(1+RatioTolerance)
}

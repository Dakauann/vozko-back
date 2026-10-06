package studio

import (
	"encoding/json"

	"vozko/domain/mediagen"
)

func (p *Project) VideoRequest(rasters map[string]string) (mediagen.Request, error) {
	if p.Kind != KindVideo {
		return mediagen.Request{}, ErrNotVideo
	}
	var doc VideoDocument
	if err := json.Unmarshal(p.Document, &doc); err != nil {
		return mediagen.Request{}, ErrInvalidDocument
	}
	timeline, err := doc.Timeline(rasters)
	if err != nil {
		return mediagen.Request{}, err
	}
	return mediagen.Request{Kind: mediagen.KindVideo, WorkspaceID: p.WorkspaceID, Aspect: doc.Canvas.Aspect, Video: timeline}, nil
}

func (d VideoDocument) Timeline(rasters map[string]string) (mediagen.Timeline, error) {
	timeline := mediagen.Timeline{DurationMS: d.DurationMS, Background: d.Canvas.Background}
	for _, track := range d.Tracks {
		if track.Hidden || (track.Kind == TrackAudio && track.Muted) {
			continue
		}
		compiled := mediagen.Track{}
		for _, c := range track.Clips {
			if c.Disabled {
				continue
			}
			clip, err := c.compiled(rasters)
			if err != nil {
				return mediagen.Timeline{}, err
			}
			compiled.Clips = append(compiled.Clips, clip)
		}
		if len(compiled.Clips) == 0 {
			continue
		}
		if track.Kind == TrackAudio {
			timeline.Audio = append(timeline.Audio, compiled)
		} else {
			timeline.Visual = append(timeline.Visual, compiled)
		}
	}
	return timeline, nil
}

func (c Clip) compiled(rasters map[string]string) (mediagen.Clip, error) {
	clip := mediagen.Clip{
		MediaID: c.AssetID, StartMS: c.StartMS, DurationMS: c.DurationMS, TrimInMS: c.TrimInMS,
		Fit: c.Fit, Transform: c.Transform, Volume: c.Volume, FadeInMS: c.FadeInMS, FadeOutMS: c.FadeOutMS,
		MotionIn: c.MotionIn, MotionOut: c.MotionOut, Keyframes: c.Keyframes,
	}
	if c.Type == ClipOverlay {
		raster := rasters[c.ID]
		if raster == "" {
			return mediagen.Clip{}, ErrNotRasterized
		}
		clip.MediaID, clip.Fit, clip.TrimInMS = raster, mediagen.FitContain, 0
	}
	if c.Type == ClipImage {
		clip.TrimInMS = 0
	}
	return clip, nil
}

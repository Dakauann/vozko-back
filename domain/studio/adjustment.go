package studio

import "vozko/domain/mediagen"

const (
	MaxCameraPan      = 2.0
	MinCameraZoom     = 0.1
	MaxCameraZoom     = 10.0
	MaxCameraRotation = 3600.0
)

type ClipCamera struct {
	PanX     float64 `json:"panX"`
	PanY     float64 `json:"panY"`
	Zoom     float64 `json:"zoom"`
	Rotation float64 `json:"rotation"`
}

type CameraKeyframes struct {
	CameraPanX     []mediagen.Keyframe `json:"cameraPanX,omitempty"`
	CameraPanY     []mediagen.Keyframe `json:"cameraPanY,omitempty"`
	CameraZoom     []mediagen.Keyframe `json:"cameraZoom,omitempty"`
	CameraRotation []mediagen.Keyframe `json:"cameraRotation,omitempty"`
}

func (k *CameraKeyframes) count() int {
	return len(k.CameraPanX) + len(k.CameraPanY) + len(k.CameraZoom) + len(k.CameraRotation)
}

func (k *CameraKeyframes) issue() string {
	for _, frames := range [][]mediagen.Keyframe{k.CameraPanX, k.CameraPanY} {
		if code := keysIssue(frames, -MaxCameraPan, MaxCameraPan); code != "" {
			return code
		}
	}
	if code := keysIssue(k.CameraZoom, MinCameraZoom, MaxCameraZoom); code != "" {
		return code
	}
	return keysIssue(k.CameraRotation, -MaxCameraRotation, MaxCameraRotation)
}

func (c *ClipCamera) issue() string {
	if c == nil {
		return ""
	}
	if !within(c.PanX, -MaxCameraPan, MaxCameraPan) || !within(c.PanY, -MaxCameraPan, MaxCameraPan) || !within(c.Zoom, MinCameraZoom, MaxCameraZoom) || !within(c.Rotation, -MaxCameraRotation, MaxCameraRotation) {
		return CodeOutOfRange
	}
	return ""
}

func (c Clip) adjustmentIssue() string {
	if !within(c.Blur, 0, MaxClipBlur) {
		return CodeOutOfRange
	}
	if k := c.Keyframes; k != nil {
		if k.count() != k.GradeKeyframes.count()+len(k.Blur)+k.CameraKeyframes.count() {
			return CodeInvalid
		}
		if code := k.issue(c.Transform); code != "" {
			return code
		}
	}
	if code := c.Color.issue(); code != "" {
		return code
	}
	if c.GradientMap != nil {
		if code := c.GradientMap.issue(); code != "" {
			return code
		}
	}
	if code := c.Camera.issue(); code != "" {
		return code
	}
	if code := masksIssue(c.Masks); code != "" {
		return code
	}
	return vfxIssue(c.Vfx)
}

func (c Clip) cameraIssue() string {
	if c.Camera != nil || (c.Keyframes != nil && c.Keyframes.CameraKeyframes.count() > 0) {
		return CodeInvalid
	}
	return ""
}

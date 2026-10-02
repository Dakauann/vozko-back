package advertising

import (
	"errors"
	"time"
)

var ErrVideoNotReady = errors.New("meta is still processing the video")

type RemoteCatalog struct {
	ID          string
	Name        string
	ProductSets []RemoteProductSet
}

type RemoteProductSet struct {
	ID           string
	Name         string
	ProductCount int64
}

type RemoteApp struct {
	ID        string
	Name      string
	StoreURLs []string
	IconURL   string
}

type RemotePost struct {
	ID          string
	Platform    string
	Message     string
	PictureURL  string
	Permalink   string
	CreatedTime *time.Time
}

type RemoteInstantExperience struct {
	ID   string
	Name string
}

type VideoState string

const (
	VideoProcessing VideoState = "processing"
	VideoReady      VideoState = "ready"
	VideoError      VideoState = "error"
)

type RemoteVideo struct {
	ID           string
	State        VideoState
	ThumbnailURL string
}

type TargetingSearchKind string

const (
	SearchInterests TargetingSearchKind = "interests"
	SearchBehaviors TargetingSearchKind = "behaviors"
	SearchLanguages TargetingSearchKind = "languages"
)

type TargetingOption struct {
	ID          string
	Name        string
	Path        []string
	AudienceMin int64
	AudienceMax int64
}

type ReachEstimate struct {
	Lower int64
	Upper int64
	Ready bool
}

type AdAccountChange struct {
	AccountMetaID string
	Field         string
	ObjectIDs     []string
}

type LeadgenEvent struct {
	LeadgenID string
	FormID    string
	AdID      string
	PageID    string
	CreatedAt time.Time
}

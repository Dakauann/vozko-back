package facebook

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalidPost            = errors.New("facebook post request is invalid")
	ErrPublishKindUnavailable = errors.New("this facebook post kind is not available yet")
	ErrPostNotFound           = errors.New("facebook post not found")
	ErrPostNotEditable        = errors.New("only posts published through Vozko can be edited")
	ErrDeleteNotPermitted     = errors.New("facebook does not allow deleting this post through the app")
	ErrPublishJobNotFound     = errors.New("facebook publish job not found")
	ErrReelLimit              = errors.New("facebook allows 30 reels per page every 24 hours")
)

const (
	MaxPostMessageRunes  = 63206
	MaxPhotoBytes        = 10 << 20
	MinAlbumPhotos       = 2
	MaxAlbumPhotos       = 10
	MinScheduleLead      = 10 * time.Minute
	MaxScheduleLead      = 75 * 24 * time.Hour
	MaxVideoScheduleLead = 180 * 24 * time.Hour
	MaxVideoBytes        = 1 << 30
	MaxReelsPerDay       = 30
	ManagePostsURL       = "https://business.facebook.com/latest/posts"
)

type PublishKind string

const (
	PublishText  PublishKind = "text"
	PublishLink  PublishKind = "link"
	PublishPhoto PublishKind = "photo"
	PublishAlbum PublishKind = "album"
	PublishVideo PublishKind = "video"
	PublishReel  PublishKind = "reel"
	PublishStory PublishKind = "story"
)

var photoMIMETypes = map[string]struct{}{
	"image/jpeg": {}, "image/png": {}, "image/gif": {}, "image/bmp": {}, "image/tiff": {},
}

type MediaRef struct {
	URL       string `json:"url"`
	MIMEType  string `json:"mimeType"`
	SizeBytes int64  `json:"sizeBytes"`
}

type PublishRequest struct {
	Kind        PublishKind `json:"kind"`
	Message     string      `json:"message,omitempty"`
	Link        string      `json:"link,omitempty"`
	Media       []MediaRef  `json:"media,omitempty"`
	VideoTitle  string      `json:"videoTitle,omitempty"`
	ScheduledAt *time.Time  `json:"scheduledAt,omitempty"`
}

func (r PublishRequest) Scheduled() bool { return r.ScheduledAt != nil }

func (r PublishRequest) IsVideoStory() bool {
	return r.Kind == PublishStory && len(r.Media) == 1 && isVideo(r.Media[0])
}

func (r PublishRequest) Validate(now time.Time) error {
	if utf8.RuneCountInString(r.Message) > MaxPostMessageRunes {
		return invalidPost("the message is longer than %d characters", MaxPostMessageRunes)
	}
	if err := r.validateSchedule(now); err != nil {
		return err
	}
	if r.VideoTitle != "" && r.Kind != PublishVideo {
		return invalidPost("only a video takes a title")
	}
	switch r.Kind {
	case PublishText:
		if strings.TrimSpace(r.Message) == "" {
			return invalidPost("a text post needs a message")
		}
		return r.refuseMedia()
	case PublishLink:
		if err := secureURL(r.Link); err != nil {
			return invalidPost("the link %v", err)
		}
		return r.refuseMedia()
	case PublishPhoto:
		if len(r.Media) != 1 {
			return invalidPost("a photo post takes exactly one image")
		}
		return validatePhotos(r.Media)
	case PublishAlbum:
		if len(r.Media) < MinAlbumPhotos || len(r.Media) > MaxAlbumPhotos {
			return invalidPost("an album takes %d to %d images", MinAlbumPhotos, MaxAlbumPhotos)
		}
		return validatePhotos(r.Media)
	case PublishVideo, PublishReel:
		if len(r.Media) != 1 {
			return invalidPost("a %s takes exactly one video", r.Kind)
		}
		return validateVideo(r.Media[0])
	case PublishStory:
		if r.Scheduled() {
			return invalidPost("stories cannot be scheduled")
		}
		if strings.TrimSpace(r.Message) != "" {
			return invalidPost("a story carries no text")
		}
		if len(r.Media) != 1 {
			return invalidPost("a story takes exactly one image or video")
		}
		if isVideo(r.Media[0]) {
			return validateVideo(r.Media[0])
		}
		return validatePhotos(r.Media)
	}
	return invalidPost("unknown kind %q", r.Kind)
}

func (r PublishRequest) validateSchedule(now time.Time) error {
	if r.ScheduledAt == nil {
		return nil
	}
	maxLead := MaxScheduleLead
	if r.Kind == PublishVideo {
		maxLead = MaxVideoScheduleLead
	}
	lead := r.ScheduledAt.Sub(now)
	if lead < MinScheduleLead || lead > maxLead {
		return invalidPost("a post can be scheduled from %s to %s ahead", MinScheduleLead, maxLead)
	}
	return nil
}

func isVideo(m MediaRef) bool {
	return strings.HasPrefix(strings.ToLower(m.MIMEType), "video/")
}

func validateVideo(m MediaRef) error {
	if err := secureURL(m.URL); err != nil {
		return invalidPost("the video %v", err)
	}
	if strings.ToLower(m.MIMEType) != "video/mp4" {
		return invalidPost("videos must be mp4, got %q", m.MIMEType)
	}
	if m.SizeBytes > MaxVideoBytes {
		return invalidPost("the video is larger than %d bytes", MaxVideoBytes)
	}
	return nil
}

func (r PublishRequest) refuseMedia() error {
	if len(r.Media) > 0 {
		return invalidPost("a %s post carries no media", r.Kind)
	}
	return nil
}

func validatePhotos(media []MediaRef) error {
	for i, m := range media {
		if err := secureURL(m.URL); err != nil {
			return invalidPost("image %d %v", i+1, err)
		}
		if _, ok := photoMIMETypes[strings.ToLower(m.MIMEType)]; !ok {
			return invalidPost("image %d has unsupported type %q", i+1, m.MIMEType)
		}
		if m.SizeBytes > MaxPhotoBytes {
			return invalidPost("image %d is larger than %d bytes", i+1, MaxPhotoBytes)
		}
	}
	return nil
}

func secureURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("is not a valid URL")
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("must use https")
	}
	return nil
}

func invalidPost(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidPost, fmt.Sprintf(format, args...))
}

type JobStatus string

const (
	JobQueued     JobStatus = "QUEUED"
	JobUploading  JobStatus = "UPLOADING"
	JobProcessing JobStatus = "PROCESSING"
	JobPublished  JobStatus = "PUBLISHED"
	JobScheduled  JobStatus = "SCHEDULED"
	JobFailed     JobStatus = "FAILED"
)

func (s JobStatus) Terminal() bool {
	return s == JobPublished || s == JobScheduled || s == JobFailed
}

func (s JobStatus) CanTransitionTo(next JobStatus) bool {
	if s.Terminal() {
		return false
	}
	switch s {
	case JobQueued:
		return next == JobUploading || next == JobFailed
	case JobUploading:
		return next == JobQueued || next == JobUploading || next == JobProcessing || next.Terminal()
	case JobProcessing:
		return next.Terminal()
	}
	return false
}

const (
	PhaseCreating    = "creating"
	MaxPublishTries  = 5
	PublishJobsTopic = "job.facebook.publish"
	PublishExchange  = "facebook_publish_exchange"
)

type JobProgress struct {
	Phase     string   `json:"phase,omitempty"`
	PhotoIDs  []string `json:"photoIds,omitempty"`
	VideoID   string   `json:"videoId,omitempty"`
	UploadURL string   `json:"uploadUrl,omitempty"`
	Checks    int      `json:"checks,omitempty"`
}

type PublishJob struct {
	ID           string
	WorkspaceID  string
	PageID       string
	RequestedBy  string
	Request      PublishRequest
	Progress     JobProgress
	Status       JobStatus
	FBObjectID   string
	FBPostID     string
	ErrorCode    int
	ErrorSubcode int
	ErrorMessage string
	Ambiguous    bool
	Attempts     int
	NextCheckAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (j *PublishJob) Fail(code, subcode int, message string, ambiguous bool) {
	j.Status, j.ErrorCode, j.ErrorSubcode, j.Ambiguous = JobFailed, code, subcode, ambiguous
	if len(message) > 500 {
		message = message[:500]
	}
	j.ErrorMessage = message
}

type PostKind string

const (
	PostStatus  PostKind = "status"
	PostLink    PostKind = "link"
	PostPhoto   PostKind = "photo"
	PostAlbum   PostKind = "album"
	PostVideo   PostKind = "video"
	PostReel    PostKind = "reel"
	PostStory   PostKind = "story"
	PostShared  PostKind = "shared"
	PostVisitor PostKind = "visitor"
)

type RemoteAttachment struct {
	Type        string
	MediaType   string
	URL         string
	Title       string
	Description string
	ImageURL    string
}

type RemotePost struct {
	FBPostID             string
	FromID               string
	Message              string
	Story                string
	StatusType           string
	PermalinkURL         string
	FullPicture          string
	CreatedTime          *time.Time
	UpdatedTime          *time.Time
	IsPublished          bool
	IsHidden             bool
	ScheduledPublishTime *time.Time
	ReactionsCount       int
	CommentsCount        int
	SharesCount          int
	Attachments          []RemoteAttachment
}

func (p *RemotePost) KindFor(fbPageID string) PostKind {
	if p.FromID != "" && p.FromID != fbPageID {
		return PostVisitor
	}
	if len(p.Attachments) == 0 {
		return PostStatus
	}
	switch a := p.Attachments[0]; {
	case a.Type == "reel":
		return PostReel
	case a.Type == "album":
		return PostAlbum
	case a.MediaType == "video" || strings.HasPrefix(a.Type, "video"):
		return PostVideo
	case a.MediaType == "photo" || a.Type == "photo":
		return PostPhoto
	case a.Type == "share" || a.MediaType == "link":
		return PostLink
	}
	return PostShared
}

func (p *RemotePost) AssetURL(thumb bool) string {
	if len(p.Attachments) > 0 && p.Attachments[0].ImageURL != "" && thumb {
		return p.Attachments[0].ImageURL
	}
	if p.FullPicture != "" {
		return p.FullPicture
	}
	if len(p.Attachments) > 0 {
		return p.Attachments[0].ImageURL
	}
	return ""
}

type Post struct {
	ID                   string
	WorkspaceID          string
	PageID               string
	FBPostID             string
	Kind                 PostKind
	StatusType           string
	Message              string
	PermalinkURL         string
	IsPublished          bool
	ScheduledPublishTime *time.Time
	IsHidden             bool
	CreatedByApp         bool
	ReactionsCount       int
	CommentsCount        int
	SharesCount          int
	CreatedTime          *time.Time
	UpdatedTime          *time.Time
}

func (p *Post) Editable() bool { return p != nil && p.CreatedByApp }

func (p *Page) OwnsPostID(fbPostID string) bool {
	return p != nil && p.FBPageID != "" && strings.HasPrefix(fbPostID, p.FBPageID+"_")
}

func PostFromRemote(page *Page, remote *RemotePost) *Post {
	return &Post{
		WorkspaceID:          page.WorkspaceID,
		PageID:               page.ID,
		FBPostID:             remote.FBPostID,
		Kind:                 remote.KindFor(page.FBPageID),
		StatusType:           remote.StatusType,
		Message:              remote.Message,
		PermalinkURL:         remote.PermalinkURL,
		IsPublished:          remote.IsPublished,
		ScheduledPublishTime: remote.ScheduledPublishTime,
		IsHidden:             remote.IsHidden,
		ReactionsCount:       remote.ReactionsCount,
		CommentsCount:        remote.CommentsCount,
		SharesCount:          remote.SharesCount,
		CreatedTime:          remote.CreatedTime,
		UpdatedTime:          remote.UpdatedTime,
	}
}

type PostListKind string

const (
	ListPublished PostListKind = "published"
	ListScheduled PostListKind = "scheduled"
	ListReels     PostListKind = "reels"
)

type VideoTarget string

const (
	VideoForReel  VideoTarget = "video_reels"
	VideoForStory VideoTarget = "video_stories"
)

type VideoSession struct {
	VideoID   string
	UploadURL string
}

type VideoInput struct {
	FileURL     string
	Title       string
	Description string
	ScheduledAt *time.Time
}

type ReelFinish struct {
	Description string
	ScheduledAt *time.Time
}

type VideoStatus struct {
	State  string
	PostID string
}

func (s VideoStatus) Ready() bool  { return s.State == "ready" }
func (s VideoStatus) Failed() bool { return s.State == "error" || s.State == "expired" }

type RemoteStory struct {
	PostID      string
	Status      string
	MediaType   string
	MediaID     string
	URL         string
	CreatedTime *time.Time
}

const (
	PhaseVideoStarted = "video_started"
	PhaseTransferred  = "transferred"
	PhaseProcessing   = "processing"
	CodeReelCap       = 613
)

type FeedPostInput struct {
	Message       string
	Link          string
	AttachedMedia []string
	ScheduledAt   *time.Time
}

type PhotoInput struct {
	URL       string
	Caption   string
	Published bool
	Temporary bool
}

type PhotoResult struct {
	PhotoID string
	PostID  string
}

type PostUpdate struct {
	Message     *string
	IsHidden    *bool
	PublishNow  bool
	ScheduledAt *time.Time
}

func (u PostUpdate) ChangesContent() bool {
	return u.Message != nil || u.PublishNow || u.ScheduledAt != nil
}

func (u PostUpdate) Empty() bool {
	return !u.ChangesContent() && u.IsHidden == nil
}

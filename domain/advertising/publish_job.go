package advertising

import (
	"fmt"
	"strconv"
	"time"
)

type JobStatus string

const (
	JobQueued      JobStatus = "QUEUED"
	JobRunning     JobStatus = "RUNNING"
	JobPublished   JobStatus = "PUBLISHED"
	JobFailed      JobStatus = "FAILED"
	JobNeedsReview JobStatus = "NEEDS_REVIEW"
)

func (s JobStatus) Terminal() bool {
	return s == JobPublished || s == JobFailed || s == JobNeedsReview
}

type StepKind string

const (
	StepMedia      StepKind = "media"
	StepVideoReady StepKind = "video_ready"
	StepCampaign   StepKind = "campaign"
	StepAdSet      StepKind = "adset"
	StepCreative   StepKind = "creative"
	StepAd         StepKind = "ad"
	StepActivate   StepKind = "activate"
)

type Step struct {
	Kind  StepKind `json:"kind"`
	Index int      `json:"index,omitempty"`
	Media MediaRef `json:"media,omitempty"`
}

func (s Step) Key() string {
	switch s.Kind {
	case StepMedia, StepVideoReady:
		return string(s.Kind) + ":" + s.Media.MediaID
	case StepCreative, StepAd:
		return string(s.Kind) + ":" + strconv.Itoa(s.Index)
	}
	return string(s.Kind)
}

func (s Step) RetrySafe() bool {
	switch s.Kind {
	case StepMedia, StepVideoReady, StepActivate:
		return true
	}
	return false
}

type Actor string

const (
	ActorPerson    Actor = "person"
	ActorAssistant Actor = "assistant"
)

type FeeState string

const (
	FeeNone     FeeState = ""
	FeeCharged  FeeState = "charged"
	FeeRefunded FeeState = "refunded"
)

type Progress struct {
	Media      map[string]string `json:"media,omitempty"`
	ReadyVideo map[string]bool   `json:"readyVideo,omitempty"`
	CampaignID string            `json:"campaignId,omitempty"`
	AdSetID    string            `json:"adSetId,omitempty"`
	Creatives  map[int]string    `json:"creatives,omitempty"`
	Ads        map[int]string    `json:"ads,omitempty"`
	Activated  bool              `json:"activated,omitempty"`
	InFlight   string            `json:"inFlight,omitempty"`
}

type PublishJob struct {
	ID           string
	WorkspaceID  string
	AdAccountID  string
	CreatedBy    string
	Actor        Actor
	Draft        AdDraft
	Status       JobStatus
	Progress     Progress
	Fee          FeeState
	FeeMicros    int64
	ErrorCode    string
	ErrorMessage string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (j *PublishJob) FeeReference() string { return "ad_publish:" + j.ID }

func (j *PublishJob) AdsToPublish() int { return j.Draft.AdsToPublish() }

func (j *PublishJob) Steps() []Step {
	var steps []Step
	seen := map[string]bool{}
	for _, ad := range j.Draft.Ads {
		for _, m := range ad.Creative.MediaRefs() {
			if seen[m.MediaID] {
				continue
			}
			seen[m.MediaID] = true
			steps = append(steps, Step{Kind: StepMedia, Media: m})
			if m.Kind == MediaVideo {
				steps = append(steps, Step{Kind: StepVideoReady, Media: m})
			}
		}
	}
	if j.Draft.NewCampaign() {
		steps = append(steps, Step{Kind: StepCampaign})
	}
	if j.Draft.NewAdSet() {
		steps = append(steps, Step{Kind: StepAdSet})
	}
	for i := range j.Draft.Ads {
		steps = append(steps, Step{Kind: StepCreative, Index: i}, Step{Kind: StepAd, Index: i})
	}
	if !j.Draft.KeepPaused {
		steps = append(steps, Step{Kind: StepActivate})
	}
	return steps
}

func (j *PublishJob) done(s Step) bool {
	p := j.Progress
	switch s.Kind {
	case StepMedia:
		return p.Media[s.Media.MediaID] != ""
	case StepVideoReady:
		return p.ReadyVideo[s.Media.MediaID]
	case StepCampaign:
		return p.CampaignID != ""
	case StepAdSet:
		return p.AdSetID != ""
	case StepCreative:
		return p.Creatives[s.Index] != ""
	case StepAd:
		return p.Ads[s.Index] != ""
	case StepActivate:
		return p.Activated
	}
	return false
}

func (j *PublishJob) NextStep() (Step, bool) {
	for _, s := range j.Steps() {
		if !j.done(s) {
			return s, true
		}
	}
	return Step{}, false
}

func (j *PublishJob) Interrupted() bool {
	if j.Progress.InFlight == "" {
		return false
	}
	for _, s := range j.Steps() {
		if s.Key() == j.Progress.InFlight {
			return !s.RetrySafe()
		}
	}
	return true
}

func (j *PublishJob) Begin(s Step) { j.Progress.InFlight = s.Key() }

func (j *PublishJob) Release() { j.Progress.InFlight = "" }

func (j *PublishJob) Complete(s Step, metaID string) error {
	if j.Progress.InFlight != s.Key() {
		return fmt.Errorf("advertising: step %s completed while %q was in flight", s.Key(), j.Progress.InFlight)
	}
	if metaID == "" && s.Kind != StepActivate && s.Kind != StepVideoReady {
		return fmt.Errorf("advertising: meta returned no id for %s", s.Key())
	}
	p := &j.Progress
	switch s.Kind {
	case StepMedia:
		p.Media = withEntry(p.Media, s.Media.MediaID, metaID)
	case StepVideoReady:
		if p.ReadyVideo == nil {
			p.ReadyVideo = map[string]bool{}
		}
		p.ReadyVideo[s.Media.MediaID] = true
	case StepCampaign:
		p.CampaignID = metaID
	case StepAdSet:
		p.AdSetID = metaID
	case StepCreative:
		p.Creatives = withIndex(p.Creatives, s.Index, metaID)
	case StepAd:
		p.Ads = withIndex(p.Ads, s.Index, metaID)
	case StepActivate:
		p.Activated = true
	}
	p.InFlight = ""
	return nil
}

func withEntry(m map[string]string, k, v string) map[string]string {
	if m == nil {
		m = map[string]string{}
	}
	m[k] = v
	return m
}

func withIndex(m map[int]string, k int, v string) map[int]string {
	if m == nil {
		m = map[int]string{}
	}
	m[k] = v
	return m
}

func (j *PublishJob) CampaignID() string {
	if j.Draft.NewCampaign() {
		return j.Progress.CampaignID
	}
	return j.Draft.Campaign.ExistingID
}

func (j *PublishJob) AdSetID() string {
	if j.Draft.NewAdSet() {
		return j.Progress.AdSetID
	}
	return j.Draft.AdSet.ExistingID
}

func (j *PublishJob) UploadedMedia() UploadedMedia {
	out := UploadedMedia{ImageHashes: map[string]string{}, VideoIDs: map[string]string{}}
	for _, ad := range j.Draft.Ads {
		for _, m := range ad.Creative.MediaRefs() {
			id := j.Progress.Media[m.MediaID]
			if id == "" {
				continue
			}
			if m.Kind == MediaVideo {
				out.VideoIDs[m.MediaID] = id
			} else {
				out.ImageHashes[m.MediaID] = id
			}
		}
	}
	return out
}

func (j *PublishJob) ToActivate() []string {
	var ids []string
	if j.Draft.NewCampaign() && j.Progress.CampaignID != "" {
		ids = append(ids, j.Progress.CampaignID)
	}
	if j.Draft.NewAdSet() && j.Progress.AdSetID != "" {
		ids = append(ids, j.Progress.AdSetID)
	}
	for i := range j.Draft.Ads {
		if id := j.Progress.Ads[i]; id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func (j *PublishJob) Publish() {
	j.Status = JobPublished
	j.ErrorCode, j.ErrorMessage = "", ""
}

func (j *PublishJob) Fail(code, message string) {
	j.Status = JobFailed
	j.ErrorCode, j.ErrorMessage = code, message
}

func (j *PublishJob) Review(code, message string) {
	j.Status = JobNeedsReview
	j.ErrorCode, j.ErrorMessage = code, message
}

func (j *PublishJob) CanSwitchOnLater() error {
	if j.Status != JobPublished || !j.Draft.KeepPaused || j.Progress.Activated {
		return ErrJobNotActivatable
	}
	return nil
}

func (j *PublishJob) SwitchedOn() {
	j.Progress.Activated = true
}

func (j *PublishJob) RefundDue() bool {
	return j.Status == JobFailed && j.Fee == FeeCharged && !j.Progress.Activated
}

func (j *PublishJob) CreatedObjects() []string {
	var ids []string
	for i := len(j.Draft.Ads) - 1; i >= 0; i-- {
		if id := j.Progress.Ads[i]; id != "" {
			ids = append(ids, id)
		}
	}
	if j.Draft.NewAdSet() && j.Progress.AdSetID != "" {
		ids = append(ids, j.Progress.AdSetID)
	}
	if j.Draft.NewCampaign() && j.Progress.CampaignID != "" {
		ids = append(ids, j.Progress.CampaignID)
	}
	return ids
}

const (
	PublishJobsTopic = "job.ads.publish"
	PublishExchange  = "ads_publish_exchange"
)

type PublishJobMessage struct {
	WorkspaceID string `json:"workspaceId"`
	JobID       string `json:"jobId"`
}

type PublishQueue interface {
	Enqueue(workspaceID, jobID string) error
}

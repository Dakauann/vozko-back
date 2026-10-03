package advertising

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

var (
	ErrDraftNotFound   = errors.New("ad draft not found")
	ErrDraftChanged    = errors.New("the ad draft was changed by someone else")
	ErrDraftPublishing = errors.New("the ad draft is being published")
)

type DraftState string

const (
	DraftEditing    DraftState = "editing"
	DraftPublishing DraftState = "publishing"
	DraftFailed     DraftState = "failed"
	DraftPublished  DraftState = "published"
)

type SavedDraft struct {
	ID          string
	WorkspaceID string
	AdAccountID string
	CreatedBy   string
	UpdatedBy   string
	Content     AdDraft
	JobID       string
	Version     int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func NewSavedDraft(workspaceID, accountID, userID string, content AdDraft) (*SavedDraft, error) {
	d := &SavedDraft{WorkspaceID: workspaceID, CreatedBy: userID}
	if err := d.Replace(accountID, userID, content); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *SavedDraft) Replace(accountID, userID string, content AdDraft) error {
	content.AdAccountID = accountID
	content.Normalize()
	if err := content.checkBounds(); err != nil {
		return err
	}
	d.AdAccountID, d.UpdatedBy, d.Content = accountID, userID, content
	return nil
}

func (d *SavedDraft) Copy(userID, name string) (*SavedDraft, error) {
	content := d.Content
	content.Ads = append([]AdItem(nil), d.Content.Ads...)
	content.rename(name)
	return NewSavedDraft(d.WorkspaceID, d.AdAccountID, userID, content)
}

func (d *AdDraft) rename(name string) {
	if strings.TrimSpace(name) == "" {
		return
	}
	switch {
	case d.Campaign.ExistingID == "" && d.AdSet.ExistingID == "":
		d.Campaign.Name = name
	case d.AdSet.ExistingID == "":
		d.AdSet.Name = name
	case len(d.Ads) == 1:
		d.Ads[0].Name = name
	}
}

func (d *AdDraft) checkBounds() error {
	v := newIssues()
	v.text("campaign.name", d.Campaign.Name, false, maxNameRunes)
	v.text("adSet.name", d.AdSet.Name, false, maxNameRunes)
	if len(d.Ads) > maxAdsPerDraft {
		v.add("ads", "too_many")
	}
	for i, ad := range d.Ads {
		v.item("ads", i).text("name", ad.Name, false, maxNameRunes)
	}
	return v.err()
}

const unconfirmedClaimTimeout = 15 * time.Minute

func (d *SavedDraft) State(job *PublishJob, now time.Time) DraftState {
	if d.JobID == "" {
		return DraftEditing
	}
	if job == nil {
		if now.Sub(d.UpdatedAt) < unconfirmedClaimTimeout {
			return DraftPublishing
		}
		return DraftEditing
	}
	switch job.Status {
	case JobPublished:
		return DraftPublished
	case JobQueued, JobRunning:
		return DraftPublishing
	}
	return DraftFailed
}

func (d *SavedDraft) Editable(job *PublishJob, now time.Time) error {
	if state := d.State(job, now); state == DraftPublishing || state == DraftPublished {
		return ErrDraftPublishing
	}
	return nil
}

type DraftRow struct {
	Key          string
	Level        Level
	Name         string
	ParentKey    string
	ParentMetaID string
	Budget       *Budget
	Objective    Objective
	Destination  Destination
	Goal         OptimizationGoal
}

func (d *SavedDraft) Rows() []DraftRow {
	c, s := d.Content.Campaign, d.Content.AdSet
	rows := make([]DraftRow, 0, len(d.Content.Ads)+2)
	campaign := DraftRow{ParentMetaID: c.ExistingID}
	if c.ExistingID == "" && s.ExistingID == "" {
		campaign = DraftRow{Key: d.ID + ":campaign"}
		rows = append(rows, DraftRow{Key: campaign.Key, Level: LevelCampaign, Name: c.Name, Budget: c.Budget, Objective: c.Objective})
	}
	adSet := DraftRow{ParentMetaID: s.ExistingID}
	if s.ExistingID == "" {
		adSet = DraftRow{Key: d.ID + ":adset"}
		rows = append(rows, DraftRow{
			Key: adSet.Key, Level: LevelAdSet, Name: s.Name, ParentKey: campaign.Key, ParentMetaID: campaign.ParentMetaID,
			Budget: s.Budget, Objective: c.Objective, Destination: s.Destination, Goal: s.Goal,
		})
	}
	for i, ad := range d.Content.Ads {
		rows = append(rows, DraftRow{
			Key: d.ID + ":ad:" + strconv.Itoa(i), Level: LevelAd, Name: ad.Name, ParentKey: adSet.Key, ParentMetaID: adSet.ParentMetaID,
			Objective: c.Objective, Destination: s.Destination, Goal: s.Goal,
		})
	}
	return rows
}

type SavedDraftRepository interface {
	Create(ctx context.Context, d *SavedDraft) error
	Find(ctx context.Context, workspaceID, id string) (*SavedDraft, error)
	Save(ctx context.Context, d *SavedDraft) error
	ClaimForPublish(ctx context.Context, d *SavedDraft, jobID string) error
	ReleaseJob(ctx context.Context, workspaceID, id, jobID string) error
	Delete(ctx context.Context, d *SavedDraft) error
	ListByAccount(ctx context.Context, workspaceID, accountID string) ([]*SavedDraft, error)
}

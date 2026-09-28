package facebook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	fbdomain "vozko/domain/facebook"
	"vozko/domain/messaging"
)

const (
	defaultPostPageSize = 25
	maxPostPageSize     = 100
	jobListLimit        = 50
)

type PublishQueue interface {
	Enqueue(jobID string) error
}

type PublishJobMessage struct {
	JobID string `json:"jobId"`
}

type queuePublisher struct {
	pub messaging.MessageQueuePub
}

func NewPublishQueue(pub messaging.MessageQueuePub) PublishQueue {
	return queuePublisher{pub: pub}
}

func (q queuePublisher) Enqueue(jobID string) error {
	payload, err := json.Marshal(PublishJobMessage{JobID: jobID})
	if err != nil {
		return err
	}
	return q.pub.Publish(fbdomain.PublishJobsTopic, payload)
}

type PostDeps struct {
	Pages   fbdomain.PageRepository
	Posts   fbdomain.PostRepository
	Jobs    fbdomain.PublishJobRepository
	Service fbdomain.PostService
	Queue   PublishQueue
}

type PostUseCases struct {
	d   PostDeps
	now func() time.Time
}

func NewPostUseCases(d PostDeps) *PostUseCases {
	return &PostUseCases{d: d, now: func() time.Time { return time.Now().UTC() }}
}

type PostView struct {
	Remote   *fbdomain.RemotePost
	Kind     fbdomain.PostKind
	Editable bool
}

func (uc *PostUseCases) List(ctx context.Context, workspaceID, pageID string, kind fbdomain.PostListKind, limit int, after string) (*fbdomain.Paged[*PostView], error) {
	page, err := pageWith(ctx, uc.d.Pages, workspaceID, pageID, fbdomain.CapReadPosts)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > maxPostPageSize {
		limit = defaultPostPageSize
	}
	out, err := uc.d.Service.List(ctx, page.FBPageID, page.PageToken, kind, limit, after)
	if err != nil {
		return nil, err
	}
	mirror := make([]*fbdomain.Post, 0, len(out.Items))
	ids := make([]string, 0, len(out.Items))
	for _, remote := range out.Items {
		mirror = append(mirror, fbdomain.PostFromRemote(page, remote))
		ids = append(ids, remote.FBPostID)
	}
	if err := uc.d.Posts.UpsertMany(ctx, mirror); err != nil {
		log.Printf("[facebook] post mirror failed page=%s: %v", page.FBPageID, err)
	}
	ours, err := uc.d.Posts.AppMadeAmong(ctx, ids)
	if err != nil {
		return nil, err
	}
	views := &fbdomain.Paged[*PostView]{Items: make([]*PostView, 0, len(out.Items)), NextCursor: out.NextCursor, HasNext: out.HasNext}
	for _, remote := range out.Items {
		views.Items = append(views.Items, &PostView{Remote: remote, Kind: remote.KindFor(page.FBPageID), Editable: ours[remote.FBPostID]})
	}
	return views, nil
}

func (uc *PostUseCases) Get(ctx context.Context, workspaceID, pageID, fbPostID string) (*PostView, error) {
	page, remote, err := uc.remotePost(ctx, workspaceID, pageID, fbPostID)
	if err != nil {
		return nil, err
	}
	post := fbdomain.PostFromRemote(page, remote)
	stored, err := uc.d.Posts.FindByFBPostID(ctx, fbPostID)
	switch {
	case err == nil:
		post.CreatedByApp = stored.CreatedByApp
	case !errors.Is(err, fbdomain.ErrPostNotFound):
		return nil, err
	}
	if err := uc.d.Posts.UpsertMany(ctx, []*fbdomain.Post{post}); err != nil {
		log.Printf("[facebook] post mirror failed post=%s: %v", fbPostID, err)
	}
	return &PostView{Remote: remote, Kind: post.Kind, Editable: post.Editable()}, nil
}

func (uc *PostUseCases) Asset(ctx context.Context, workspaceID, pageID, fbPostID string, thumb bool) ([]byte, string, error) {
	_, remote, err := uc.remotePost(ctx, workspaceID, pageID, fbPostID)
	if err != nil {
		return nil, "", err
	}
	url := remote.AssetURL(thumb)
	if url == "" {
		return nil, "", fmt.Errorf("%w: post %s has no image", fbdomain.ErrPostNotFound, fbPostID)
	}
	return uc.d.Service.FetchBytes(ctx, url)
}

func (uc *PostUseCases) Create(ctx context.Context, workspaceID, pageID, userID string, req fbdomain.PublishRequest) (*fbdomain.PublishJob, error) {
	page, err := pageWith(ctx, uc.d.Pages, workspaceID, pageID, fbdomain.CapPublish)
	if err != nil {
		return nil, err
	}
	if err := req.Validate(uc.now()); err != nil {
		return nil, err
	}
	if err := uc.withinReelCap(ctx, page, req); err != nil {
		return nil, err
	}
	job := &fbdomain.PublishJob{
		WorkspaceID: page.WorkspaceID, PageID: page.ID, RequestedBy: userID,
		Request: req, Status: fbdomain.JobQueued,
	}
	if err := uc.d.Jobs.Create(ctx, job); err != nil {
		return nil, err
	}
	if err := uc.d.Queue.Enqueue(job.ID); err != nil {
		job.Fail(0, 0, "the publish job could not be queued", false)
		if saveErr := uc.d.Jobs.Save(ctx, job); saveErr != nil {
			log.Printf("[facebook] unqueued job %s could not be marked failed: %v", job.ID, saveErr)
		}
		return nil, fmt.Errorf("facebook: queue publish job: %w", err)
	}
	return job, nil
}

func (uc *PostUseCases) Update(ctx context.Context, workspaceID, pageID, fbPostID string, update fbdomain.PostUpdate) error {
	if update.Empty() {
		return fmt.Errorf("%w: nothing to change", fbdomain.ErrInvalidPost)
	}
	page, err := uc.ownedPost(ctx, workspaceID, pageID, fbPostID)
	if err != nil {
		return err
	}
	if update.ChangesContent() {
		stored, err := uc.d.Posts.FindByFBPostID(ctx, fbPostID)
		if err != nil && !errors.Is(err, fbdomain.ErrPostNotFound) {
			return err
		}
		if !stored.Editable() {
			return fbdomain.ErrPostNotEditable
		}
	}
	if err := uc.d.Service.Update(ctx, page.PageToken, fbPostID, update); err != nil {
		return err
	}
	uc.mirrorUpdate(ctx, fbPostID, update)
	return nil
}

func (uc *PostUseCases) mirrorUpdate(ctx context.Context, fbPostID string, update fbdomain.PostUpdate) {
	if update.IsHidden != nil {
		if err := uc.d.Posts.SetHidden(ctx, fbPostID, *update.IsHidden); err != nil {
			log.Printf("[facebook] post %s hidden flag not mirrored: %v", fbPostID, err)
		}
	}
	if update.Message != nil {
		if err := uc.d.Posts.UpdateMessage(ctx, fbPostID, *update.Message); err != nil {
			log.Printf("[facebook] post %s message not mirrored: %v", fbPostID, err)
		}
	}
}

func (uc *PostUseCases) Delete(ctx context.Context, workspaceID, pageID, fbPostID string) error {
	page, err := uc.ownedPost(ctx, workspaceID, pageID, fbPostID)
	if err != nil {
		return err
	}
	if err := uc.d.Service.Delete(ctx, page.PageToken, fbPostID); err != nil {
		if fbdomain.HasCode(err, fbdomain.CodePermissionDenied) || fbdomain.Classify(err) == fbdomain.FailureAccessLevel {
			return errors.Join(fbdomain.ErrDeleteNotPermitted, err)
		}
		return err
	}
	if err := uc.d.Posts.Remove(ctx, fbPostID); err != nil {
		log.Printf("[facebook] deleted post %s not removed from the mirror: %v", fbPostID, err)
	}
	return nil
}

func (uc *PostUseCases) Job(ctx context.Context, workspaceID, pageID, jobID string) (*fbdomain.PublishJob, error) {
	page, err := pageWith(ctx, uc.d.Pages, workspaceID, pageID, "")
	if err != nil {
		return nil, err
	}
	job, err := uc.d.Jobs.FindByID(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if job.PageID != page.ID {
		return nil, fbdomain.ErrPublishJobNotFound
	}
	return job, nil
}

func (uc *PostUseCases) Jobs(ctx context.Context, workspaceID, pageID string, status fbdomain.JobStatus) ([]*fbdomain.PublishJob, error) {
	page, err := pageWith(ctx, uc.d.Pages, workspaceID, pageID, "")
	if err != nil {
		return nil, err
	}
	return uc.d.Jobs.ListByPage(ctx, page.ID, status, jobListLimit)
}

func (uc *PostUseCases) remotePost(ctx context.Context, workspaceID, pageID, fbPostID string) (*fbdomain.Page, *fbdomain.RemotePost, error) {
	page, err := pageWith(ctx, uc.d.Pages, workspaceID, pageID, fbdomain.CapReadPosts)
	if err != nil {
		return nil, nil, err
	}
	if !page.OwnsPostID(fbPostID) {
		return nil, nil, fbdomain.ErrPostNotFound
	}
	remote, err := uc.d.Service.Get(ctx, page.PageToken, fbPostID)
	if err != nil {
		return nil, nil, err
	}
	return page, remote, nil
}

func (uc *PostUseCases) ownedPost(ctx context.Context, workspaceID, pageID, fbPostID string) (*fbdomain.Page, error) {
	page, err := pageWith(ctx, uc.d.Pages, workspaceID, pageID, fbdomain.CapPublish)
	if err != nil {
		return nil, err
	}
	if !page.OwnsPostID(fbPostID) {
		return nil, fbdomain.ErrPostNotFound
	}
	return page, nil
}

func pageWith(ctx context.Context, pages fbdomain.PageRepository, workspaceID, pageID string, capability fbdomain.Capability) (*fbdomain.Page, error) {
	page, err := pages.FindByID(ctx, pageID)
	if err != nil {
		return nil, err
	}
	if page.WorkspaceID != workspaceID {
		return nil, fbdomain.ErrPageNotFound
	}
	if capability != "" && !page.Can(capability) {
		return nil, fmt.Errorf("%w: page %s cannot %s", fbdomain.ErrCapabilityDenied, page.Name, capability)
	}
	return page, nil
}

func (uc *PostUseCases) withinReelCap(ctx context.Context, page *fbdomain.Page, req fbdomain.PublishRequest) error {
	if req.Kind != fbdomain.PublishReel {
		return nil
	}
	recent, err := uc.d.Jobs.CountSince(ctx, page.ID, fbdomain.PublishReel, uc.now().Add(-24*time.Hour))
	if err != nil {
		return err
	}
	if recent >= fbdomain.MaxReelsPerDay {
		return fbdomain.ErrReelLimit
	}
	return nil
}

func (uc *PostUseCases) Stories(ctx context.Context, workspaceID, pageID string) ([]*fbdomain.RemoteStory, error) {
	page, err := pageWith(ctx, uc.d.Pages, workspaceID, pageID, fbdomain.CapReadPosts)
	if err != nil {
		return nil, err
	}
	return uc.d.Service.ListStories(ctx, page.FBPageID, page.PageToken)
}

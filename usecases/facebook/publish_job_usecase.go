package facebook

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"vozko/domain/cache"
	fbdomain "vozko/domain/facebook"
	"vozko/domain/messaging"
	webhook_usecase "vozko/usecases/webhook"
)

const (
	staleJobAge   = 15 * time.Minute
	reapBatchSize = 100
)

var errPublishRetry = errors.New("facebook: publish step throttled, retrying")

type PublishJobWorker struct {
	d   PostDeps
	now func() time.Time
}

func NewPublishJobWorker(d PostDeps) *PublishJobWorker {
	return &PublishJobWorker{d: d, now: func() time.Time { return time.Now().UTC() }}
}

func (w *PublishJobWorker) Execute(ctx context.Context, msg *PublishJobMessage) error {
	claimed, err := w.d.Jobs.Claim(ctx, msg.JobID, fbdomain.JobQueued, fbdomain.JobUploading)
	if err != nil || !claimed {
		return err
	}
	job, err := w.d.Jobs.FindByID(ctx, msg.JobID)
	if err != nil {
		return err
	}
	if job.Attempts > fbdomain.MaxPublishTries {
		job.Fail(0, 0, "publishing kept failing; giving up", false)
		return w.d.Jobs.Save(ctx, job)
	}
	if job.Progress.Phase == fbdomain.PhaseCreating {
		job.Fail(0, 0, "an earlier attempt stopped while Facebook was creating the post; check the Page before retrying", true)
		return w.d.Jobs.Save(ctx, job)
	}
	page, err := w.d.Pages.FindByID(ctx, job.PageID)
	if err != nil {
		return err
	}
	if !page.Can(fbdomain.CapPublish) {
		job.Fail(0, 0, "the page no longer grants publishing", false)
		return w.d.Jobs.Save(ctx, job)
	}

	if err := w.publish(ctx, page, job); err != nil {
		return w.settle(ctx, job, err)
	}
	if job.Status == fbdomain.JobProcessing {
		return nil
	}
	return w.complete(ctx, page, job)
}

func (w *PublishJobWorker) publish(ctx context.Context, page *fbdomain.Page, job *fbdomain.PublishJob) error {
	req := job.Request
	switch req.Kind {
	case fbdomain.PublishText, fbdomain.PublishLink:
		return w.createFeedPost(ctx, page, job, nil)
	case fbdomain.PublishPhoto:
		if !req.Scheduled() {
			return w.createPhotoPost(ctx, page, job)
		}
		fallthrough
	case fbdomain.PublishAlbum:
		if err := w.uploadPhotos(ctx, page, job); err != nil {
			return err
		}
		return w.createFeedPost(ctx, page, job, job.Progress.PhotoIDs)
	case fbdomain.PublishVideo:
		return w.publishVideo(ctx, page, job)
	case fbdomain.PublishReel:
		return w.publishReel(ctx, page, job)
	case fbdomain.PublishStory:
		if req.IsVideoStory() {
			return w.publishVideoStory(ctx, page, job)
		}
		return w.publishPhotoStory(ctx, page, job)
	}
	return fmt.Errorf("%w: %s", fbdomain.ErrPublishKindUnavailable, req.Kind)
}

func (w *PublishJobWorker) uploadPhotos(ctx context.Context, page *fbdomain.Page, job *fbdomain.PublishJob) error {
	for i := len(job.Progress.PhotoIDs); i < len(job.Request.Media); i++ {
		result, err := w.d.Service.UploadPhoto(ctx, page.FBPageID, page.PageToken, fbdomain.PhotoInput{
			URL: job.Request.Media[i].URL, Published: false, Temporary: job.Request.Scheduled(),
		})
		if err != nil {
			return unpublishedStepFailure(err)
		}
		job.Progress.PhotoIDs = append(job.Progress.PhotoIDs, result.PhotoID)
		if err := w.d.Jobs.Save(ctx, job); err != nil {
			return err
		}
	}
	return nil
}

func (w *PublishJobWorker) createPhotoPost(ctx context.Context, page *fbdomain.Page, job *fbdomain.PublishJob) error {
	return w.creating(ctx, job, func() (string, error) {
		result, err := w.d.Service.UploadPhoto(ctx, page.FBPageID, page.PageToken, fbdomain.PhotoInput{
			URL: job.Request.Media[0].URL, Caption: job.Request.Message, Published: true,
		})
		if err != nil {
			return "", err
		}
		job.FBObjectID = result.PhotoID
		return result.PostID, nil
	})
}

func (w *PublishJobWorker) createFeedPost(ctx context.Context, page *fbdomain.Page, job *fbdomain.PublishJob, attached []string) error {
	return w.creating(ctx, job, func() (string, error) {
		return w.d.Service.CreateFeedPost(ctx, page.FBPageID, page.PageToken, fbdomain.FeedPostInput{
			Message: job.Request.Message, Link: job.Request.Link,
			AttachedMedia: attached, ScheduledAt: job.Request.ScheduledAt,
		})
	})
}

func (w *PublishJobWorker) creating(ctx context.Context, job *fbdomain.PublishJob, create func() (string, error)) error {
	job.Progress.Phase = fbdomain.PhaseCreating
	if err := w.d.Jobs.Save(ctx, job); err != nil {
		return err
	}
	fbPostID, err := create()
	if err != nil {
		if fbdomain.Classify(err) == fbdomain.FailureRetryable {
			job.Progress.Phase = ""
			return fmt.Errorf("%w: %w", errPublishRetry, err)
		}
		return err
	}
	job.FBPostID = fbPostID
	return nil
}

func unpublishedStepFailure(err error) error {
	switch fbdomain.Classify(err) {
	case fbdomain.FailureRetryable, fbdomain.FailureUnknown:
		return fmt.Errorf("%w: %w", errPublishRetry, err)
	}
	return err
}

func (w *PublishJobWorker) settle(ctx context.Context, job *fbdomain.PublishJob, err error) error {
	switch {
	case job.Request.Kind == fbdomain.PublishReel && fbdomain.HasCode(err, fbdomain.CodeReelCap):
		job.Fail(fbdomain.CodeReelCap, 0, fbdomain.ErrReelLimit.Error(), false)
	case errors.Is(err, errPublishRetry):
		job.Status = fbdomain.JobQueued
		if saveErr := w.d.Jobs.Save(ctx, job); saveErr != nil {
			return saveErr
		}
		return err
	case fbdomain.Classify(err) == fbdomain.FailureUnknown && job.Progress.Phase == fbdomain.PhaseCreating:
		job.Fail(0, 0, "Facebook did not confirm the post; check the Page before retrying: "+err.Error(), true)
	default:
		code, subcode := fbdomain.ErrorCodes(err)
		job.Fail(code, subcode, err.Error(), false)
	}
	log.Printf("[facebook-publish] job %s failed: %v", job.ID, err)
	return w.d.Jobs.Save(ctx, job)
}

func (w *PublishJobWorker) complete(ctx context.Context, page *fbdomain.Page, job *fbdomain.PublishJob) error {
	job.Status = fbdomain.JobPublished
	if job.Request.Scheduled() {
		job.Status = fbdomain.JobScheduled
	}
	job.Progress.Phase = ""
	if err := w.d.Jobs.Save(ctx, job); err != nil {
		return err
	}
	now := w.now()
	post := &fbdomain.Post{
		WorkspaceID: page.WorkspaceID, PageID: page.ID, FBPostID: job.FBPostID,
		Kind: postKindFor(job.Request.Kind), Message: job.Request.Message, CreatedByApp: true,
		IsPublished: !job.Request.Scheduled(), ScheduledPublishTime: job.Request.ScheduledAt, CreatedTime: &now,
	}
	if err := w.d.Posts.Track(ctx, post); err != nil {
		log.Printf("[facebook-publish] post %s published but not mirrored: %v", job.FBPostID, err)
	}
	return nil
}

var publishedKinds = map[fbdomain.PublishKind]fbdomain.PostKind{
	fbdomain.PublishText:  fbdomain.PostStatus,
	fbdomain.PublishLink:  fbdomain.PostLink,
	fbdomain.PublishPhoto: fbdomain.PostPhoto,
	fbdomain.PublishAlbum: fbdomain.PostAlbum,
	fbdomain.PublishVideo: fbdomain.PostVideo,
	fbdomain.PublishReel:  fbdomain.PostReel,
	fbdomain.PublishStory: fbdomain.PostStory,
}

func postKindFor(kind fbdomain.PublishKind) fbdomain.PostKind {
	if k, ok := publishedKinds[kind]; ok {
		return k
	}
	return fbdomain.PostStatus
}

func (w *PublishJobWorker) Reap(ctx context.Context) error {
	ids, err := w.d.Jobs.ReleaseStale(ctx, w.now().Add(-staleJobAge), reapBatchSize)
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		if err := w.d.Queue.Enqueue(id); err != nil {
			errs = append(errs, fmt.Errorf("requeue %s: %w", id, err))
		}
	}
	return errors.Join(append(errs, w.checkProcessing(ctx))...)
}

func ClassifyPublishFailure(err error) webhook_usecase.Disposition {
	if errors.Is(err, fbdomain.ErrPublishJobNotFound) || errors.Is(err, fbdomain.ErrPageNotFound) {
		return webhook_usecase.DispositionDrop
	}
	return webhook_usecase.DispositionRetry
}

const (
	publishConcurrency = 4
	publishTimeout     = 5 * time.Minute
)

func NewPublishConsumer(sub messaging.MessageQueueSub, pub messaging.MessageQueuePub, state cache.SharedState, worker *PublishJobWorker) *webhook_usecase.ConsumerRunner[PublishJobMessage] {
	return webhook_usecase.NewConsumerRunner(webhook_usecase.ConsumerConfig[PublishJobMessage]{
		Name:        "facebook-publish",
		Topic:       fbdomain.PublishJobsTopic,
		QueueSub:    sub,
		QueuePub:    pub,
		SharedState: state,
		Concurrency: publishConcurrency,
		Timeout:     publishTimeout,
		Handle:      worker.Execute,
		Classify:    ClassifyPublishFailure,
	})
}

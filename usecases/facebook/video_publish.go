package facebook

import (
	"context"
	"errors"
	"log"
	"time"

	fbdomain "vozko/domain/facebook"
)

const (
	firstVideoCheck    = 15 * time.Second
	maxVideoCheckGap   = 5 * time.Minute
	videoProcessingTTL = 2 * time.Hour
	processingBatch    = 20
)

func (w *PublishJobWorker) publishVideo(ctx context.Context, page *fbdomain.Page, job *fbdomain.PublishJob) error {
	req := job.Request
	err := w.creating(ctx, job, func() (string, error) {
		videoID, err := w.d.Service.CreateVideo(ctx, page.FBPageID, page.PageToken, fbdomain.VideoInput{
			FileURL: req.Media[0].URL, Title: req.VideoTitle, Description: req.Message, ScheduledAt: req.ScheduledAt,
		})
		if err != nil {
			return "", err
		}
		job.FBObjectID = videoID
		return "", nil
	})
	if err != nil {
		return err
	}
	return w.awaitProcessing(ctx, job)
}

func (w *PublishJobWorker) publishReel(ctx context.Context, page *fbdomain.Page, job *fbdomain.PublishJob) error {
	if err := w.uploadVideo(ctx, page, job, fbdomain.VideoForReel); err != nil {
		return err
	}
	err := w.creating(ctx, job, func() (string, error) {
		return w.d.Service.FinishReel(ctx, page.FBPageID, page.PageToken, job.Progress.VideoID, fbdomain.ReelFinish{
			Description: job.Request.Message, ScheduledAt: job.Request.ScheduledAt,
		})
	})
	if err != nil {
		return err
	}
	return w.awaitProcessing(ctx, job)
}

func (w *PublishJobWorker) publishVideoStory(ctx context.Context, page *fbdomain.Page, job *fbdomain.PublishJob) error {
	if err := w.uploadVideo(ctx, page, job, fbdomain.VideoForStory); err != nil {
		return err
	}
	err := w.creating(ctx, job, func() (string, error) {
		return w.d.Service.FinishVideoStory(ctx, page.FBPageID, page.PageToken, job.Progress.VideoID)
	})
	if err != nil {
		return err
	}
	return w.awaitProcessing(ctx, job)
}

func (w *PublishJobWorker) publishPhotoStory(ctx context.Context, page *fbdomain.Page, job *fbdomain.PublishJob) error {
	if err := w.uploadPhotos(ctx, page, job); err != nil {
		return err
	}
	return w.creating(ctx, job, func() (string, error) {
		return w.d.Service.CreatePhotoStory(ctx, page.FBPageID, page.PageToken, job.Progress.PhotoIDs[0])
	})
}

func (w *PublishJobWorker) uploadVideo(ctx context.Context, page *fbdomain.Page, job *fbdomain.PublishJob, target fbdomain.VideoTarget) error {
	if job.Progress.VideoID == "" {
		session, err := w.d.Service.StartVideoUpload(ctx, page.FBPageID, page.PageToken, target)
		if err != nil {
			return unpublishedStepFailure(err)
		}
		job.Progress.VideoID, job.Progress.UploadURL = session.VideoID, session.UploadURL
		job.Progress.Phase, job.FBObjectID = fbdomain.PhaseVideoStarted, session.VideoID
		if err := w.d.Jobs.Save(ctx, job); err != nil {
			return err
		}
	}
	if job.Progress.Phase != fbdomain.PhaseVideoStarted {
		return nil
	}
	session := fbdomain.VideoSession{VideoID: job.Progress.VideoID, UploadURL: job.Progress.UploadURL}
	if err := w.d.Service.TransferVideo(ctx, page.PageToken, session, job.Request.Media[0].URL); err != nil {
		return unpublishedStepFailure(err)
	}
	job.Progress.Phase = fbdomain.PhaseTransferred
	return w.d.Jobs.Save(ctx, job)
}

func (w *PublishJobWorker) awaitProcessing(ctx context.Context, job *fbdomain.PublishJob) error {
	next := w.now().Add(firstVideoCheck)
	job.Status, job.Progress.Phase, job.NextCheckAt = fbdomain.JobProcessing, fbdomain.PhaseProcessing, &next
	return w.d.Jobs.Save(ctx, job)
}

func (w *PublishJobWorker) ResolveVideo(ctx context.Context, videoID, state string) error {
	job, err := w.d.Jobs.FindProcessingByVideoID(ctx, videoID)
	if errors.Is(err, fbdomain.ErrPublishJobNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	switch status := (fbdomain.VideoStatus{State: state}); {
	case status.Failed():
		job.Fail(0, 0, "Facebook could not process the video", false)
		return w.d.Jobs.Save(ctx, job)
	case status.Ready():
		return w.checkVideo(ctx, job)
	}
	return nil
}

func (w *PublishJobWorker) checkProcessing(ctx context.Context) error {
	due, err := w.d.Jobs.ListDueProcessing(ctx, w.now(), processingBatch)
	if err != nil {
		return err
	}
	var errs []error
	for _, job := range due {
		if err := w.checkVideo(ctx, job); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (w *PublishJobWorker) checkVideo(ctx context.Context, job *fbdomain.PublishJob) error {
	page, err := w.d.Pages.FindByID(ctx, job.PageID)
	if err != nil {
		return err
	}
	status, err := w.d.Service.VideoStatus(ctx, page.PageToken, job.FBObjectID)
	if err != nil {
		log.Printf("[facebook-publish] job %s video %s status unavailable: %v", job.ID, job.FBObjectID, err)
		return w.checkAgain(ctx, job)
	}
	switch {
	case status.Ready():
		if job.FBPostID == "" {
			job.FBPostID = status.PostID
		}
		if job.FBPostID == "" {
			job.FBPostID = page.FBPageID + "_" + job.FBObjectID
		}
		return w.complete(ctx, page, job)
	case status.Failed():
		job.Fail(0, 0, "Facebook could not process the video", false)
		return w.d.Jobs.Save(ctx, job)
	case w.now().Sub(job.CreatedAt) > videoProcessingTTL:
		job.Fail(0, 0, "Facebook was still processing the video after 2 hours", false)
		return w.d.Jobs.Save(ctx, job)
	}
	return w.checkAgain(ctx, job)
}

func (w *PublishJobWorker) checkAgain(ctx context.Context, job *fbdomain.PublishJob) error {
	gap := firstVideoCheck << min(job.Progress.Checks, 5)
	if gap > maxVideoCheckGap {
		gap = maxVideoCheckGap
	}
	next := w.now().Add(gap)
	job.Progress.Checks++
	job.NextCheckAt = &next
	return w.d.Jobs.Save(ctx, job)
}

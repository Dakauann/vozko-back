package report_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"vozko/domain/export"
	"vozko/domain/messaging"
	"vozko/domain/report"
)

const (
	renderTimeout   = 10 * time.Minute
	progressUpdates = 5
)

type Worker struct {
	service    *Service
	subscriber messaging.MessageQueueSub
}

func NewWorker(service *Service, subscriber messaging.MessageQueueSub) *Worker {
	return &Worker{service: service, subscriber: subscriber}
}

func (w *Worker) Start() error {
	if w == nil || w.service == nil || w.subscriber == nil {
		return ErrNotConfigured
	}
	if err := w.service.ready(); err != nil {
		return err
	}
	for _, topic := range report.QueueTopics() {
		if err := w.subscriber.Subscribe(topic, w.handle); err != nil {
			return fmt.Errorf("report worker: subscribing to %s: %w", topic, err)
		}
	}
	return nil
}

func (w *Worker) handle(message []byte, ack messaging.MessageAck) {
	var payload report.QueueMessage
	if err := json.Unmarshal(message, &payload); err != nil {
		w.service.logf("discarding an unreadable job message: %v", err)
		_ = ack.Nack(false)
		return
	}

	err := w.process(payload)
	if err == nil {
		_ = ack.Ack()
		return
	}

	if isTerminalRenderError(err) || ack.DeliveryCount() >= messaging.MaxRetries {
		w.service.logf("job %s failed after %d attempt(s): %v", payload.JobID, ack.DeliveryCount(), err)
		_ = w.service.repo.MarkFailed(payload.JobID, failureCodeFor(err), w.service.now())
		_ = ack.Nack(false)
		return
	}

	w.service.logf("job %s attempt %d failed, retrying: %v", payload.JobID, ack.DeliveryCount(), err)
	_ = ack.Nack(true)
}

func (w *Worker) process(payload report.QueueMessage) error {
	job, err := w.service.repo.GetByID(payload.WorkspaceID, payload.JobID)
	if err != nil {
		if errors.Is(err, report.ErrNotFound) {
			return nil
		}
		return err
	}
	if job.Status.Terminal() {
		return nil
	}

	renderer, found := w.service.registry.Lookup(job.Kind)
	if !found {
		_ = w.service.repo.MarkFailed(job.ID, report.FailureNoRenderer, w.service.now())
		return nil
	}

	if err := w.service.repo.MarkRunning(job.ID, w.service.now()); err != nil {
		if errors.Is(err, report.ErrNotFound) {
			return nil
		}
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), renderTimeout)
	defer cancel()

	lastReported := 0
	progress := func(percent int) {
		if percent-lastReported < progressUpdates && percent < 100 {
			return
		}
		lastReported = percent
		_ = w.service.repo.UpdateProgress(job.ID, percent)
	}

	artifact, err := renderer.Render(ctx, *job, progress)
	if err != nil {
		return fmt.Errorf("render %s: %w", job.Kind, err)
	}

	key := objectKeyFor(*job, artifact.Filename)
	contentType := artifact.ContentType
	if contentType == "" {
		contentType = job.Format.ContentType()
	}
	if err := w.service.storage.Upload(key, artifact.Data, contentType); err != nil {
		return fmt.Errorf("upload %s: %w", job.ID, err)
	}

	expires := w.service.now().Add(w.service.retention)
	return w.service.repo.MarkDone(
		job.ID,
		key,
		artifact.Filename,
		int64(len(artifact.Data)),
		artifact.RowCount,
		w.service.now(),
		&expires,
	)
}

func objectKeyFor(job report.Job, filename string) string {
	extension := string(job.Format)
	_ = filename
	return fmt.Sprintf("reports/%s/%s.%s", job.WorkspaceID, job.ID, extension)
}

func isTerminalRenderError(err error) bool {
	return errors.Is(err, export.ErrTooManyRows) ||
		errors.Is(err, report.ErrEmptyResult) ||
		errors.Is(err, report.ErrUnknownKind) ||
		errors.Is(err, report.ErrFormatUnsupported) ||
		errors.Is(err, report.ErrNoRenderer)
}

func failureCodeFor(err error) report.FailureCode {
	switch {
	case errors.Is(err, export.ErrTooManyRows):
		return report.FailureTooManyRows
	case errors.Is(err, report.ErrEmptyResult):
		return report.FailureEmptyResult
	case errors.Is(err, report.ErrUnknownKind):
		return report.FailureUnknownKind
	case errors.Is(err, report.ErrFormatUnsupported):
		return report.FailureUnsupported
	case errors.Is(err, report.ErrNoRenderer):
		return report.FailureNoRenderer
	case errors.Is(err, context.DeadlineExceeded):
		return report.FailureRenderFailed
	}
	return report.FailureRenderFailed
}

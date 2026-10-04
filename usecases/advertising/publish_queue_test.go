package advertising

import (
	"context"
	"errors"
	"testing"

	ads "vozko/domain/advertising"
	webhook_usecase "vozko/usecases/webhook"
)

type recordingQueue struct {
	enqueued []ads.PublishJobMessage
	err      error
}

func (q *recordingQueue) Enqueue(workspaceID, jobID string) error {
	if q.err != nil {
		return q.err
	}
	q.enqueued = append(q.enqueued, ads.PublishJobMessage{WorkspaceID: workspaceID, JobID: jobID})
	return nil
}

func queuedPublisher(w *world, queue *recordingQueue) *PublishUseCase {
	uc := NewPublishUseCase(w.sync, w.gateway, w.jobs, w.numbers, w.media, w.fees, queue)
	uc.media.sleep = noSleep
	uc.preflight.media.sleep = noSleep
	return uc
}

func TestPublishingAnswersAtOnceWithAChargedQueuedJob(t *testing.T) {
	w := newWorld()
	queue := &recordingQueue{}
	job, err := queuedPublisher(w, queue).Publish(context.Background(), PublishInput{WorkspaceID: "ws-1", UserID: "u-1", Actor: ads.ActorPerson, Draft: publishableDraft()})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != ads.JobQueued || job.Fee != ads.FeeCharged || len(w.fees.charged) != 1 {
		t.Fatalf("job %+v charged %v", job, w.fees.charged)
	}
	if len(queue.enqueued) != 1 || queue.enqueued[0] != (ads.PublishJobMessage{WorkspaceID: "ws-1", JobID: job.ID}) {
		t.Fatalf("enqueued %+v", queue.enqueued)
	}
	if writes := metaWrites(w.gateway.calls); len(writes) != 0 {
		t.Fatalf("the request reached Meta: %v", writes)
	}
}

func TestAJobThatCannotBeQueuedStaysQueuedForTheResumerAndIsChargedOnce(t *testing.T) {
	w := newWorld()
	job, err := queuedPublisher(w, &recordingQueue{err: errors.New("rabbit down")}).Publish(context.Background(), PublishInput{WorkspaceID: "ws-1", UserID: "u-1", Actor: ads.ActorPerson, Draft: publishableDraft()})
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := w.jobs.Find(context.Background(), "ws-1", job.ID)
	if stored.Status != ads.JobQueued || len(w.fees.charged) != 1 || len(w.fees.refunded) != 0 {
		t.Fatalf("stored %+v charged %v refunded %v", stored, w.fees.charged, w.fees.refunded)
	}
}

func TestTheWorkerRunsAQueuedJobToTheEnd(t *testing.T) {
	w := newWorld()
	queue := &recordingQueue{}
	uc := queuedPublisher(w, queue)
	job, err := uc.Publish(context.Background(), PublishInput{WorkspaceID: "ws-1", UserID: "u-1", Actor: ads.ActorPerson, Draft: publishableDraft()})
	if err != nil {
		t.Fatal(err)
	}
	if err := uc.Process(context.Background(), &queue.enqueued[0]); err != nil {
		t.Fatal(err)
	}
	done, _ := w.jobs.Find(context.Background(), "ws-1", job.ID)
	if done.Status != ads.JobPublished {
		t.Fatalf("job %+v", done)
	}
	writes := len(metaWrites(w.gateway.calls))
	if err := uc.Process(context.Background(), &queue.enqueued[0]); err != nil {
		t.Fatal(err)
	}
	if len(metaWrites(w.gateway.calls)) != writes || len(w.fees.charged) != 1 {
		t.Fatal("a repeated message published again")
	}
}

func TestTheWorkerDropsAMessageWithoutAJob(t *testing.T) {
	w := newWorld()
	err := queuedPublisher(w, &recordingQueue{}).Process(context.Background(), &ads.PublishJobMessage{WorkspaceID: "ws-1", JobID: "missing"})
	if !errors.Is(err, ads.ErrJobNotFound) || ClassifyPublishFailure(err) != webhook_usecase.DispositionDrop {
		t.Fatalf("got %v", err)
	}
	if ClassifyPublishFailure(errors.New("db down")) == webhook_usecase.DispositionDrop {
		t.Fatal("a database outage dropped the message")
	}
}

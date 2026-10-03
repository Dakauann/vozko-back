package imagegen

import (
	"context"
	"time"
)

type Generator interface {
	Generate(ctx context.Context, req Request, references []ReferenceImage) (*GeneratedImage, error)
}

type Repository interface {
	Create(ctx context.Context, job *Job) error
	Get(ctx context.Context, workspaceID, id string) (*Job, error)
	FindActive(ctx context.Context, workspaceID, requestedBy, fingerprint string, since time.Time) (*Job, error)
	Claim(ctx context.Context, id string) (*Job, bool, error)
	MarkDone(ctx context.Context, id string, result Result, at time.Time) error
	MarkFailed(ctx context.Context, id string, code FailureCode, at time.Time) error
	FailStale(ctx context.Context, createdBefore time.Time, limit int) ([]string, error)
}

type Queue interface {
	Enqueue(jobID string) error
}

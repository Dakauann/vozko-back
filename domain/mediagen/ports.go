package mediagen

import (
	"context"
	"time"
)

type Generator interface {
	Generate(ctx context.Context, req Request, sources []Source) (*Output, error)
}

type Repository interface {
	Create(ctx context.Context, job *Job) error
	Get(ctx context.Context, workspaceID, id string) (*Job, error)
	FindActive(ctx context.Context, workspaceID, requestedBy, fingerprint string, since time.Time) (*Job, error)
	FindDelivered(ctx context.Context, workspaceID string, kind Kind, sourceMediaID string) (*Job, error)
	Claim(ctx context.Context, id string) (*Job, bool, error)
	MarkDone(ctx context.Context, id string, result Result, at time.Time) error
	MarkFailed(ctx context.Context, id string, code FailureCode, at time.Time) error
	FailStale(ctx context.Context, createdBefore time.Time, limit int) ([]string, error)
	MarkSettling(ctx context.Context, id string, settlement Settlement, at time.Time) error
	ListSettling(ctx context.Context, createdAfter time.Time, limit int) ([]*Job, error)
	Settle(ctx context.Context, id string, at time.Time) (*Job, bool, error)
	ExpireSettling(ctx context.Context, createdBefore time.Time, limit int) ([]string, error)
	CountActive(ctx context.Context, workspaceID string, kinds []Kind, since time.Time) (int, error)
}

type ProcessingCharges interface {
	Charge(ctx context.Context, job *Job) error
	Priced(kind Kind) bool
}

type CostLookup interface {
	CostMicros(ctx context.Context, generationID string) (int64, bool)
}

type Queue interface {
	Enqueue(job *Job) error
}

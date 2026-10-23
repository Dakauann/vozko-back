package emailwebhook

import (
	"context"
	"time"

	"vozko/domain/emailapi"
)

type EndpointStore interface {
	Create(ctx context.Context, e *Endpoint, limit int) error
	List(ctx context.Context, workspaceID string) ([]Endpoint, error)
	Get(ctx context.Context, workspaceID, id string) (*Endpoint, error)
	Update(ctx context.Context, e *Endpoint) error
	SetSecret(ctx context.Context, workspaceID, id string, cipher []byte, hint string, at time.Time) error
	Delete(ctx context.Context, workspaceID, id string) error
	Subscribed(ctx context.Context, workspaceID string, t EventType) ([]Endpoint, error)
	MarkSuccess(ctx context.Context, id string, at time.Time) error
	MarkFailure(ctx context.Context, id string, at time.Time) error
	MarkAlerted(ctx context.Context, id string, at time.Time) error
}

type Target struct {
	Delivery Delivery
	Endpoint Endpoint
}

type Finish struct {
	ID         string
	Attempt    Attempt
	Outcome    Outcome
	StatusCode int
	Error      string
	At         time.Time
}

type Requeued struct {
	ID         string
	EndpointID string
	Attempts   int
}

type DeliveryStore interface {
	InsertBatch(ctx context.Context, deliveries []Delivery) ([]Delivery, error)
	Target(ctx context.Context, id string) (*Target, error)
	Claim(ctx context.Context, id string, attempt int, at time.Time) (bool, error)
	Finish(ctx context.Context, f Finish) error
	Skip(ctx context.Context, id string, reason string, at time.Time) error
	Requeue(ctx context.Context, now time.Time, limit int) ([]Requeued, error)
	Retry(ctx context.Context, workspaceID, endpointID, id string, at time.Time) (*Delivery, error)
	ListByEndpoint(ctx context.Context, workspaceID, endpointID string, cursor emailapi.Cursor, limit int) ([]Delivery, error)
	Attempts(ctx context.Context, deliveryIDs []string) (map[string][]Attempt, error)
	PurgeAttempts(ctx context.Context, before time.Time, limit int) (int64, error)
	PurgeDeliveries(ctx context.Context, before time.Time, limit int) (int64, error)
}

type Cipher interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

type HostChecker interface {
	CheckHost(ctx context.Context, host string) error
}

type Response struct {
	StatusCode int
}

type Poster interface {
	Post(ctx context.Context, url string, headers map[string]string, body []byte) (Response, error)
}

type Slots interface {
	Acquire(endpointID string) (release func(), ok bool, err error)
}

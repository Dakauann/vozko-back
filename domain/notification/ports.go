package notification

import (
	"context"
	"time"

	"vozko/domain/shared"
)

type Outbox interface {
	Create(ctx context.Context, r Record) (Record, bool, error)
	Get(ctx context.Context, id string) (Record, error)
	AddRecipients(ctx context.Context, recipients []Recipient) error
	MarkDelivered(ctx context.Context, id string, recipients int, at time.Time) error
	StalePending(ctx context.Context, createdBefore time.Time, limit int) ([]string, error)
	Withdraw(ctx context.Context, id string) error
	PurgeBefore(ctx context.Context, cutoff time.Time, limit int) (int, error)
}

type Inbox interface {
	List(ctx context.Context, v Viewer, q ListQuery) ([]Item, error)
	Summary(ctx context.Context, v Viewer, scanLimit int) (Summary, error)
	MarkRead(ctx context.Context, v Viewer, ids []string, at time.Time) (int, error)
	MarkUnread(ctx context.Context, v Viewer, ids []string) (int, error)
	MarkAllRead(ctx context.Context, v Viewer, category Category, at time.Time) (int, error)
}

type PreferenceStore interface {
	Preferences(ctx context.Context, v Viewer) (Preferences, error)
	PreferencesFor(ctx context.Context, workspaceID string, userIDs []string) (map[string]Preferences, error)
	SavePreferences(ctx context.Context, p Preferences) (Preferences, error)
}

type DigestStore interface {
	HoldForDigest(ctx context.Context, notificationID string, userIDs []string) error
	WorkspacesWithHeldDigests(ctx context.Context, upTo time.Time, afterID string, limit int) ([]string, error)
	HeldDigests(ctx context.Context, workspaceID string, upTo time.Time, itemLimit int) ([]Digest, error)
	ReleaseDigests(ctx context.Context, workspaceID string, upTo time.Time, keepUserIDs []string) error
}

type BroadcastStore interface {
	CreateBroadcast(ctx context.Context, b Broadcast) (Broadcast, error)
	Broadcast(ctx context.Context, id string) (Broadcast, error)
	CompleteBroadcast(ctx context.Context, id string, workspaces int, at time.Time) error
	Broadcasts(ctx context.Context, before shared.Keyset, paged bool, limit int) ([]Broadcast, error)
	StaleQueued(ctx context.Context, createdBefore time.Time, limit int) ([]string, error)
	BroadcastStats(ctx context.Context, broadcastIDs []string) (map[string]BroadcastStats, error)
	BroadcastRecipients(ctx context.Context, broadcastID string, q RecipientQuery) ([]BroadcastRecipient, error)
}

type Directory interface {
	Members(ctx context.Context, workspaceID string) ([]Member, error)
	MembersOf(ctx context.Context, workspaceIDs []string) (map[string][]Member, error)
	WorkspaceName(ctx context.Context, workspaceID string) (string, error)
	WorkspaceIDs(ctx context.Context, afterID string, limit int) ([]string, error)
}

type Pusher interface {
	PushNotification(workspaceID string, userIDs []string, item Item)
}

type Queue interface {
	EnqueueFanout(ctx context.Context, notificationID string) error
	EnqueueBroadcast(ctx context.Context, broadcastID string) error
}

type ImageStore interface {
	Save(ctx context.Context, data []byte, img Image) (string, error)
	Owns(url string) bool
}

type Publisher interface {
	Publish(ctx context.Context, d Draft) (Record, error)
}

type Resolver interface {
	Resolve(ctx context.Context, workspaceID string, episodeKeys ...string) error
}

type EpisodePublisher interface {
	Publisher
	Resolver
}

type EpisodeStore interface {
	OpenEpisode(ctx context.Context, workspaceID, key string) (Episode, bool, error)
	StartEpisode(ctx context.Context, e Episode) (Episode, error)
	SeeEpisode(ctx context.Context, id string, at time.Time) error
	AdvanceEpisode(ctx context.Context, id string, step EpisodeStep, notificationID string, at time.Time) (bool, error)
	CloseEpisode(ctx context.Context, id string, at time.Time, reason EpisodeClose) error
	ResolveEpisodes(ctx context.Context, workspaceID string, keys []string, at time.Time) (int, error)
	PurgeEpisodesBefore(ctx context.Context, cutoff time.Time, limit int) (int, error)
}

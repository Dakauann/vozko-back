package notification_usecase

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"vozko/domain/notification"
	"vozko/domain/shared"
	"vozko/domain/workspace"
)

const defaultWorkspacePage = 500

type BroadcastsDeps struct {
	Store     notification.BroadcastStore
	Directory notification.Directory
	Publisher notification.Publisher
	Queue     notification.Queue
	Images      notification.ImageStore
	Preferences notification.PreferenceStore
	Permissions workspace.PermissionChecker
	Now         func() time.Time
	PageSize    int
}

type Broadcasts struct {
	store     notification.BroadcastStore
	directory notification.Directory
	publisher notification.Publisher
	queue     notification.Queue
	images      notification.ImageStore
	preferences notification.PreferenceStore
	selector    recipientSelector
	now         func() time.Time
	pageSize    int
}

func NewBroadcasts(d BroadcastsDeps) (*Broadcasts, error) {
	if d.Store == nil || d.Directory == nil || d.Publisher == nil || d.Queue == nil || d.Images == nil || d.Preferences == nil {
		return nil, errors.New("notification broadcasts: store, directory, publisher, queue, image store and preferences are required")
	}
	if d.PageSize <= 0 {
		d.PageSize = defaultWorkspacePage
	}
	return &Broadcasts{
		store: d.Store, directory: d.Directory, publisher: d.Publisher, queue: d.Queue,
		images: d.Images, preferences: d.Preferences, selector: recipientSelector{permissions: d.Permissions},
		now: clockOrNow(d.Now), pageSize: d.PageSize,
	}, nil
}

func (b *Broadcasts) Create(ctx context.Context, adminID string, draft notification.BroadcastDraft) (notification.Broadcast, error) {
	draft = draft.Normalize()
	if err := draft.Validate(); err != nil {
		return notification.Broadcast{}, err
	}
	if draft.ImageURL != "" && !b.images.Owns(draft.ImageURL) {
		return notification.Broadcast{}, notification.ErrImageForeign
	}
	stored, err := b.store.CreateBroadcast(ctx, notification.Broadcast{
		BroadcastDraft: draft, CreatedBy: adminID, Status: notification.BroadcastQueued, CreatedAt: b.now(),
	})
	if err != nil {
		return notification.Broadcast{}, err
	}
	if err := b.queue.EnqueueBroadcast(ctx, stored.ID); err != nil {
		log.Printf("[notifications] broadcast %s not queued, the sweeper will retry: %v", stored.ID, err)
	}
	return stored, nil
}

func (b *Broadcasts) Expand(ctx context.Context, broadcastID string) error {
	broadcast, err := b.store.Broadcast(ctx, broadcastID)
	if err != nil {
		return err
	}
	if broadcast.Status == notification.BroadcastCompleted {
		return nil
	}
	published := 0
	err = b.eachTargetPage(ctx, broadcast.Target, func(workspaceIDs []string) error {
		for _, workspaceID := range workspaceIDs {
			if _, err := b.publisher.Publish(ctx, broadcast.DraftFor(workspaceID)); err != nil {
				return err
			}
			published++
		}
		return nil
	})
	if err != nil {
		return err
	}
	return b.store.CompleteBroadcast(ctx, broadcast.ID, published, b.now())
}

func (b *Broadcasts) eachTargetPage(ctx context.Context, target notification.BroadcastTarget, visit func([]string) error) error {
	if !target.All {
		for start := 0; start < len(target.WorkspaceIDs); start += b.pageSize {
			if err := visit(target.WorkspaceIDs[start:min(start+b.pageSize, len(target.WorkspaceIDs))]); err != nil {
				return err
			}
		}
		return nil
	}
	after := ""
	for {
		page, err := b.directory.WorkspaceIDs(ctx, after, b.pageSize)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			return nil
		}
		if err := visit(page); err != nil {
			return err
		}
		if len(page) < b.pageSize {
			return nil
		}
		after = page[len(page)-1]
	}
}

func (b *Broadcasts) List(ctx context.Context, cursor string, limit int) (notification.BroadcastPage, error) {
	before, paged, err := shared.ParseKeyset(cursor)
	if err != nil {
		return notification.BroadcastPage{}, notification.ErrCursorInvalid
	}
	q := notification.ListQuery{Limit: limit}.Normalize()
	rows, err := b.store.Broadcasts(ctx, before, paged, q.Limit+1)
	if err != nil {
		return notification.BroadcastPage{}, err
	}
	page := notification.BroadcastPageOf(rows, q.Limit)
	ids := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		ids = append(ids, item.ID)
	}
	stats, err := b.store.BroadcastStats(ctx, ids)
	if err != nil {
		return notification.BroadcastPage{}, err
	}
	for i := range page.Items {
		page.Items[i].Stats = stats[page.Items[i].ID]
	}
	return page, nil
}

func (b *Broadcasts) Recipients(ctx context.Context, broadcastID, status, cursor string, limit int) (notification.RecipientPage, error) {
	readStatus, err := notification.ParseReadStatus(status)
	if err != nil {
		return notification.RecipientPage{}, err
	}
	after, paged, err := notification.ParseRecipientCursor(cursor)
	if err != nil {
		return notification.RecipientPage{}, err
	}
	if _, err := b.store.Broadcast(ctx, broadcastID); err != nil {
		return notification.RecipientPage{}, err
	}
	q := notification.ListQuery{Limit: limit}.Normalize()
	rows, err := b.store.BroadcastRecipients(ctx, broadcastID, notification.RecipientQuery{Status: readStatus, After: after, Paged: paged, Limit: q.Limit + 1})
	if err != nil {
		return notification.RecipientPage{}, err
	}
	return notification.RecipientPageOf(rows, q.Limit), nil
}

func (b *Broadcasts) Preview(ctx context.Context, draft notification.BroadcastDraft) (notification.BroadcastReach, error) {
	draft = draft.Normalize()
	if err := draft.Validate(); err != nil {
		return notification.BroadcastReach{}, err
	}
	var reach notification.BroadcastReach
	people, emailed := map[string]bool{}, map[string]bool{}
	err := b.eachTargetPage(ctx, draft.Target, func(workspaceIDs []string) error {
		membersByWorkspace, err := b.directory.MembersOf(ctx, workspaceIDs)
		if err != nil {
			return err
		}
		for _, workspaceID := range workspaceIDs {
			selected := b.selector.choose(workspaceID, draft.Audience, membersByWorkspace[workspaceID])
			if len(selected) == 0 {
				continue
			}
			reach.Workspaces++
			reach.Recipients += len(selected)
			for _, m := range selected {
				people[m.UserID] = true
			}
			if draft.Email {
				if err := b.countEmails(ctx, workspaceID, draft, selected, emailed); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return notification.BroadcastReach{}, err
	}
	reach.People, reach.Emails = len(people), len(emailed)
	return reach, nil
}

func (b *Broadcasts) countEmails(ctx context.Context, workspaceID string, draft notification.BroadcastDraft, selected []notification.Member, emailed map[string]bool) error {
	prefs, err := b.preferences.PreferencesFor(ctx, workspaceID, memberIDs(selected))
	if err != nil {
		return err
	}
	for _, m := range selected {
		if strings.TrimSpace(m.Email) != "" && prefs[m.UserID].WantsEmail(draft.Category, draft.Severity) {
			emailed[m.UserID] = true
		}
	}
	return nil
}

type Images struct {
	store notification.ImageStore
}

func NewImages(store notification.ImageStore) *Images {
	return &Images{store: store}
}

func (i *Images) Upload(ctx context.Context, data []byte) (string, error) {
	img, err := notification.ClassifyImage(data)
	if err != nil {
		return "", err
	}
	return i.store.Save(ctx, data, img)
}

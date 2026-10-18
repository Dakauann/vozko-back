package notification_usecase

import (
	"context"
	"errors"
	"strings"
	"time"

	"vozko/domain/notification"
	"vozko/domain/workspace"
)

const (
	PushChunk     = 500
	emailDedupTTL = 7 * 24 * time.Hour
)

type DeliveryDeps struct {
	Outbox       notification.Outbox
	Directory    notification.Directory
	Permissions  workspace.PermissionChecker
	Preferences  notification.PreferenceStore
	Pusher       notification.Pusher
	Email        notification.Notifier
	Digests      notification.DigestStore
	DashboardURL string
	Now          func() time.Time
}

type Delivery struct {
	outbox      notification.Outbox
	directory   notification.Directory
	selector    recipientSelector
	preferences notification.PreferenceStore
	pusher      notification.Pusher
	email       notification.Notifier
	digests     notification.DigestStore
	emails      emailComposer
	now         func() time.Time
}

func NewDelivery(d DeliveryDeps) (*Delivery, error) {
	if d.Outbox == nil || d.Directory == nil || d.Preferences == nil || d.Pusher == nil || d.Email == nil || d.Digests == nil {
		return nil, errors.New("notification delivery: outbox, directory, preferences, pusher, email and digests are required")
	}
	return &Delivery{
		outbox: d.Outbox, directory: d.Directory, selector: recipientSelector{permissions: d.Permissions}, preferences: d.Preferences,
		pusher: d.Pusher, email: d.Email, digests: d.Digests, emails: emailComposer{dashboardURL: strings.TrimRight(d.DashboardURL, "/")},
		now: clockOrNow(d.Now),
	}, nil
}

func (d *Delivery) Deliver(ctx context.Context, notificationID string) error {
	rec, err := d.outbox.Get(ctx, notificationID)
	if err != nil {
		return err
	}
	if rec.Status == notification.StatusDelivered {
		return nil
	}
	members, err := d.directory.Members(ctx, rec.WorkspaceID)
	if err != nil {
		return err
	}
	selected := d.selector.choose(rec.WorkspaceID, rec.Audience, members)
	if err := d.outbox.AddRecipients(ctx, recipientsOf(rec, selected)); err != nil {
		return err
	}
	d.push(rec, selected)
	if rec.Email {
		if err := d.sendEmails(ctx, rec, selected); err != nil {
			return err
		}
	}
	return d.outbox.MarkDelivered(ctx, rec.ID, len(selected), d.now())
}

func recipientsOf(rec notification.Record, members []notification.Member) []notification.Recipient {
	out := make([]notification.Recipient, 0, len(members))
	for _, m := range members {
		out = append(out, notification.Recipient{
			NotificationID: rec.ID, WorkspaceID: rec.WorkspaceID, UserID: m.UserID,
			Category: rec.Category, CreatedAt: rec.CreatedAt,
		})
	}
	return out
}

func (d *Delivery) push(rec notification.Record, members []notification.Member) {
	item := rec.Item()
	for start := 0; start < len(members); start += PushChunk {
		end := min(start+PushChunk, len(members))
		d.pusher.PushNotification(rec.WorkspaceID, memberIDs(members[start:end]), item)
	}
}

func (d *Delivery) sendEmails(ctx context.Context, rec notification.Record, members []notification.Member) error {
	prefs, err := d.preferences.PreferencesFor(ctx, rec.WorkspaceID, memberIDs(members))
	if err != nil {
		return err
	}
	workspaceName, err := d.directory.WorkspaceName(ctx, rec.WorkspaceID)
	if err != nil {
		return err
	}
	email := d.emails.compose(rec, workspaceName)
	var held []string
	for _, m := range members {
		p := prefs[m.UserID]
		if strings.TrimSpace(m.Email) == "" || !p.WantsEmail(rec.Category, rec.Severity) {
			continue
		}
		if p.HoldsForDigest(rec.Severity) {
			held = append(held, m.UserID)
			continue
		}
		if err := d.email.Notify(notification.Notification{
			WorkspaceID:  rec.WorkspaceID,
			Email:        m.Email,
			Subject:      email.Subject,
			Template:     email.Template,
			Placeholders: email.Placeholders,
			DedupKey:     rec.EmailDedupKey(m.UserID),
			DedupTTL:     emailDedupTTL,
		}); err != nil {
			return err
		}
	}
	if len(held) == 0 {
		return nil
	}
	return d.digests.HoldForDigest(ctx, rec.ID, held)
}

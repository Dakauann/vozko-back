package notification_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/brand"
	"vozko/domain/notification"
)

const (
	DigestEmailTemplate = "notification_digest.html"
	digestDedupTTL      = 48 * time.Hour
	digestWorkspacePage = 200
)

type DigestsDeps struct {
	Store        notification.DigestStore
	Directory    notification.Directory
	Email        notification.Notifier
	DashboardURL string
	Now          func() time.Time
	Location     *time.Location
}

type Digests struct {
	store        notification.DigestStore
	directory    notification.Directory
	email        notification.Notifier
	dashboardURL string
	now          func() time.Time
	location     *time.Location
}

type digestLine struct {
	Title         string
	Body          string
	CategoryLabel string
	Tone          string
	ActionURL     string
	SentAt        string
}

func NewDigests(d DigestsDeps) (*Digests, error) {
	if d.Store == nil || d.Directory == nil || d.Email == nil {
		return nil, errors.New("notification digests: store, directory and email are required")
	}
	location := d.Location
	if location == nil {
		location = brasilia
	}
	return &Digests{
		store: d.Store, directory: d.Directory, email: d.Email,
		dashboardURL: strings.TrimRight(d.DashboardURL, "/"), now: clockOrNow(d.Now), location: location,
	}, nil
}

func (d *Digests) Send(ctx context.Context) error {
	cutoff := notification.DigestCutoff(d.now(), d.location)
	var failures []error
	after := ""
	for {
		workspaces, err := d.store.WorkspacesWithHeldDigests(ctx, cutoff, after, digestWorkspacePage)
		if err != nil {
			return errors.Join(append(failures, err)...)
		}
		for _, workspaceID := range workspaces {
			if err := d.sendWorkspace(ctx, workspaceID, cutoff); err != nil {
				failures = append(failures, fmt.Errorf("workspace %s: %w", workspaceID, err))
			}
		}
		if len(workspaces) < digestWorkspacePage {
			return errors.Join(failures...)
		}
		after = workspaces[len(workspaces)-1]
	}
}

func (d *Digests) sendWorkspace(ctx context.Context, workspaceID string, cutoff time.Time) error {
	digests, err := d.store.HeldDigests(ctx, workspaceID, cutoff, notification.DigestItemLimit)
	if err != nil {
		return err
	}
	members, err := d.directory.Members(ctx, workspaceID)
	if err != nil {
		return err
	}
	workspaceName, err := d.directory.WorkspaceName(ctx, workspaceID)
	if err != nil {
		return err
	}
	emails := make(map[string]string, len(members))
	for _, m := range members {
		emails[m.UserID] = strings.TrimSpace(m.Email)
	}
	var kept []string
	var failures []error
	for _, digest := range digests {
		address := emails[digest.UserID]
		if digest.Empty() || address == "" {
			continue
		}
		if err := d.email.Notify(d.message(digest, address, workspaceName, cutoff)); err != nil {
			kept = append(kept, digest.UserID)
			failures = append(failures, err)
		}
	}
	if err := d.store.ReleaseDigests(ctx, workspaceID, cutoff, kept); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func (d *Digests) message(digest notification.Digest, address, workspaceName string, cutoff time.Time) notification.Notification {
	inbox := d.dashboardURL + "/dashboard"
	lines := make([]digestLine, 0, len(digest.Items))
	for _, item := range digest.Items {
		link := inbox
		if item.ActionURL != "" {
			link = notification.AbsoluteLink(d.dashboardURL, item.ActionURL)
		}
		lines = append(lines, digestLine{
			Title: item.Title, Body: item.Body, CategoryLabel: categoryLabels[item.Category], Tone: severityTones[item.Severity],
			ActionURL: link, SentAt: item.CreatedAt.In(d.location).Format("02/01 15:04"),
		})
	}
	day := cutoff.In(d.location)
	return notification.Notification{
		WorkspaceID: digest.WorkspaceID,
		Email:       address,
		Subject:     fmt.Sprintf("Resumo de %s: %d %s - %s", workspaceName, digest.Total, plural(digest.Total, "aviso", "avisos"), brand.Active().Name),
		Template:    DigestEmailTemplate,
		Placeholders: map[string]interface{}{
			"WorkspaceName": workspaceName,
			"Date":          day.Format("02/01/2006"),
			"Items":         lines,
			"Total":         digest.Total,
			"More":          digest.More(),
			"InboxURL":      inbox,
		},
		DedupKey: "notification_digest:" + digest.WorkspaceID + ":" + digest.UserID + ":" + day.Format("2006-01-02"),
		DedupTTL: digestDedupTTL,
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

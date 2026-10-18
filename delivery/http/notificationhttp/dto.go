package notificationhttp

import (
	"time"

	"vozko/delivery/notificationview"
	"vozko/domain/notification"
	"vozko/domain/workspace"
)

type ListResponse struct {
	Items []notificationview.Item `json:"items"`
	Next  string                  `json:"next" example:"MjAyNi0xMC0xNlQxMjowMDowMFp8NmYxZA"`
}

type SummaryResponse struct {
	Unread     int            `json:"unread" example:"3"`
	ByCategory map[string]int `json:"byCategory"`
	Capped     bool           `json:"capped" example:"false"`
}

type IDsRequest struct {
	IDs []string `json:"ids" example:"6f1d1c1e-8f1a-4f7b-9a43-3e8c4f0c9b11"`
}

type ReadAllRequest struct {
	Category string `json:"category,omitempty" example:"campaigns"`
}

type ChangeResponse struct {
	Updated int `json:"updated" example:"2"`
}

type PreferencesRequest struct {
	EmailMuted  []string `json:"emailMuted" example:"billing"`
	EmailDigest bool     `json:"emailDigest" example:"false"`
}

type PreferencesResponse struct {
	Categories  []string `json:"categories" example:"campaigns"`
	EmailMuted  []string `json:"emailMuted" example:"billing"`
	EmailDigest bool     `json:"emailDigest" example:"false"`
}

func presentSummary(s notification.Summary) SummaryResponse {
	byCategory := make(map[string]int, len(s.ByCategory))
	for c, n := range s.ByCategory {
		byCategory[string(c)] = n
	}
	return SummaryResponse{Unread: s.Unread, ByCategory: byCategory, Capped: s.Capped}
}

func presentPreferences(p notification.Preferences) PreferencesResponse {
	return PreferencesResponse{Categories: categoryNames(notification.Categories()), EmailMuted: categoryNames(p.EmailMuted), EmailDigest: p.EmailDigest}
}

func categoryNames(categories []notification.Category) []string {
	out := make([]string, 0, len(categories))
	for _, c := range categories {
		out = append(out, string(c))
	}
	return out
}

type AudienceDTO struct {
	Kind     string   `json:"kind"`
	UserIDs  []string `json:"userIds,omitempty"`
	Resource string   `json:"resource,omitempty"`
	Action   string   `json:"action,omitempty"`
}

type BroadcastRequest struct {
	Target      notification.BroadcastTarget `json:"target"`
	Audience    AudienceDTO                  `json:"audience"`
	Category    string                       `json:"category"`
	Severity    string                       `json:"severity"`
	Title       string                       `json:"title"`
	Body        string                       `json:"body"`
	ImageURL    string                       `json:"imageUrl"`
	ActionURL   string                       `json:"actionUrl"`
	ActionLabel string                       `json:"actionLabel"`
	Email       bool                         `json:"email"`
}

type BroadcastResponse struct {
	ID             string                       `json:"id"`
	Target         notification.BroadcastTarget `json:"target"`
	Audience       AudienceDTO                  `json:"audience"`
	Category       string                       `json:"category"`
	Severity       string                       `json:"severity"`
	Title          string                       `json:"title"`
	Body           string                       `json:"body"`
	ImageURL       string                       `json:"imageUrl,omitempty"`
	ActionURL      string                       `json:"actionUrl,omitempty"`
	ActionLabel    string                       `json:"actionLabel,omitempty"`
	Email          bool                         `json:"email"`
	CreatedBy      string                       `json:"createdBy"`
	Status         string                       `json:"status"`
	WorkspaceCount int                          `json:"workspaceCount"`
	Recipients     int                          `json:"recipients"`
	Read           int                          `json:"read"`
	CreatedAt      time.Time                    `json:"createdAt"`
	CompletedAt    *time.Time                   `json:"completedAt,omitempty"`
}

type BroadcastListResponse struct {
	Items []BroadcastResponse `json:"items"`
	Next  string              `json:"next"`
}

type ImageResponse struct {
	URL string `json:"url"`
}

func (a AudienceDTO) domain() notification.Audience {
	out := notification.Audience{Kind: notification.AudienceKind(a.Kind), UserIDs: a.UserIDs}
	if a.Resource != "" || a.Action != "" {
		out.Permission = &workspace.PermissionEntry{Resource: workspace.Resource(a.Resource), Action: workspace.Action(a.Action)}
	}
	return out
}

func presentAudience(a notification.Audience) AudienceDTO {
	out := AudienceDTO{Kind: string(a.Kind), UserIDs: a.UserIDs}
	if a.Permission != nil {
		out.Resource, out.Action = string(a.Permission.Resource), string(a.Permission.Action)
	}
	return out
}

func (b BroadcastRequest) domain() notification.BroadcastDraft {
	return notification.BroadcastDraft{
		Target: b.Target, Audience: b.Audience.domain(), Category: notification.Category(b.Category),
		Severity: notification.Severity(b.Severity), Title: b.Title, Body: b.Body, ImageURL: b.ImageURL,
		ActionURL: b.ActionURL, ActionLabel: b.ActionLabel, Email: b.Email,
	}
}

func presentBroadcast(b notification.Broadcast) BroadcastResponse {
	return BroadcastResponse{
		ID: b.ID, Target: b.Target, Audience: presentAudience(b.Audience), Category: string(b.Category),
		Severity: string(b.Severity), Title: b.Title, Body: b.Body, ImageURL: b.ImageURL, ActionURL: b.ActionURL,
		ActionLabel: b.ActionLabel, Email: b.Email, CreatedBy: b.CreatedBy, Status: string(b.Status),
		WorkspaceCount: b.WorkspaceCount, Recipients: b.Stats.Recipients, Read: b.Stats.Read,
		CreatedAt: b.CreatedAt, CompletedAt: b.CompletedAt,
	}
}

type ReachResponse struct {
	Workspaces int `json:"workspaces"`
	Recipients int `json:"recipients"`
	People     int `json:"people"`
	Emails     int `json:"emails"`
}

type RecipientResponse struct {
	UserID        string     `json:"userId"`
	Name          string     `json:"name"`
	Email         string     `json:"email"`
	WorkspaceID   string     `json:"workspaceId"`
	WorkspaceName string     `json:"workspaceName"`
	ReadAt        *time.Time `json:"readAt,omitempty"`
}

type RecipientListResponse struct {
	Items []RecipientResponse `json:"items"`
	Next  string              `json:"next"`
}

func presentReach(r notification.BroadcastReach) ReachResponse {
	return ReachResponse{Workspaces: r.Workspaces, Recipients: r.Recipients, People: r.People, Emails: r.Emails}
}

func presentRecipients(page notification.RecipientPage) RecipientListResponse {
	items := make([]RecipientResponse, 0, len(page.Items))
	for _, r := range page.Items {
		items = append(items, RecipientResponse{
			UserID: r.UserID, Name: r.Name, Email: r.Email, WorkspaceID: r.WorkspaceID, WorkspaceName: r.WorkspaceName, ReadAt: r.ReadAt,
		})
	}
	return RecipientListResponse{Items: items, Next: page.Next}
}

package notificationview

import (
	"time"

	"vozko/domain/notification"
)

type Item struct {
	ID          string     `json:"id" example:"6f1d1c1e-8f1a-4f7b-9a43-3e8c4f0c9b11"`
	Category    string     `json:"category" example:"campaigns"`
	Severity    string     `json:"severity" example:"warning"`
	Title       string     `json:"title" example:"Campanha pausada automaticamente"`
	Body        string     `json:"body" example:"O modelo foi pausado pela Meta."`
	ImageURL    string     `json:"imageUrl,omitempty" example:"https://cdn.vozko.com.br/notifications/images/a.png"`
	ActionURL   string     `json:"actionUrl,omitempty" example:"/dashboard/whatsapp-campaigns/6f1d1c1e"`
	ActionLabel string     `json:"actionLabel,omitempty" example:"Ver campanha"`
	CreatedAt   time.Time  `json:"createdAt" example:"2026-10-16T12:00:00Z"`
	ReadAt      *time.Time `json:"readAt,omitempty" example:"2026-10-16T12:05:00Z"`
}

func ItemOf(i notification.Item) Item {
	return Item{
		ID: i.ID, Category: string(i.Category), Severity: string(i.Severity), Title: i.Title, Body: i.Body,
		ImageURL: i.ImageURL, ActionURL: i.ActionURL, ActionLabel: i.ActionLabel, CreatedAt: i.CreatedAt, ReadAt: i.ReadAt,
	}
}

func ItemsOf(items []notification.Item) []Item {
	out := make([]Item, 0, len(items))
	for _, i := range items {
		out = append(out, ItemOf(i))
	}
	return out
}

type Created struct {
	WorkspaceID string `json:"workspaceId"`
	Item        Item   `json:"item"`
}

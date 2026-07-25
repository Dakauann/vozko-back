package shortlink_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/shared"
	"vozko/domain/shortlink"
)

func TestGetAnalytics_Defaults(t *testing.T) {
	var captured shortlink.AnalyticsInput
	repo := &fakeClickRepo{AnalyticsFn: func(ctx context.Context, in shortlink.AnalyticsInput) (*shortlink.Analytics, error) {
		captured = in
		return &shortlink.Analytics{TotalClicks: 5}, nil
	}}
	res, err := NewGetAnalyticsUseCase(repo).Execute(context.Background(), shortlink.AnalyticsInput{ShortLinkID: "l"})
	if err != nil || res.TotalClicks != 5 {
		t.Fatalf("analytics = %v %+v", err, res)
	}
	if captured.To.IsZero() || captured.From.IsZero() {
		t.Fatal("defaults not applied")
	}
	if captured.To.Sub(captured.From) < 29*24*time.Hour {
		t.Fatal("default window too small")
	}
}

func TestGetAnalytics_ExplicitAndError(t *testing.T) {
	from := time.Now().Add(-time.Hour)
	to := time.Now()
	var captured shortlink.AnalyticsInput
	repo := &fakeClickRepo{AnalyticsFn: func(ctx context.Context, in shortlink.AnalyticsInput) (*shortlink.Analytics, error) {
		captured = in
		return &shortlink.Analytics{}, nil
	}}
	_, _ = NewGetAnalyticsUseCase(repo).Execute(context.Background(), shortlink.AnalyticsInput{ShortLinkID: "l", From: from, To: to})
	if !captured.From.Equal(from) || !captured.To.Equal(to) {
		t.Fatal("explicit window not preserved")
	}

	errRepo := &fakeClickRepo{AnalyticsFn: func(ctx context.Context, in shortlink.AnalyticsInput) (*shortlink.Analytics, error) {
		return nil, errors.New("db")
	}}
	if _, err := NewGetAnalyticsUseCase(errRepo).Execute(context.Background(), shortlink.AnalyticsInput{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestListRecentClicks(t *testing.T) {
	repo := &fakeClickRepo{RecentFn: func(ctx context.Context, ws, id string, opts shared.Pagination) (*shared.PaginatedResult[*shortlink.Click], error) {
		return shared.NewPaginatedResult([]*shortlink.Click{{ID: "c"}}, opts, 1), nil
	}}
	res, err := NewListRecentClicksUseCase(repo).Execute(context.Background(), "ws", "id", 1, 20)
	if err != nil || res.TotalItems != 1 {
		t.Fatalf("recent = %v %+v", err, res)
	}
}

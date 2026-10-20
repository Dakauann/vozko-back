package campaignlivehttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"vozko/domain/cache"
	"vozko/domain/campaign"
)

type timelineFunc func(context.Context, campaign.LiveCampaign, campaign.TimelineBucket) (*campaign.Timeline, error)

func (f timelineFunc) Execute(ctx context.Context, live campaign.LiveCampaign, bucket campaign.TimelineBucket) (*campaign.Timeline, error) {
	return f(ctx, live, bucket)
}

func codeOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Code
}

func TestTheTimelineReadsTheRequestedBucket(t *testing.T) {
	var asked campaign.TimelineBucket
	uc := timelineFunc(func(_ context.Context, _ campaign.LiveCampaign, bucket campaign.TimelineBucket) (*campaign.Timeline, error) {
		asked = bucket
		return &campaign.Timeline{Bucket: bucket, Points: []campaign.TimelinePoint{}}, nil
	})
	rec := httptest.NewRecorder()
	WriteTimeline(rec, httptest.NewRequest(http.MethodGet, "/x?bucket=hour", nil), uc, campaign.LiveCampaign{ID: "camp-1"})
	if rec.Code != http.StatusOK || asked != campaign.TimelineHour {
		t.Fatalf("status %d asked %q", rec.Code, asked)
	}
}

func TestLiveRefusalsCarryACode(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{campaign.ErrTimelineBucketInvalid, http.StatusBadRequest, "campaign_timeline_bucket_invalid"},
		{campaign.ErrTimelineLocationMissing, http.StatusServiceUnavailable, "campaign_live_unavailable"},
		{errors.New("db down"), http.StatusServiceUnavailable, "campaign_live_unavailable"},
	}
	for _, tc := range cases {
		uc := timelineFunc(func(context.Context, campaign.LiveCampaign, campaign.TimelineBucket) (*campaign.Timeline, error) {
			return nil, tc.err
		})
		rec := httptest.NewRecorder()
		WriteTimeline(rec, httptest.NewRequest(http.MethodGet, "/x", nil), uc, campaign.LiveCampaign{ID: "camp-1"})
		if rec.Code != tc.status || codeOf(t, rec) != tc.code {
			t.Fatalf("%v answered %d %q", tc.err, rec.Code, codeOf(t, rec))
		}
	}
	busy := timelineFunc(func(context.Context, campaign.LiveCampaign, campaign.TimelineBucket) (*campaign.Timeline, error) {
		return nil, cache.ErrGateBusy
	})
	rec := httptest.NewRecorder()
	WriteTimeline(rec, httptest.NewRequest(http.MethodGet, "/x", nil), busy, campaign.LiveCampaign{ID: "camp-1"})
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("a busy gate answered %d with Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestMissingLiveUseCasesRefuse(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteTimeline(rec, httptest.NewRequest(http.MethodGet, "/x", nil), nil, campaign.LiveCampaign{ID: "camp-1"})
	if rec.Code != http.StatusServiceUnavailable || codeOf(t, rec) != "campaign_live_unavailable" {
		t.Fatalf("timeline without a use case answered %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	WriteProgress(rec, httptest.NewRequest(http.MethodGet, "/x", nil), nil, campaign.LiveCampaign{ID: "camp-1"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("progress without a use case answered %d", rec.Code)
	}
}

func TestLiveRefusalCodesAreStable(t *testing.T) {
	want := []string{"campaign_timeline_bucket_invalid", "campaign_live_unavailable", "campaign_live_unavailable", "campaign_live_unavailable"}
	if got := RefusalCodes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("codes = %v", got)
	}
}

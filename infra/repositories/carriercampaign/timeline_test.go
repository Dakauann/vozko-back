package carriercampaign_repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"vozko/domain/campaign"
	"vozko/domain/carriercampaign"
)

func TestTimelineCountsTopLevelMessagesOfTheCampaignRecipients(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT MIN\(LEAST\(e\.accepted_at, e\.failed_at\)\) AS first, MAX\(GREATEST\(e\.accepted_at, e\.delivered_at, e\.read_at, e\.failed_at\)\) AS last FROM carrier_campaign_recipients r JOIN carrier_messages e ON e\.id = r\.message_id WHERE r\.campaign_id = \$1 AND e\.parent_message_id IS NULL`).
		WithArgs(campaignID).WillReturnRows(sqlmock.NewRows([]string{"first", "last"}).AddRow(testNow, testNow.Add(time.Hour)))
	span, err := NewTimelineReader(db).Span(campaignID)
	if err != nil || span.First == nil || !span.Last.Equal(testNow.Add(time.Hour)) {
		t.Fatalf("span %+v %v", span, err)
	}
	expectationsMet(t, mock)
}

func TestIntegrationTimelineBucketsTheCarrierMilestones(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	base := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	w := funnelWorld{t: t, db: db, ws: uuid.NewString(), now: base}
	c := seedCampaign(t, NewCampaignRepository(db), w.ws, nil)
	at := func(d time.Duration) *time.Time { v := base.Add(d); return &v }
	m1 := w.message("rcs", "rcs", "read", "", "", "", at(0), at(time.Minute), at(2*time.Minute))
	m2 := w.message("rcs", "rcs", "accepted", "", "", "", at(time.Minute), nil, nil)
	w.message("sms", "sms", "delivered", m2, "", "", at(90*time.Second), at(90*time.Second), nil)
	run := uuid.NewString()
	if err := NewRecipientRepository(db).CreateMany(ctx, []carriercampaign.Recipient{
		{WorkspaceID: w.ws, CampaignID: c.ID, RunID: run, LeadID: uuid.NewString(), Status: carriercampaign.RecipientSent, MessageID: m1},
		{WorkspaceID: w.ws, CampaignID: c.ID, RunID: run, LeadID: uuid.NewString(), Status: carriercampaign.RecipientSent, MessageID: m2},
	}); err != nil {
		t.Fatal(err)
	}
	reader := NewTimelineReader(db)
	span, err := reader.Span(c.ID)
	if err != nil || !span.First.Equal(base) || !span.Last.Equal(base.Add(2*time.Minute)) {
		t.Fatalf("span %+v %v", span, err)
	}
	points, err := reader.Count(c.ID, campaign.TimelinePlan{Bucket: campaign.TimelineMinute, From: base, To: base.Add(3 * time.Minute), Location: time.UTC})
	if err != nil {
		t.Fatal(err)
	}
	want := []campaign.TimelinePoint{{At: base, Sent: 1}, {At: base.Add(time.Minute), Sent: 1, Delivered: 1}, {At: base.Add(2 * time.Minute), Read: 1}}
	if len(points) != len(want) {
		t.Fatalf("points %+v", points)
	}
	for i := range want {
		if !points[i].At.Equal(want[i].At) || points[i].Sent != want[i].Sent || points[i].Delivered != want[i].Delivered || points[i].Read != want[i].Read {
			t.Fatalf("point %d = %+v, want %+v", i, points[i], want[i])
		}
	}
}

package carriercampaign_repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/shared"
	"vozko/domain/sms"
	"vozko/infra/database/schema"
	ccuc "vozko/usecases/carriercampaign"
)

var (
	factColumns    = []string{"day", "channel", "kind", "total"}
	spendColumns   = []string{"day", "channel", "total"}
	carrierColumns = []string{"operator", "sent", "delivered", "fell_back"}
)

func TestOverviewAssemblesTheUseCaseResult(t *testing.T) {
	db, mock := newMockDB(t)
	from, to := testNow.Add(-48*time.Hour), testNow
	mock.ExpectQuery(`WITH facts AS`).
		WithArgs("ws1", "ws1", "ws1", "ws1", "America/Sao_Paulo", from, to).
		WillReturnRows(sqlmock.NewRows(factColumns).
			AddRow("2026-10-20", "rcs", "sent", 10).
			AddRow("2026-10-20", "rcs", "delivered", 8).
			AddRow("2026-10-20", "rcs", "rejected", 1).
			AddRow("2026-10-20", "rcs", "fell_back_sms", 2).
			AddRow("2026-10-20", "rcs", "targeted", 12).
			AddRow("2026-10-20", "sms", "sent", 4).
			AddRow("2026-10-21", "rcs", "taps", 3))
	mock.ExpectQuery(`FROM balance_transactions`).
		WithArgs("America/Sao_Paulo", "ws1", "rcs", "sms", from, to).
		WillReturnRows(sqlmock.NewRows(spendColumns).AddRow("2026-10-20", "sms", int64(700)).AddRow("2026-10-21", "rcs", int64(300)))
	mock.ExpectQuery(`FROM carrier_settings`).WillReturnRows(sqlmock.NewRows([]string{"attribution_days"}))
	mock.ExpectQuery(`FROM opportunities o`).WithArgs("ws1", "won", from, to).
		WillReturnRows(sqlmock.NewRows([]string{"id", "lead_id", "won_at", "value_cents"}).AddRow("o1", "l1", testNow.Add(-time.Hour), 500))
	mock.ExpectQuery(`AS delivered_at`).
		WillReturnRows(sqlmock.NewRows(touchColumns).AddRow("m1", "", "", 0, "l1", "sms", testNow.Add(-2*time.Hour)))
	mock.ExpectQuery(`FROM carrier_messages t WHERE t.workspace_id = \$1 AND t.parent_message_id IS NULL AND t.created_at >= \$2 AND t.created_at < \$3 GROUP BY 1`).
		WillReturnRows(sqlmock.NewRows(carrierColumns).AddRow("Vivo", 9, 8, 1))
	got, err := NewOverviewReader(db).Overview(context.Background(), "ws1", from, to, "America/Sao_Paulo", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []ccuc.OverviewDay{
		{Day: shared.Date{Year: 2026, Month: 10, Day: 20}, Channel: carriermessaging.ChannelRCS, Sent: 10, Delivered: 8, Failed: 1},
		{Day: shared.Date{Year: 2026, Month: 10, Day: 20}, Channel: carriermessaging.ChannelSMS, Sent: 4, SpendMicros: 700},
		{Day: shared.Date{Year: 2026, Month: 10, Day: 21}, Channel: carriermessaging.ChannelRCS, SpendMicros: 300},
	}
	if len(got.Daily) != len(want) {
		t.Fatalf("daily %+v", got.Daily)
	}
	for i := range want {
		if got.Daily[i] != want[i] {
			t.Fatalf("day %d = %+v, want %+v", i, got.Daily[i], want[i])
		}
	}
	wantTotals := carriercampaign.Funnel{Targeted: 12, Sent: 14, Delivered: 8, Rejected: 1, FellBackSMS: 2, Taps: 3, SpendMicros: 1000, Conversions: 1, ConversionValueMicros: 500 * centsToMicros}
	if got.Totals != wantTotals {
		t.Fatalf("totals\n got %+v\nwant %+v", got.Totals, wantTotals)
	}
	wantChannels := []ccuc.ChannelTotals{{Channel: carriermessaging.ChannelRCS, SpendMicros: 300}, {Channel: carriermessaging.ChannelSMS, SpendMicros: 700, Conversions: 1}}
	if len(got.Channels) != 2 || got.Channels[0] != wantChannels[0] || got.Channels[1] != wantChannels[1] {
		t.Fatalf("channels %+v", got.Channels)
	}
	if len(got.Carriers) != 1 || got.Carriers[0].Operator != "Vivo" {
		t.Fatalf("carriers %+v", got.Carriers)
	}
	expectationsMet(t, mock)
}

func TestOverviewFiltersOneChannel(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`WITH facts AS`).
		WillReturnRows(sqlmock.NewRows(factColumns).AddRow("2026-10-20", "rcs", "sent", 1).AddRow("2026-10-20", "sms", "sent", 1))
	mock.ExpectQuery(`FROM balance_transactions`).WithArgs("UTC", "ws1", "sms", testNow.Add(-time.Hour), testNow).
		WillReturnRows(sqlmock.NewRows(spendColumns))
	mock.ExpectQuery(`FROM carrier_settings`).WillReturnRows(sqlmock.NewRows([]string{"attribution_days"}))
	mock.ExpectQuery(`FROM opportunities o`).WillReturnRows(sqlmock.NewRows([]string{"id", "lead_id", "won_at", "value_cents"}))
	mock.ExpectQuery(`FROM carrier_messages t WHERE .* AND t.channel = \$4`).WithArgs("ws1", testNow.Add(-time.Hour), testNow, "sms").
		WillReturnRows(sqlmock.NewRows(carrierColumns))
	got, err := NewOverviewReader(db).Overview(context.Background(), "ws1", testNow.Add(-time.Hour), testNow, "UTC", carriermessaging.ChannelSMS)
	if err != nil || len(got.Daily) != 1 || got.Daily[0].Channel != carriermessaging.ChannelSMS || got.Totals.Sent != 1 ||
		len(got.Channels) != 1 || got.Channels[0].Channel != carriermessaging.ChannelSMS {
		t.Fatalf("got %+v %v", got, err)
	}
	expectationsMet(t, mock)
}

func TestOverviewFailsClosed(t *testing.T) {
	cases := []struct {
		name    string
		tz      string
		from    time.Time
		channel carriermessaging.Channel
		setup   func(sqlmock.Sqlmock)
		want    error
	}{
		{"unknown time zone", "Mars/Olympus", testNow.Add(-time.Hour), "", func(sqlmock.Sqlmock) {}, errOverviewQuery},
		{"inverted range", "UTC", testNow.Add(time.Hour), "", func(sqlmock.Sqlmock) {}, errOverviewQuery},
		{"unknown channel", "UTC", testNow.Add(-time.Hour), "fax", func(sqlmock.Sqlmock) {}, errUnknownStoredValue},
		{"fact without a channel", "UTC", testNow.Add(-time.Hour), "", func(m sqlmock.Sqlmock) {
			m.ExpectQuery(`WITH facts AS`).WillReturnRows(sqlmock.NewRows(factColumns).AddRow("2026-10-20", nil, "replies", 1))
			m.ExpectQuery(`FROM balance_transactions`).WillReturnRows(sqlmock.NewRows(spendColumns))
		}, errUnknownStoredValue},
		{"unknown fact kind", "UTC", testNow.Add(-time.Hour), "", func(m sqlmock.Sqlmock) {
			m.ExpectQuery(`WITH facts AS`).WillReturnRows(sqlmock.NewRows(factColumns).AddRow("2026-10-20", "rcs", "likes", 1))
			m.ExpectQuery(`FROM balance_transactions`).WillReturnRows(sqlmock.NewRows(spendColumns))
		}, errUnknownStoredValue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			tc.setup(mock)
			if _, err := NewOverviewReader(db).Overview(context.Background(), "ws1", tc.from, testNow, tc.tz, tc.channel); !errors.Is(err, tc.want) {
				t.Fatalf("got %v", err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestIntegrationOverviewBucketsByWorkspaceDayAndChannel(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	at := func(s string) *time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return &v
	}
	w := funnelWorld{t: t, db: db, ws: uuid.NewString(), now: *at("2026-10-20T12:00:00Z")}
	stamp := func(id, column string, value *time.Time) {
		if err := db.Exec("UPDATE carrier_messages SET "+column+" = ? WHERE id = ?", value, id).Error; err != nil {
			t.Fatal(err)
		}
	}
	read := w.message("rcs", "rcs", "read", "", "Vivo", "", at("2026-10-20T12:00:30Z"), at("2026-10-20T12:01:00Z"), at("2026-10-20T12:05:00Z"))
	failed := w.message("rcs", "rcs", "failed", "", "Claro", "", nil, nil, nil)
	stamp(failed, "created_at", at("2026-10-20T02:00:00Z"))
	stamp(failed, "failed_at", at("2026-10-20T02:30:00Z"))
	whatsapp := w.message("rcs", "whatsapp", "failed", failed, "", "", nil, nil, nil)
	stamp(whatsapp, "created_at", at("2026-10-20T02:31:00Z"))
	child := w.message("sms", "sms", "delivered", whatsapp, "", "", nil, at("2026-10-20T03:11:00Z"), nil)
	stamp(child, "created_at", at("2026-10-20T03:10:00Z"))
	smsTop := w.message("sms", "sms", "accepted", "", "", "", nil, nil, nil)
	stamp(smsTop, "created_at", at("2026-10-21T15:00:00Z"))
	old := w.message("rcs", "rcs", "accepted", "", "", "", nil, nil, nil)
	stamp(old, "created_at", at("2026-10-10T15:00:00Z"))
	route := uuid.NewString()
	if err := db.Create(&schema.SMSRoute{ID: route, WorkspaceID: w.ws, BrandPrefix: "Loja", Kind: string(sms.RouteShared), Status: string(sms.RouteActive), CreatedAt: w.now, UpdatedAt: w.now}).Error; err != nil {
		t.Fatal(err)
	}
	w.now = *at("2026-10-20T13:00:00Z")
	minus1, zero := -1, 0
	w.event(read, "tap", "t1", &minus1, &zero)
	if err := db.Exec(`INSERT INTO carrier_message_events (id, workspace_id, sender_id, phone, kind, label, dedup_key, occurred_at, created_at, updated_at)
		VALUES (?, ?, ?, '5511999990030', 'inbound', '', 'i1', ?, now(), now())`, uuid.NewString(), w.ws, route, *at("2026-10-21T16:00:00Z")).Error; err != nil {
		t.Fatal(err)
	}
	w.now = *at("2026-10-20T12:00:00Z")
	w.ledger("debit", 500, false, "carrier:rcs_basic:a")
	w.ledger("credit", 200, true, "refund:carrier:rcs_basic:a")
	if err := db.Exec(`INSERT INTO balance_transactions (id, workspace_id, type, amount, service_type, reference_id, is_refund, created_at) VALUES (?, ?, 'debit', 300, 'sms', 'carrier:sms_segment:b', false, ?)`,
		uuid.NewString(), w.ws, *at("2026-10-21T15:00:00Z")).Error; err != nil {
		t.Fatal(err)
	}
	lead := uuid.NewString()
	if err := db.Exec("UPDATE carrier_messages SET lead_id = ? WHERE id IN (?, ?)", lead, read, failed).Error; err != nil {
		t.Fatal(err)
	}
	w.won(lead, *at("2026-10-21T10:00:00Z"), 400, "won")

	got, err := NewOverviewReader(db).Overview(ctx, w.ws, *at("2026-10-19T03:00:00Z"), *at("2026-10-22T03:00:00Z"), "America/Sao_Paulo", "")
	if err != nil {
		t.Fatal(err)
	}
	day := func(d int) shared.Date { return shared.Date{Year: 2026, Month: 10, Day: d} }
	want := []ccuc.OverviewDay{
		{Day: day(19), Channel: carriermessaging.ChannelRCS, Sent: 1, Failed: 1},
		{Day: day(20), Channel: carriermessaging.ChannelRCS, Sent: 1, Delivered: 1, Read: 1, SpendMicros: 300},
		{Day: day(21), Channel: carriermessaging.ChannelSMS, Sent: 1, SpendMicros: 300},
	}
	if len(got.Daily) != len(want) {
		t.Fatalf("daily %+v", got.Daily)
	}
	for i := range want {
		if got.Daily[i] != want[i] {
			t.Fatalf("day %d = %+v, want %+v", i, got.Daily[i], want[i])
		}
	}
	if got.Totals.FellBackWhatsApp != 1 || got.Totals.FellBackSMS != 1 || got.Totals.Taps != 1 || got.Totals.Replies != 1 || got.Totals.Accepted != 1 ||
		got.Totals.SpendMicros != 600 || got.Totals.Conversions != 1 || got.Totals.ConversionValueMicros != 400*centsToMicros {
		t.Fatalf("totals %+v", got.Totals)
	}
	if len(got.Channels) != 2 || got.Channels[0].Conversions != 1 || got.Channels[0].SpendMicros != 300 || got.Channels[1].SpendMicros != 300 {
		t.Fatalf("channels %+v", got.Channels)
	}
	if len(got.Carriers) != 3 {
		t.Fatalf("carriers %+v", got.Carriers)
	}
}

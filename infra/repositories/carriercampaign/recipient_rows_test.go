package carriercampaign_repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/shared"
	ccuc "vozko/usecases/carriercampaign"
)

func TestRecipientRowsJoinTheLeadAndTheMessage(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM carrier_campaign_recipients r WHERE r.workspace_id = \$1 AND r.campaign_id = \$2 AND r.run_id = \$3 AND r.status = \$4`).
		WithArgs("ws1", campaignID, runID, "sent").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(41))
	mock.ExpectQuery(`SELECT r\.\*, COALESCE\(l\.name, ''\) AS lead_name, COALESCE\(m\.status, ''\) AS message_status FROM carrier_campaign_recipients r LEFT JOIN leads l ON l\.id = r\.lead_id LEFT JOIN carrier_messages m ON m\.id = r\.message_id WHERE .* ORDER BY r\.created_at ASC, r\.id ASC LIMIT \$5 OFFSET \$6`).
		WithArgs("ws1", campaignID, runID, "sent", 20, 20).
		WillReturnRows(sqlmock.NewRows(append(recipientColumns, "lead_name", "message_status")).
			AddRow(recipientID, "ws1", campaignID, runID, "l1", "5511999990000", 0, "sent", "", "m1", "", testNow, testNow, testNow, "Ana", "delivered"))
	page, err := NewRecipientRowReader(db).ListRecipientRows(context.Background(), ccuc.RecipientQuery{
		WorkspaceID: "ws1", CampaignID: campaignID, RunID: runID, Status: carriercampaign.RecipientSent, Pagination: shared.Pagination{Page: 2, PageSize: 20},
	})
	if err != nil || page.TotalItems != 41 || len(page.Items) != 1 {
		t.Fatalf("got %+v %v", page, err)
	}
	row := page.Items[0]
	if row.LeadName != "Ana" || row.MessageStatus != carriermessaging.StatusDelivered || row.Recipient.MessageID != "m1" || row.Recipient.Status != carriercampaign.RecipientSent {
		t.Fatalf("row %+v", row)
	}
	expectationsMet(t, mock)
}

func TestRecipientRowsFailClosed(t *testing.T) {
	cases := []struct {
		name  string
		q     ccuc.RecipientQuery
		setup func(sqlmock.Sqlmock)
		want  error
	}{
		{"malformed campaign", ccuc.RecipientQuery{WorkspaceID: "ws1", CampaignID: "x"}, func(sqlmock.Sqlmock) {}, carriercampaign.ErrCampaignNotFound},
		{"malformed run", ccuc.RecipientQuery{WorkspaceID: "ws1", CampaignID: campaignID, RunID: "x"}, func(sqlmock.Sqlmock) {}, carriercampaign.ErrRunNotFound},
		{"unknown status filter", ccuc.RecipientQuery{WorkspaceID: "ws1", CampaignID: campaignID, Status: "queued"}, func(sqlmock.Sqlmock) {}, errUnknownStoredValue},
		{"unknown message status", ccuc.RecipientQuery{WorkspaceID: "ws1", CampaignID: campaignID}, func(m sqlmock.Sqlmock) {
			m.ExpectQuery(`SELECT count`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
			m.ExpectQuery(`SELECT r\.\*`).WillReturnRows(sqlmock.NewRows(append(recipientColumns, "lead_name", "message_status")).
				AddRow(recipientID, "ws1", campaignID, runID, "l1", "", 0, "sent", "", "m1", "", testNow, testNow, testNow, "", "sent"))
		}, errUnknownStoredValue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			tc.setup(mock)
			if _, err := NewRecipientRowReader(db).ListRecipientRows(context.Background(), tc.q); !errors.Is(err, tc.want) {
				t.Fatalf("got %v", err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestIntegrationRecipientRows(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	w := funnelWorld{t: t, db: db, ws: uuid.NewString(), now: testNow}
	c := seedCampaign(t, NewCampaignRepository(db), w.ws, nil)
	lead := uuid.NewString()
	if err := db.Exec(`INSERT INTO leads (id, workspace_id, name) VALUES (?, ?, ?)`, lead, w.ws, "Ana").Error; err != nil {
		t.Fatal(err)
	}
	m := w.message("rcs", "rcs", "read", "", "", "", nil, nil, nil)
	run := uuid.NewString()
	if err := NewRecipientRepository(db).CreateMany(ctx, []carriercampaign.Recipient{
		{WorkspaceID: w.ws, CampaignID: c.ID, RunID: run, LeadID: lead, Status: carriercampaign.RecipientSent, MessageID: m},
		{WorkspaceID: w.ws, CampaignID: c.ID, RunID: run, LeadID: uuid.NewString(), Status: carriercampaign.RecipientPending},
		{WorkspaceID: w.ws, CampaignID: c.ID, RunID: uuid.NewString(), LeadID: lead, Status: carriercampaign.RecipientPending},
	}); err != nil {
		t.Fatal(err)
	}
	page, err := NewRecipientRowReader(db).ListRecipientRows(ctx, ccuc.RecipientQuery{WorkspaceID: w.ws, CampaignID: c.ID, RunID: run, Pagination: shared.Pagination{Page: 1, PageSize: 10}})
	if err != nil || page.TotalItems != 2 || len(page.Items) != 2 {
		t.Fatalf("page %+v %v", page, err)
	}
	var sent ccuc.RecipientRow
	for _, row := range page.Items {
		if row.Recipient.Status == carriercampaign.RecipientSent {
			sent = row
		}
	}
	if sent.LeadName != "Ana" || sent.MessageStatus != carriermessaging.StatusRead {
		t.Fatalf("sent row %+v", sent)
	}
	all, err := NewRecipientRowReader(db).ListRecipientRows(ctx, ccuc.RecipientQuery{WorkspaceID: w.ws, CampaignID: c.ID, Status: carriercampaign.RecipientPending})
	if err != nil || all.TotalItems != 2 {
		t.Fatalf("pending across runs %+v %v", all, err)
	}
}

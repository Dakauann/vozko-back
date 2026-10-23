package carriermessaging_repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"vozko/domain/carriermessaging"
	"vozko/domain/export"
)

func TestExportListsTheChannelConversationsWithTheirContacts(t *testing.T) {
	cases := []struct {
		channel carriermessaging.Channel
		table   string
	}{
		{carriermessaging.ChannelRCS, "rcs_conversations"},
		{carriermessaging.ChannelSMS, "sms_conversations"},
	}
	for _, tc := range cases {
		t.Run(string(tc.channel), func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectQuery(`FROM `+tc.table+` cv JOIN carrier_contacts ct ON ct\.id = cv\.contact_id AND ct\.deleted_at IS NULL WHERE \(cv\.workspace_id = \$1 AND cv\.deleted_at IS NULL\) AND cv\.sender_id = \$2 ORDER BY cv\.created_at DESC`).
				WithArgs("ws1", "s1").
				WillReturnRows(sqlmock.NewRows([]string{"entry_id", "status", "created_at", "updated_at", "phone", "display_name"}).
					AddRow("cv1", "new", testNow, testNow, "5511999990000", "").
					AddRow("cv2", "closed", testNow, testNow, "5511999990001", "Ana"))
			var got []export.ChannelEntry
			err := NewExportRepository(db, tc.channel).ListForExport(context.Background(), export.Scope{WorkspaceID: "ws1", ContainerID: "s1"}, func(e export.ChannelEntry) error {
				got = append(got, e)
				return nil
			})
			if err != nil || len(got) != 2 || got[0].Name != "5511999990000" || got[1].Name != "Ana" || got[1].Number != "5511999990001" || got[1].Status != "closed" {
				t.Fatalf("got %+v %v", got, err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestExportWithoutAWorkspaceListsNothing(t *testing.T) {
	db, mock := newMockDB(t)
	if err := NewExportRepository(db, carriermessaging.ChannelRCS).ListForExport(context.Background(), export.Scope{}, func(export.ChannelEntry) error {
		t.Fatal("nothing to emit")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}

func TestIntegrationExportKeepsChannelsApart(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	ws, sender := uuid.NewString(), uuid.NewString()
	contact, err := NewContactRepository(db).FindOrCreate(ctx, ws, "5511999990040", "Ana")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewSMSConversationRepository(db).FindOrCreate(ctx, ws, sender, contact.ID); err != nil {
		t.Fatal(err)
	}
	count := func(channel carriermessaging.Channel) int {
		n := 0
		if err := NewExportRepository(db, channel).ListForExport(ctx, export.Scope{WorkspaceID: ws}, func(e export.ChannelEntry) error {
			if e.Name != "Ana" || e.Number != "5511999990040" || e.CreatedAt == "" || e.Status != "new" {
				t.Fatalf("entry %+v", e)
			}
			if _, err := time.Parse(time.RFC3339, e.CreatedAt); err != nil {
				t.Fatal(err)
			}
			n++
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count(carriermessaging.ChannelSMS) != 1 || count(carriermessaging.ChannelRCS) != 0 {
		t.Fatal("an sms conversation must export only as sms")
	}
}

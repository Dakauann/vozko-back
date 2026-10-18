package unofficial_whatsapp_repository

import (
	"context"
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
	"vozko/infra/database/schema"
)

func groupIntegrationDB(t *testing.T) *groupRepository {
	t.Helper()
	db := postgresTestDB(t, []any{&schema.UnofficialWhatsAppGroup{}, &schema.UnofficialWhatsAppGroupParticipant{}}, []string{
		`CREATE UNIQUE INDEX ux_uw_group_instance_jid ON unofficial_whatsapp_groups (instance_id, jid) WHERE deleted_at IS NULL`,
		`CREATE UNIQUE INDEX ux_uw_group_participant ON unofficial_whatsapp_group_participants (group_id, jid)`,
	})
	return &groupRepository{db: db}
}

func TestParticipantSyncKeepsUnchangedRowsAndDropsOnlyWhoLeft(t *testing.T) {
	ctx := context.Background()
	repo := groupIntegrationDB(t)
	base := func(participants ...uw.GroupParticipant) *uw.Group {
		return &uw.Group{WorkspaceID: "11111111-1111-1111-1111-111111111111", InstanceID: "22222222-2222-2222-2222-222222222222",
			JID: "120363000000000001@g.us", Subject: "Ofertas", Participants: participants}
	}
	rita := uw.GroupParticipant{JID: "5511981124410@s.whatsapp.net", DisplayName: "Rita", Role: uw.GroupRoleMember}
	jorge := uw.GroupParticipant{JID: "5511992307781@s.whatsapp.net", DisplayName: "Jorge", Role: uw.GroupRoleMember}

	first := base(rita, jorge)
	if err := repo.Upsert(ctx, first); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	before, _ := repo.participantRows(ctx, first.ID)
	ritaID := ""
	for _, p := range before {
		if p.JID == rita.JID {
			ritaID = p.ID
		}
	}

	jorge.Role = uw.GroupRoleAdmin
	second := base(rita, jorge)
	if err := repo.Upsert(ctx, second); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	third := base(jorge)
	if err := repo.Upsert(ctx, third); err != nil {
		t.Fatalf("third sync: %v", err)
	}

	after, err := repo.participantRows(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].JID != jorge.JID || after[0].Role != string(uw.GroupRoleAdmin) {
		t.Fatalf("participants = %+v, want only Jorge promoted to admin", after)
	}
	if ritaID == "" {
		t.Fatal("rita had no row after the first sync")
	}

	again := base(jorge)
	if err := repo.Upsert(ctx, again); err != nil {
		t.Fatal(err)
	}
	final, _ := repo.participantRows(ctx, first.ID)
	if len(final) != 1 || final[0].ID != after[0].ID {
		t.Fatalf("an unchanged participant was rewritten: %s became %s", after[0].ID, final[0].ID)
	}
}

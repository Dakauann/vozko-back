package database

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"vozko/infra/database/schema"
)

func TestInstagramRulesAndPrivateRepliesMoveToTheSharedTablesOnce(t *testing.T) {
	tx := repairTx(t)
	if err := tx.AutoMigrate(&schema.InstagramCommentRule{}, &schema.InstagramPrivateReply{}, &schema.CommentRule{}, &schema.CommentPrivateReply{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	live := schema.InstagramCommentRule{ID: uuid.NewString(), WorkspaceID: uuid.NewString(), IGAccountID: uuid.NewString(),
		Name: "Promo", Enabled: true, IGMediaID: "m-1", Match: "contains", Keywords: pq.StringArray{"preço"}, Actions: pq.StringArray{"hide"}, Priority: 2}
	gone := schema.InstagramCommentRule{ID: uuid.NewString(), WorkspaceID: live.WorkspaceID, IGAccountID: live.IGAccountID,
		Name: "Old", Match: "any", Actions: pq.StringArray{"hide"}}
	for _, row := range []*schema.InstagramCommentRule{&live, &gone} {
		if err := tx.Create(row).Error; err != nil {
			t.Fatalf("rule: %v", err)
		}
	}
	if err := tx.Delete(&schema.InstagramCommentRule{}, "id = ?", gone.ID).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	recipient := "igsid-1"
	if err := tx.Create(&schema.InstagramPrivateReply{IGCommentID: "c-1", IGAccountID: live.IGAccountID, Status: "SENT", RecipientIGSID: &recipient}).Error; err != nil {
		t.Fatalf("reply: %v", err)
	}

	for range 2 {
		if err := copyInstagramCommentRules(tx); err != nil {
			t.Fatalf("rules repair: %v", err)
		}
		if err := copyInstagramPrivateReplies(tx); err != nil {
			t.Fatalf("replies repair: %v", err)
		}
	}

	var copied schema.CommentRule
	if err := tx.First(&copied, "id = ?", live.ID).Error; err != nil {
		t.Fatalf("live rule not copied: %v", err)
	}
	if copied.Source != "instagram" || copied.AccountID != live.IGAccountID || copied.ContainerID != "m-1" || copied.Priority != 2 || len(copied.Keywords) != 1 {
		t.Fatalf("copied = %+v", copied)
	}
	var deleted int64
	tx.Model(&schema.CommentRule{}).Where("id = ?", gone.ID).Count(&deleted)
	if deleted != 0 {
		t.Fatal("a deleted instagram rule came back")
	}
	var total int64
	tx.Unscoped().Model(&schema.CommentRule{}).Count(&total)
	if total != 2 {
		t.Fatalf("rules copied %d times", total)
	}
	var reply schema.CommentPrivateReply
	if err := tx.First(&reply, "source = ? AND comment_id = ?", "instagram", "c-1").Error; err != nil || reply.Status != "SENT" || reply.RecipientRef == nil || *reply.RecipientRef != "igsid-1" {
		t.Fatalf("reply = %+v, %v", reply, err)
	}
}

func TestCommentAutomationRepairsAreRegistered(t *testing.T) {
	for _, name := range []string{"ca_copy_instagram_comment_rules", "ca_copy_instagram_private_replies"} {
		if !slices.Contains(repairNames(), name) {
			t.Errorf("%s is not run at boot", name)
		}
	}
}

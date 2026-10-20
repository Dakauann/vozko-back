package commentautomation_repository

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	ca "vozko/domain/commentautomation"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestAccountPurgeStatementsBindEveryPlaceholder(t *testing.T) {
	want := []int{3, 2, 2}
	for i, stmt := range accountPurgeSQL {
		if got := strings.Count(stmt, "?"); got != want[i] {
			t.Errorf("statement %d binds %d placeholders, want %d: %s", i, got, want[i], stmt)
		}
	}
}

func TestPurgingAnAccountRemovesItsRulesAndTheirExecutionsOnly(t *testing.T) {
	db := repotest.IsolatedDB(t, "carp_test", &schema.CommentRule{}, &schema.CommentRuleExecution{}, &schema.CommentPrivateReply{})
	ctx := context.Background()
	rules := NewRuleRepository(db)
	claims := NewExecutionClaimRepository(db)
	ws, account, other := uuid.NewString(), uuid.NewString(), uuid.NewString()

	mine := &ca.Rule{WorkspaceID: ws, Source: shared.EntryTypeMercadoLivre, AccountID: account, Name: "a", Enabled: true, Match: ca.MatchAny, Actions: []ca.Action{ca.ActionDelete}}
	gone := &ca.Rule{WorkspaceID: ws, Source: shared.EntryTypeMercadoLivre, AccountID: account, Name: "b", Enabled: true, Match: ca.MatchAny, Actions: []ca.Action{ca.ActionDelete}}
	theirs := &ca.Rule{WorkspaceID: ws, Source: shared.EntryTypeMercadoLivre, AccountID: other, Name: "c", Enabled: true, Match: ca.MatchAny, Actions: []ca.Action{ca.ActionDelete}}
	for _, r := range []*ca.Rule{mine, gone, theirs} {
		if err := rules.Create(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := rules.Delete(ctx, ws, gone.ID); err != nil {
		t.Fatal(err)
	}
	for comment, rule := range map[string]string{"q1": mine.ID, "q2": gone.ID, "q3": theirs.ID} {
		if won, err := claims.Claim(ctx, shared.EntryTypeMercadoLivre, comment, rule); err != nil || !won {
			t.Fatalf("claim %s: %v %v", comment, won, err)
		}
	}

	if err := NewAccountPurger(db).PurgeAccount(ctx, shared.EntryTypeMercadoLivre, account); err != nil {
		t.Fatal(err)
	}
	var ruleRows, execRows int64
	db.Unscoped().Model(&schema.CommentRule{}).Where("account_id = ?", account).Count(&ruleRows)
	db.Model(&schema.CommentRuleExecution{}).Where("comment_id IN ?", []string{"q1", "q2"}).Count(&execRows)
	if ruleRows != 0 || execRows != 0 {
		t.Fatalf("left rules=%d executions=%d, soft deleted ones included", ruleRows, execRows)
	}
	var kept int64
	db.Model(&schema.CommentRuleExecution{}).Where("comment_id = ?", "q3").Count(&kept)
	if list, _ := rules.ListByAccount(ctx, ws, shared.EntryTypeMercadoLivre, other); len(list) != 1 || kept != 1 {
		t.Fatalf("another account lost its rules (%d) or executions (%d)", len(list), kept)
	}
}

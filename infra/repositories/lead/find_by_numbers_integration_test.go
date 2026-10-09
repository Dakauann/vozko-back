package lead

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/lead"
)

func TestContactsAreFoundByAnyFormatOfTheirNumbersInsideTheWorkspace(t *testing.T) {
	db := leadStoreDB(t)
	repo := &repository{db: db}
	ctx := context.Background()
	ws, other := uuid.NewString(), uuid.NewString()
	insert := func(workspaceID, number, name string, phones ...lead.ContactPhone) *lead.Lead {
		l := &lead.Lead{WorkspaceID: workspaceID, Number: number, Name: name, Source: lead.SourceManual, Phones: phones}
		if err := repo.Insert(ctx, l, nil); err != nil {
			t.Fatal(err)
		}
		return l
	}
	maria := insert(ws, "558494409684", "Maria")
	insert(ws, "5511999990000", "João")
	insert(other, "5511888880000", "Stranger")
	home := insert(ws, "", "Dona Rosa", lead.ContactPhone{Number: "558494409684", Label: lead.PhoneLandline})
	insert(other, "", "Elsewhere", lead.ContactPhone{Number: "558494409684", Label: lead.PhoneLandline})

	found, err := NewNumberDirectory(db).FindByNumbers(ws, []string{"5584994409684", "5511888880000", "100"})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, l := range found {
		ids[l.ID] = true
	}
	if len(found) != 2 || !ids[maria.ID] || !ids[home.ID] {
		t.Fatalf("found = %+v, want Maria by her identity and Dona Rosa by her contact phone, inside the workspace only", found)
	}
	if none, err := repo.FindByNumbers(ws, []string{"100"}); err != nil || len(none) != 0 {
		t.Fatalf("unmatchable numbers = %+v, %v", none, err)
	}
}

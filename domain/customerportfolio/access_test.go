package customerportfolio

import (
	"errors"
	"testing"
)

func TestAnOperatorPlacesOnlyIntoTheirOwnCarteira(t *testing.T) {
	operator := Caller{UserID: "u1", Uses: true}
	if err := operator.MayPlaceFor("u1"); err != nil {
		t.Fatalf("own carteira = %v", err)
	}
	if err := operator.MayPlaceFor("u2"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a colleague's carteira = %v, want ErrForbidden", err)
	}
	if err := (Caller{UserID: "u1"}).MayPlaceFor("u1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("without the carteira permission = %v, want ErrForbidden", err)
	}
}

func TestAManagerPlacesIntoAnyCarteira(t *testing.T) {
	manager := Caller{UserID: "m1", Uses: true, Manages: true}
	if err := manager.MayPlaceFor("u2"); err != nil {
		t.Fatalf("manager placing for a colleague = %v", err)
	}
}

func TestRemovingIsForTheHolderOrAManager(t *testing.T) {
	entry := Entry{HolderID: "u1"}
	if err := (Caller{UserID: "u1", Uses: true}).MayRemove(entry); err != nil {
		t.Fatalf("holder removing = %v", err)
	}
	if err := (Caller{UserID: "u2", Uses: true}).MayRemove(entry); !errors.Is(err, ErrForbidden) {
		t.Fatalf("colleague removing = %v, want ErrForbidden", err)
	}
	if err := (Caller{UserID: "u1"}).MayRemove(entry); !errors.Is(err, ErrForbidden) {
		t.Fatalf("holder who lost the permission = %v, want ErrForbidden", err)
	}
	if err := (Caller{UserID: "m1", Manages: true}).MayRemove(entry); err != nil {
		t.Fatalf("manager removing = %v", err)
	}
}

func TestOnlyManagersMoveACustomerBetweenCarteiras(t *testing.T) {
	if err := (Caller{UserID: "u1", Uses: true}).MayMove(); !errors.Is(err, ErrForbidden) {
		t.Fatalf("operator moving = %v, want ErrForbidden", err)
	}
	if err := (Caller{UserID: "m1", Manages: true}).MayMove(); err != nil {
		t.Fatalf("manager moving = %v", err)
	}
}

func TestSeeingCarteirasNeedsViewOrManage(t *testing.T) {
	if err := (Caller{UserID: "u1", Views: true}).MaySee(); err != nil {
		t.Fatalf("viewer = %v", err)
	}
	if err := (Caller{UserID: "m1", Manages: true}).MaySee(); err != nil {
		t.Fatalf("manager = %v", err)
	}
	if err := (Caller{UserID: "u1", Uses: true}).MaySee(); !errors.Is(err, ErrForbidden) {
		t.Fatalf("without view = %v, want ErrForbidden", err)
	}
}

func TestListingSomeoneElsesCarteiraOrEveryoneNeedsManage(t *testing.T) {
	if err := (Caller{UserID: "u1", Uses: true}).MayList("u1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("own list without view = %v, want ErrForbidden", err)
	}
	operator := Caller{UserID: "u1", Views: true}
	if err := operator.MayList("u1"); err != nil {
		t.Fatalf("own list = %v", err)
	}
	if err := operator.MayList("u2"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("colleague's list = %v", err)
	}
	if err := operator.MayList(""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("everyone's list = %v", err)
	}
	manager := Caller{UserID: "m1", Manages: true}
	if err := manager.MayList(""); err != nil {
		t.Fatalf("manager everyone = %v", err)
	}
}

package lead_usecase

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/lead"
	businessphone "vozko/domain/whatsapp/business_phone"
)

func blockerFixture(meta *fakeMeta) *WhatsAppBlocker {
	b, _ := NewWhatsAppBlocker(fakePhones{
		"bp-1":         {ID: "bp-1", OwnerWorkspaceID: cmdWorkspace, AccessToken: "token", MetaPhoneNumberID: "meta-1"},
		"bp-2":         {ID: "bp-2", OwnerWorkspaceID: "ws-2", AccessToken: "token", MetaPhoneNumberID: "meta-2"},
		"bp-platform":  {ID: "bp-platform", AccessToken: "token", MetaPhoneNumberID: "meta-p"},
		"bp-tokenless": {ID: "bp-tokenless", OwnerWorkspaceID: cmdWorkspace},
	}, fakeGrants{cmdWorkspace + ":bp-platform": true}, meta)
	return b
}

func TestTheWhatsAppBlockerResolvesAPhoneOnceAndAppliesManyNumbers(t *testing.T) {
	meta := &fakeMeta{}
	phone, err := blockerFixture(meta).Phone(cmdWorkspace, "bp-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := phone.Apply("5511987654321", true); err != nil {
		t.Fatal(err)
	}
	if err := phone.Apply("5511912345678", false); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(meta.blocked, []string{"5511987654321"}) || !reflect.DeepEqual(meta.unblocked, []string{"5511912345678"}) {
		t.Fatalf("meta = %+v", meta)
	}
	if _, err := blockerFixture(meta).Phone(cmdWorkspace, "bp-platform"); err != nil {
		t.Fatalf("a granted platform phone = %v", err)
	}
}

func TestTheWhatsAppBlockerRefusesPhonesTheWorkspaceCannotUse(t *testing.T) {
	for _, id := range []string{"bp-2", "bp-tokenless", "bp-missing", " "} {
		if _, err := blockerFixture(&fakeMeta{}).Phone(cmdWorkspace, id); !errors.Is(err, ErrBlockingPhoneUnavailable) {
			t.Errorf("phone %q = %v", id, err)
		}
	}
	if _, err := NewWhatsAppBlocker(nil, fakeGrants{}, &fakeMeta{}); err == nil {
		t.Fatal("a blocker without its ports must not be built")
	}
	var _ businessphone.AccessGrantReader = fakeGrants{}
}

func TestCheckOwnerAppliesTheSingleLeadOwnerRules(t *testing.T) {
	f := newCommandsFixture(t, allPermissions())
	if err := f.cmds.CheckOwner(operator(), " "+cmdUser+" "); err != nil {
		t.Fatalf("a visible member = %v", err)
	}
	if err := f.cmds.CheckOwner(operator(), otherUser); !errors.Is(err, lead.ErrLeadOwnerOutOfReach) {
		t.Fatalf("a member out of reach = %v", err)
	}
	if err := f.cmds.CheckOwner(operator(), ""); err != nil {
		t.Fatalf("removing the owner = %v", err)
	}
}

type brokenPhones struct{ err error }

func (b brokenPhones) FindByID(string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, b.err
}

type brokenGrants struct{}

func (brokenGrants) HasAccess(string, string) (bool, error) {
	return false, errors.New("connection reset")
}

func TestTheWhatsAppBlockerReportsAReadFailureAsAFailureNotAsAnUnavailablePhone(t *testing.T) {
	transient, _ := NewWhatsAppBlocker(brokenPhones{err: errors.New("connection reset")}, fakeGrants{}, &fakeMeta{})
	if _, err := transient.Phone(cmdWorkspace, "bp-1"); err == nil || errors.Is(err, ErrBlockingPhoneUnavailable) {
		t.Fatalf("a failed phone read = %v", err)
	}
	missing, _ := NewWhatsAppBlocker(brokenPhones{err: businessphone.ErrPhoneNumberNotFound}, fakeGrants{}, &fakeMeta{})
	if _, err := missing.Phone(cmdWorkspace, "bp-1"); !errors.Is(err, ErrBlockingPhoneUnavailable) {
		t.Fatalf("a missing phone = %v", err)
	}
	grants, _ := NewWhatsAppBlocker(fakePhones{"bp-platform": {ID: "bp-platform", AccessToken: "t", MetaPhoneNumberID: "m"}}, brokenGrants{}, &fakeMeta{})
	if _, err := grants.Phone(cmdWorkspace, "bp-platform"); err == nil || errors.Is(err, ErrBlockingPhoneUnavailable) {
		t.Fatalf("a failed grant read = %v", err)
	}
}

func TestOwnerReachStandsAloneFromTheCommands(t *testing.T) {
	f := newCommandsFixture(t, allPermissions())
	reach, err := NewOwnerReach(f.cmds.deps.Owners, f.cmds.deps.Visibility)
	if err != nil {
		t.Fatal(err)
	}
	if err := reach.CheckOwner(operator(), otherUser); !errors.Is(err, lead.ErrLeadOwnerOutOfReach) {
		t.Fatalf("a member out of reach = %v", err)
	}
	if err := reach.CheckOwner(operator(), cmdUser); err != nil {
		t.Fatalf("a visible member = %v", err)
	}
	if _, err := NewOwnerReach(nil, f.cmds.deps.Visibility); err == nil {
		t.Fatal("an owner reach without its ports must not be built")
	}
}

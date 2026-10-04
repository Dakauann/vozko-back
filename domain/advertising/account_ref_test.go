package advertising

import (
	"errors"
	"testing"
)

func refAccounts() []*AdAccount {
	return []*AdAccount{
		{ID: "4e4e9eee-d65a-4ecc-8c67-2cfa5deff426", MetaAccountID: "1628610989003756", Name: "Vozko CRM"},
		{ID: "4297acdd-751e-46f6-9c97-7865a4c5905b", MetaAccountID: "1762972444913096", Name: "Vozko CRM BRL"},
	}
}

func TestAnAccountIsFoundByOurIdMetasIdOrItsName(t *testing.T) {
	cases := []string{"4297acdd-751e-46f6-9c97-7865a4c5905b", "1762972444913096", "act_1762972444913096", " vozko crm brl "}
	for _, ref := range cases {
		got, err := ResolveAccount(refAccounts(), ref)
		if err != nil || got.Name != "Vozko CRM BRL" {
			t.Fatalf("%q: %+v %v", ref, got, err)
		}
	}
}

func TestAnAccountIsNeverPickedWithoutAReference(t *testing.T) {
	if _, err := ResolveAccount(refAccounts()[:1], " "); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("the only account was picked without being named: %v", err)
	}
}

func TestAnUnknownOrRepeatedNameIsNeverGuessed(t *testing.T) {
	if _, err := ResolveAccount(refAccounts(), "Vozko"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("partial name matched: %v", err)
	}
	twins := append(refAccounts(), &AdAccount{ID: "x", MetaAccountID: "9", Name: "Vozko CRM"})
	if _, err := ResolveAccount(twins, "Vozko CRM"); !errors.Is(err, ErrAmbiguousAccount) {
		t.Fatalf("repeated name matched: %v", err)
	}
}

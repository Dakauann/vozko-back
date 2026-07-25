package branch

import "testing"

func TestComputeHA1_MatchesRFC2617Example(t *testing.T) {
	// RFC 2617 §3.5 canonical example: HA1 = MD5("Mufasa:testrealm@host.com:Circle Of Life").
	got := ComputeHA1("Mufasa", "testrealm@host.com", "Circle Of Life")
	want := "939e7578ed9e3c518a452acee763bce9"
	if got != want {
		t.Fatalf("ComputeHA1 = %q, want %q", got, want)
	}
}

func TestSetSecret_DerivesHA1FromNormalizedUser(t *testing.T) {
	b := NewBranch("id", "ws", "member", "user", "1001", "Front Desk")
	b.Validate() // normalizes sip_user to lowercase "1001"
	b.SetSecret("vozko", "s3cret")

	if b.Realm != "vozko" {
		t.Fatalf("realm = %q, want vozko", b.Realm)
	}
	want := ComputeHA1("1001", "vozko", "s3cret")
	if b.SecretHA1 != want {
		t.Fatalf("SecretHA1 = %q, want %q", b.SecretHA1, want)
	}
}

func TestGenerateSecret_UniqueAndNonEmpty(t *testing.T) {
	a, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	if a == "" || b == "" {
		t.Fatal("generated secret is empty")
	}
	if a == b {
		t.Fatal("two generated secrets collided")
	}
}

func TestValidate(t *testing.T) {
	base := func() *Branch { return NewBranch("id", "ws", "m", "u", "1001", "Desk") }

	if err := base().Validate(); err != nil {
		t.Fatalf("valid branch rejected: %v", err)
	}

	cases := map[string]func(*Branch){
		"empty workspace":   func(b *Branch) { b.WorkspaceID = "" },
		"empty member":      func(b *Branch) { b.MemberID = "" },
		"empty user":        func(b *Branch) { b.UserID = "" },
		"empty sip user":    func(b *Branch) { b.SIPUser = "" },
		"bad sip user char": func(b *Branch) { b.SIPUser = "10 01" },
		"sip user at":       func(b *Branch) { b.SIPUser = "1001@x" },
		"bad codec":         func(b *Branch) { b.Codecs = []CodecID{"opus"} },
		"too many contacts": func(b *Branch) { b.MaxContacts = 99 },
	}
	for name, mutate := range cases {
		b := base()
		mutate(b)
		if err := b.Validate(); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}

	// MaxContacts <= 0 is normalized to the default, so it stays valid.
	b := base()
	b.MaxContacts = 0
	if err := b.Validate(); err != nil {
		t.Errorf("MaxContacts 0 should normalize to default and validate, got %v", err)
	}
}

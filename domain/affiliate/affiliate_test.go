package affiliate

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeCode(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"single char rejected", "A", ""},
		{"lowercase to upper", "  myCode  ", "MYCODE"},
		{"spaces become dashes and collapsed", "my   code  brand", "MY-CODE-BRAND"},
		{"trimmed leading/trailing dashes", "---promo---", "PROMO"},
		{"special chars replaced", "promo!@#123", "PROMO-123"},
		{"keeps digits", "CODE123", "CODE123"},
		{"too long (>32)", strings.Repeat("A", 40), ""},
		{"starts with dash after trim ok", "-A-B-", "A-B"},
		{"only non-alnum", "!!!", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeCode(tt.in); got != tt.want {
				t.Fatalf("NormalizeCode(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestGenerateCodeFromBrand(t *testing.T) {
	if got := GenerateCodeFromBrand("Cool Brand"); got != "COOL-BRAND" {
		t.Fatalf("got %q", got)
	}
	if got := GenerateCodeFromBrand("!!!"); got != "" {
		t.Fatalf("expected empty for invalid brand, got %q", got)
	}
}

func TestValidateCommissionPct(t *testing.T) {
	if err := ValidateCommissionPct(-0.01); !errors.Is(err, ErrInvalidCommissionPct) {
		t.Fatalf("negative should fail, got %v", err)
	}
	if err := ValidateCommissionPct(MaxCommissionPct + 0.0001); !errors.Is(err, ErrInvalidCommissionPct) {
		t.Fatalf("over max should fail, got %v", err)
	}
	if err := ValidateCommissionPct(0); err != nil {
		t.Fatalf("zero should pass, got %v", err)
	}
	if err := ValidateCommissionPct(MaxCommissionPct); err != nil {
		t.Fatalf("max should pass, got %v", err)
	}
}

func TestAffiliateValidate(t *testing.T) {
	valid := func() *Affiliate {
		return &Affiliate{
			UserID:        "user-1",
			Code:          "PROMO-1",
			BrandName:     "My Brand",
			BrandLogoURL:  "https://cdn.example.com/logo.png",
			AsaasWalletID: "wallet-123",
			CommissionPct: 0.05,
		}
	}

	if err := valid().Validate(); err != nil {
		t.Fatalf("valid affiliate should pass, got %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(*Affiliate)
		wantErr error
	}{
		{"empty user", func(a *Affiliate) { a.UserID = "  " }, ErrAffiliateNotFound},
		{"invalid code", func(a *Affiliate) { a.Code = "bad code" }, ErrInvalidCode},
		{"empty code", func(a *Affiliate) { a.Code = "" }, ErrInvalidCode},
		{"short brand", func(a *Affiliate) { a.BrandName = "ab" }, ErrInvalidBrandName},
		{"long brand", func(a *Affiliate) { a.BrandName = strings.Repeat("x", 129) }, ErrInvalidBrandName},
		{"empty logo", func(a *Affiliate) { a.BrandLogoURL = "" }, ErrInvalidBrandLogoURL},
		{"non-http logo", func(a *Affiliate) { a.BrandLogoURL = "ftp://a" }, ErrInvalidBrandLogoURL},
		{"long logo", func(a *Affiliate) { a.BrandLogoURL = "https://" + strings.Repeat("a", 520) }, ErrInvalidBrandLogoURL},
		{"empty wallet", func(a *Affiliate) { a.AsaasWalletID = "" }, ErrInvalidAsaasWalletID},
		{"long wallet", func(a *Affiliate) { a.AsaasWalletID = strings.Repeat("w", 65) }, ErrInvalidAsaasWalletID},
		{"bad pct", func(a *Affiliate) { a.CommissionPct = 1.0 }, ErrInvalidCommissionPct},
		{"bad tier", func(a *Affiliate) { a.Tier = Tier("wholesaler") }, ErrInvalidTier},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := valid()
			c.mutate(a)
			err := a.Validate()
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("want %v, got %v", c.wantErr, err)
			}
		})
	}
}

func TestAffiliateValidate_TierDefaultsAndAccepted(t *testing.T) {
	base := func() *Affiliate {
		return &Affiliate{
			UserID:        "user-1",
			Code:          "PROMO-1",
			BrandName:     "My Brand",
			BrandLogoURL:  "https://cdn.example.com/logo.png",
			AsaasWalletID: "wallet-123",
			CommissionPct: 0.05,
		}
	}

	t.Run("empty tier defaults to affiliate", func(t *testing.T) {
		a := base()
		if err := a.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.Tier != TierAffiliate {
			t.Fatalf("want default %q, got %q", TierAffiliate, a.Tier)
		}
	})
	t.Run("reseller tier accepted", func(t *testing.T) {
		a := base()
		a.Tier = TierReseller
		if err := a.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.Tier != TierReseller {
			t.Fatalf("tier was mutated unexpectedly: %q", a.Tier)
		}
	})
}

func TestTierIsValid(t *testing.T) {
	cases := map[Tier]bool{
		TierAffiliate:    true,
		TierReseller:     true,
		Tier(""):         false,
		Tier("partner"):  false,
		Tier("Reseller"): false,
	}
	for in, want := range cases {
		if got := in.IsValid(); got != want {
			t.Fatalf("Tier(%q).IsValid() = %v, want %v", string(in), got, want)
		}
	}
}

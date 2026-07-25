package affiliate

import (
	"regexp"
	"strings"
	"time"
)

const MaxCommissionPct = 0.10
const DefaultCommissionPct = 0.05

type Tier string

const (
	TierAffiliate Tier = "affiliate"
	TierReseller  Tier = "reseller"
)

func (t Tier) IsValid() bool {
	return t == TierAffiliate || t == TierReseller
}

var (
	codeRegex      = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{1,30}[A-Z0-9]$`)
	nonAlnumRegex  = regexp.MustCompile(`[^A-Z0-9-]+`)
	multiDashRegex = regexp.MustCompile(`-+`)
)

type Affiliate struct {
	ID            string    `json:"id"`
	UserID        string    `json:"userId"`
	Code          string    `json:"code"`
	BrandName     string    `json:"brandName"`
	BrandLogoURL  string    `json:"brandLogoUrl"`
	AsaasWalletID string    `json:"asaasWalletId"`
	CommissionPct float64   `json:"commissionPct"`
	Tier          Tier      `json:"tier"`
	Active        bool      `json:"active"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type Referral struct {
	ID          string    `json:"id"`
	AffiliateID string    `json:"affiliateId"`
	WorkspaceID string    `json:"workspaceId"`
	ReferredAt  time.Time `json:"referredAt"`
}

type Earning struct {
	ID                 string    `json:"id"`
	AffiliateID        string    `json:"affiliateId"`
	InvoiceID          string    `json:"invoiceId"`
	WorkspaceID        string    `json:"workspaceId"`
	AmountMicros       int64     `json:"amountMicros"`
	ExchangeRateMicros int64     `json:"exchangeRateMicros"`
	Purpose            string    `json:"purpose"`
	Status             string    `json:"status"`
	CreatedAt          time.Time `json:"createdAt"`
}

type Stats struct {
	TotalReferrals     int64 `json:"totalReferrals"`
	TotalEarningMicros int64 `json:"totalEarningMicros"`
	MonthEarningMicros int64 `json:"monthEarningMicros"`
}

func NormalizeCode(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	s = nonAlnumRegex.ReplaceAllString(s, "-")
	s = multiDashRegex.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if !codeRegex.MatchString(s) {
		return ""
	}
	return s
}

func GenerateCodeFromBrand(brand string) string {
	code := NormalizeCode(brand)
	if code == "" {
		return ""
	}
	return code
}

func ValidateCommissionPct(pct float64) error {
	if pct < 0 || pct > MaxCommissionPct {
		return ErrInvalidCommissionPct
	}
	return nil
}

func (a *Affiliate) Validate() error {
	if strings.TrimSpace(a.UserID) == "" {
		return ErrAffiliateNotFound
	}
	if NormalizeCode(a.Code) != a.Code || a.Code == "" {
		return ErrInvalidCode
	}
	name := strings.TrimSpace(a.BrandName)
	if len(name) < 3 || len(name) > 128 {
		return ErrInvalidBrandName
	}
	logo := strings.TrimSpace(a.BrandLogoURL)
	if logo == "" || len(logo) > 512 {
		return ErrInvalidBrandLogoURL
	}
	if !strings.HasPrefix(logo, "http://") && !strings.HasPrefix(logo, "https://") {
		return ErrInvalidBrandLogoURL
	}
	if strings.TrimSpace(a.AsaasWalletID) == "" || len(a.AsaasWalletID) > 64 {
		return ErrInvalidAsaasWalletID
	}
	if err := ValidateCommissionPct(a.CommissionPct); err != nil {
		return err
	}

	if a.Tier == "" {
		a.Tier = TierAffiliate
	}
	if !a.Tier.IsValid() {
		return ErrInvalidTier
	}
	return nil
}

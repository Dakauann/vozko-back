package config

import (
	"context"
	"errors"
	"time"
)

var (
	ErrConfigNotFound                = errors.New("system configuration not found")
	ErrUnauthorized                  = errors.New("only administrators can modify system configuration")
	ErrInvalidWorkTimeSlot           = errors.New("work time start must be before work time end")
	ErrInvalidAffiliateCommissionPct = errors.New("affiliate commission percentage must be between 0 and " + "30%")
)

const (
	DefaultWorkTimeStart = "08:00"
	DefaultWorkTimeEnd   = "20:00"

	// DefaultMaxConcurrentCalls is the platform-wide ceiling on simultaneous
	// outbound/inbound telephony legs when no explicit value is configured.
	DefaultMaxConcurrentCalls = 30

	DefaultAffiliateCommissionPct = 0.05

	MaxAffiliateCommissionPct = 0.10
)

var BrazilianTimezone = mustLoadLocation("America/Sao_Paulo")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic("failed to load timezone: " + name)
	}
	return loc
}

type SystemConfig struct {
	ID                     string  `json:"id"`
	BaseSystemPrompt       string  `json:"baseSystemPrompt"`
	WorkTimeEnabled        bool    `json:"workTimeEnabled"`
	WorkTimeStart          string  `json:"workTimeStart"`
	WorkTimeEnd            string  `json:"workTimeEnd"`
	MaxConcurrentCalls     int     `json:"maxConcurrentCalls"`
	AffiliateCommissionPct float64 `json:"affiliateCommissionPct"`

	UpdatedBy string `json:"updatedBy"`
	UpdatedAt string `json:"updatedAt"`
}

func (c *SystemConfig) GetMaxConcurrentCalls() int {
	if c.MaxConcurrentCalls <= 0 {
		return DefaultMaxConcurrentCalls
	}
	return c.MaxConcurrentCalls
}

func (c *SystemConfig) GetWorkTimeStart() string {
	if c.WorkTimeStart == "" {
		return DefaultWorkTimeStart
	}
	return c.WorkTimeStart
}

func (c *SystemConfig) GetWorkTimeEnd() string {
	if c.WorkTimeEnd == "" {
		return DefaultWorkTimeEnd
	}
	return c.WorkTimeEnd
}

func (c *SystemConfig) GetAffiliateCommissionPct() float64 {
	if c.AffiliateCommissionPct <= 0 || c.AffiliateCommissionPct > MaxAffiliateCommissionPct {
		return DefaultAffiliateCommissionPct
	}
	return c.AffiliateCommissionPct
}

func (c *SystemConfig) IsWithinWorkHours() bool {
	if !c.WorkTimeEnabled {
		return true
	}

	now := time.Now().In(BrazilianTimezone)
	currentMinutes := now.Hour()*60 + now.Minute()

	startMinutes, err := parseTimeToMinutes(c.GetWorkTimeStart())
	if err != nil {
		return true
	}

	endMinutes, err := parseTimeToMinutes(c.GetWorkTimeEnd())
	if err != nil {
		return true
	}

	return currentMinutes >= startMinutes && currentMinutes < endMinutes
}

func isBusinessDay(t time.Time) bool {
	dayOfWeek := t.Weekday()

	return dayOfWeek != time.Sunday && dayOfWeek != time.Saturday
}

func parseTimeToMinutes(timeStr string) (int, error) {
	t, err := time.Parse("15:04", timeStr)
	if err != nil {
		return 0, err
	}
	return t.Hour()*60 + t.Minute(), nil
}

func ValidateWorkTime(start, end string) error {
	startMinutes, err := parseTimeToMinutes(start)
	if err != nil {
		return err
	}
	endMinutes, err := parseTimeToMinutes(end)
	if err != nil {
		return err
	}
	if startMinutes >= endMinutes {
		return ErrInvalidWorkTimeSlot
	}
	return nil
}

type SystemConfigRepository interface {
	Get(ctx context.Context) (*SystemConfig, error)
	Upsert(ctx context.Context, config *SystemConfig) error
}

type GetSystemConfigUseCase interface {
	Execute(ctx context.Context) (*SystemConfig, error)
}

type UpdateSystemConfigUseCase interface {
	Execute(ctx context.Context, userID string, userRole string, input UpdateSystemConfigInput) (*SystemConfig, error)
}

type UpdateSystemConfigInput struct {
	BaseSystemPrompt       *string  `json:"baseSystemPrompt,omitempty"`
	WorkTimeEnabled        *bool    `json:"workTimeEnabled,omitempty"`
	WorkTimeStart          *string  `json:"workTimeStart,omitempty"`
	WorkTimeEnd            *string  `json:"workTimeEnd,omitempty"`
	MaxConcurrentCalls     *int     `json:"maxConcurrentCalls,omitempty"`
	AffiliateCommissionPct *float64 `json:"affiliateCommissionPct,omitempty"`
}

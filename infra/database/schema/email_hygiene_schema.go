package schema

import "time"

type EmailBadAddress struct {
	AddressHash []byte    `gorm:"primaryKey;type:bytea"`
	Reason      string    `gorm:"type:varchar(16);not null"`
	FirstSeenAt time.Time `gorm:"type:timestamptz;not null"`
	LastSeenAt  time.Time `gorm:"type:timestamptz;not null"`
}

func (EmailBadAddress) TableName() string { return "email_bad_addresses" }

type EmailAddressActivity struct {
	WorkspaceID           string     `gorm:"primaryKey;type:uuid"`
	AddressHash           []byte     `gorm:"primaryKey;type:bytea"`
	FirstDeliveredAt      *time.Time `gorm:"type:timestamptz"`
	LastDeliveredAt       *time.Time `gorm:"type:timestamptz"`
	LastEngagedAt         *time.Time `gorm:"type:timestamptz;index:idx_email_address_activity_last_engaged"`
	SunsetSince           *time.Time `gorm:"type:timestamptz"`
	CampaignsSinceEngaged int        `gorm:"not null;default:0"`
	SoftStreak            int        `gorm:"not null;default:0"`
	SoftStreakStartedAt   *time.Time `gorm:"type:timestamptz"`
	LastOutcomeMessageID  string     `gorm:"type:varchar(64);not null;default:''"`
	TouchedAt             time.Time  `gorm:"type:timestamptz;not null;index:idx_email_address_activity_touched"`
}

func (EmailAddressActivity) TableName() string { return "email_address_activity" }

type EmailDomainCheck struct {
	Domain    string     `gorm:"primaryKey;type:varchar(253)"`
	State     string     `gorm:"type:varchar(16);not null"`
	Detail    string     `gorm:"type:varchar(16);not null;default:''"`
	CheckedAt *time.Time `gorm:"type:timestamptz"`
	ExpiresAt time.Time  `gorm:"type:timestamptz;not null"`
	DueAt     *time.Time `gorm:"type:timestamptz;index:idx_email_domain_checks_due,where:due_at IS NOT NULL"`
	Attempts  int        `gorm:"not null;default:0"`
	CreatedAt time.Time  `gorm:"type:timestamptz;not null"`
	UpdatedAt time.Time  `gorm:"type:timestamptz;not null;index:idx_email_domain_checks_updated"`
}

func (EmailDomainCheck) TableName() string { return "email_domain_checks" }

type EmailDisposableList struct {
	ID        string    `gorm:"primaryKey;type:uuid"`
	FetchedAt time.Time `gorm:"type:timestamptz;not null;index:idx_email_disposable_lists_fetched"`
	Domains   int       `gorm:"not null"`
	SHA256    string    `gorm:"column:sha256;type:varchar(64);not null"`
	Content   string    `gorm:"type:text;not null"`
}

func (EmailDisposableList) TableName() string { return "email_disposable_lists" }

type EmailRunOutcome struct {
	RunKind     string    `gorm:"primaryKey;type:varchar(16)"`
	RunID       string    `gorm:"primaryKey;type:varchar(64)"`
	Delivered   int64     `gorm:"not null;default:0"`
	HardBounces int64     `gorm:"not null;default:0"`
	Complaints  int64     `gorm:"not null;default:0"`
	UpdatedAt   time.Time `gorm:"type:timestamptz;not null"`
}

func (EmailRunOutcome) TableName() string { return "email_run_outcomes" }

func EmailHygieneModels() []any {
	return []any{
		&EmailBadAddress{},
		&EmailAddressActivity{},
		&EmailDomainCheck{},
		&EmailDisposableList{},
		&EmailRunOutcome{},
	}
}

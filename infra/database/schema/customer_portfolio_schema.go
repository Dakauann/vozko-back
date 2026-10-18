package schema

import "time"

type CustomerPortfolioEntry struct {
	ID          string       `gorm:"primaryKey;type:uuid"`
	WorkspaceID string       `gorm:"type:uuid;not null;index:idx_customer_portfolio_holder,priority:1;uniqueIndex:ux_customer_portfolio_lead,priority:1,where:lead_id IS NOT NULL;uniqueIndex:ux_customer_portfolio_contact,priority:1,where:contact_id IS NOT NULL"`
	HolderID    string       `gorm:"type:uuid;not null;index:idx_customer_portfolio_holder,priority:2"`
	LeadID      OptionalText `gorm:"type:uuid;uniqueIndex:ux_customer_portfolio_lead,priority:2,where:lead_id IS NOT NULL;check:chk_customer_portfolio_one_subject,(lead_id IS NULL) <> (contact_id IS NULL)"`
	ContactType OptionalText `gorm:"type:varchar(32);uniqueIndex:ux_customer_portfolio_contact,priority:2,where:contact_id IS NOT NULL"`
	ContactID   OptionalText `gorm:"type:uuid;uniqueIndex:ux_customer_portfolio_contact,priority:3,where:contact_id IS NOT NULL"`
	AddedBy     OptionalText `gorm:"type:uuid"`
	CreatedAt   time.Time    `gorm:"type:timestamptz;not null"`
	UpdatedAt   time.Time    `gorm:"type:timestamptz;not null"`
}

func (CustomerPortfolioEntry) TableName() string {
	return "customer_portfolio_entries"
}

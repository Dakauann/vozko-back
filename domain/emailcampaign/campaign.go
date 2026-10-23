package emailcampaign

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/campaignaudience"
	"vozko/domain/emaildesign"
	"vozko/domain/emailsending"
	"vozko/domain/selection"
)

const (
	MaxNameRunes      = 120
	MaxPreheaderRunes = 200
	utmChannel        = "email"
	fallbackUTMName   = "campanha"
)

var (
	ErrCampaignNotFound = errors.New("emailcampaign: campaign not found")
	ErrNotEditable      = errors.New("emailcampaign: only drafts and scheduled campaigns can be edited")
	ErrNameRequired     = errors.New("emailcampaign: a name is required")
	ErrNameTooLong      = errors.New("emailcampaign: the name is too long")
	ErrPreheaderTooLong = errors.New("emailcampaign: the preheader is too long")
	ErrAudienceInvalid  = errors.New("emailcampaign: the audience is invalid")
)

type HoldReason string

const (
	HoldNone              HoldReason = ""
	HoldDailyCap          HoldReason = "daily_cap"
	HoldAccountPaused     HoldReason = "account_paused"
	HoldSenderUnavailable HoldReason = "sender_unavailable"
	HoldBalanceShort      HoldReason = "balance_insufficient"
)

type Campaign struct {
	ID            string
	WorkspaceID   string
	Name          string
	Status        Status
	HoldReason    HoldReason
	SenderID      string
	Subject       string
	Preheader     string
	Document      emaildesign.Document
	Selection     *selection.Selection
	ListID        string
	MergePolicies []campaignaudience.Variable
	ABTest        *ABTest
	UTM           *emaildesign.UTM
	ScheduledAt   *time.Time
	StartedAt     *time.Time
	CompletedAt   *time.Time
	Settlement    *RunSettlement
	TestBatch     *TestBatch
	CreatedBy     string
	LaunchedBy    string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Draft struct {
	Name          string
	SenderID      string
	Subject       string
	Preheader     string
	Document      emaildesign.Document
	Selection     *selection.Selection
	ListID        string
	MergePolicies []campaignaudience.Variable
	ABTest        *ABTest
	UTM           *emaildesign.UTM
}

func (d Draft) Validate() error {
	name := strings.TrimSpace(d.Name)
	if name == "" {
		return ErrNameRequired
	}
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return ErrNameTooLong
	}
	if utf8.RuneCountInString(d.Preheader) > MaxPreheaderRunes {
		return ErrPreheaderTooLong
	}
	if d.Selection != nil {
		if strings.TrimSpace(d.ListID) != "" {
			return fmt.Errorf("%w: %w", ErrAudienceInvalid, campaignaudience.ErrSourceAmbiguous)
		}
		if err := d.Selection.Inferred().ValidateForCount(); err != nil {
			return fmt.Errorf("%w: %w", ErrAudienceInvalid, err)
		}
	}
	if err := campaignaudience.ValidateVariables(d.MergePolicies); err != nil {
		return fmt.Errorf("%w: %w", ErrAudienceInvalid, err)
	}
	if d.ABTest != nil {
		return d.ABTest.Validate()
	}
	return nil
}

func (c *Campaign) Apply(d Draft) {
	c.Name = strings.TrimSpace(d.Name)
	c.SenderID = strings.TrimSpace(d.SenderID)
	c.Subject = strings.TrimSpace(d.Subject)
	c.Preheader = strings.TrimSpace(d.Preheader)
	c.Document = d.Document
	if d.Selection != nil {
		inferred := d.Selection.Inferred()
		c.Selection = &inferred
	} else {
		c.Selection = nil
	}
	c.ListID = strings.TrimSpace(d.ListID)
	c.MergePolicies = campaignaudience.NormalizeVariables(d.MergePolicies)
	c.ABTest = d.ABTest
	c.UTM = d.UTM
}

func (c Campaign) SubjectFor(v Variant) string {
	if c.ABTest == nil {
		return c.Subject
	}
	switch v {
	case VariantB:
		return c.ABTest.VariantBSubject
	case VariantWinner:
		if c.ABTest.Winner == VariantB {
			return c.ABTest.VariantBSubject
		}
	}
	return c.Subject
}

func (c Campaign) TrackingUTM() emaildesign.UTM {
	utm := emaildesign.UTM{}
	if c.UTM != nil {
		utm = *c.UTM
	}
	utm.Source, utm.Medium = utmChannel, utmChannel
	if strings.TrimSpace(utm.Campaign) == "" {
		utm.Campaign = emailsending.SlugCandidate(c.Name)
	}
	if utm.Campaign == "" {
		utm.Campaign = fallbackUTMName
	}
	return utm
}

func (c Campaign) RenderDocument() emaildesign.Document {
	doc := c.Document
	if c.Preheader != "" {
		doc.Settings.Preheader = c.Preheader
	}
	return doc
}

func (c Campaign) TestWindowOpen(now time.Time) bool {
	return c.ABTest != nil && c.ABTest.Winner == "" && c.StartedAt != nil && now.Before(c.ABTest.DecideAt(*c.StartedAt))
}

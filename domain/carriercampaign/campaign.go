package carriercampaign

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/carriermessaging"
	"vozko/domain/customfield"
	"vozko/domain/selection"
	"vozko/domain/shared"
	"vozko/domain/sms"
)

const (
	MaxNameRunes = 120
	MinVariants  = 1
	MaxVariants  = 4
)

var (
	ErrCampaignNotFound     = errors.New("carriercampaign: campaign not found")
	ErrChannelInvalid       = errors.New("carriercampaign: a campaign is sent over rcs or sms")
	ErrNameRequired         = errors.New("carriercampaign: a name is required")
	ErrNameTooLong          = errors.New("carriercampaign: the name is too long")
	ErrSenderRequired       = errors.New("carriercampaign: a sender is required")
	ErrKindInvalid          = errors.New("carriercampaign: a campaign is marketing or transactional")
	ErrVariantsRequired     = errors.New("carriercampaign: a campaign needs a message")
	ErrTooManyVariants      = errors.New("carriercampaign: an A/B test has at most 4 variants")
	ErrVariantNameRequired  = errors.New("carriercampaign: every variant of an A/B test needs a name")
	ErrVariantNameDuplicate = errors.New("carriercampaign: two variants share a name")
	ErrScheduleInPast       = errors.New("carriercampaign: the schedule must be in the future")
	ErrScheduleMissing      = errors.New("carriercampaign: a scheduled campaign needs a start time")
	ErrRecurrenceEnded      = errors.New("carriercampaign: the recurrence has no occurrence left")
	ErrAudienceInvalid      = errors.New("carriercampaign: the audience is invalid")
	ErrAudienceRequired     = errors.New("carriercampaign: the campaign needs an audience")
)

type Kind string

const (
	KindMarketing     Kind = "marketing"
	KindTransactional Kind = "transactional"
)

func (k Kind) Valid() bool {
	return k == KindMarketing || k == KindTransactional
}

func (k Kind) ObeysMarketingRules() bool {
	return k != KindTransactional
}

type Campaign struct {
	ID          string
	WorkspaceID string
	Name        string
	SenderID    string
	Channel     carriermessaging.Channel
	Status      Status
	Kind        Kind
	Variants    []Variant
	Fallback    Fallback
	Audience    selection.Selection
	ScheduledAt *time.Time
	Recurrence  *Recurrence
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (c Campaign) ABTest() bool {
	return len(c.Variants) > 1
}

func (c Campaign) VariantFor(recipientKey string) int {
	return PickVariant(recipientKey, len(c.Variants))
}

func PickVariant(recipientKey string, variants int) int {
	return shared.VariantIndexFor(recipientKey, variants)
}

func (c Campaign) HasAudience() bool {
	return c.Audience.Inferred().Mode != ""
}

func (c Campaign) Validate(now time.Time, fields []*customfield.Definition) error {
	name := strings.TrimSpace(c.Name)
	switch {
	case name == "":
		return ErrNameRequired
	case utf8.RuneCountInString(name) > MaxNameRunes:
		return ErrNameTooLong
	case !c.Channel.Valid():
		return ErrChannelInvalid
	case blank(c.SenderID):
		return ErrSenderRequired
	case !c.Kind.Valid():
		return ErrKindInvalid
	case !c.Status.Valid():
		return ErrStatusInvalid
	case c.Channel != carriermessaging.ChannelRCS && c.Fallback.Any():
		return ErrFallbackNotAllowed
	}
	if err := c.validateVariants(fields); err != nil {
		return err
	}
	if err := c.Fallback.Validate(fields); err != nil {
		return err
	}
	if c.HasAudience() {
		if err := c.Audience.Inferred().ValidateForCount(); err != nil {
			return fmt.Errorf("%w: %w", ErrAudienceInvalid, err)
		}
	}
	return c.validateSchedule(now)
}

func (c Campaign) SMSRouteID() string {
	switch {
	case c.Channel == carriermessaging.ChannelSMS:
		return c.SenderID
	case c.Channel == carriermessaging.ChannelRCS && c.Fallback.SMS != nil:
		return c.Fallback.SMS.RouteID
	}
	return ""
}

func (c Campaign) smsTemplates() []string {
	if c.Channel == carriermessaging.ChannelSMS {
		out := make([]string, 0, len(c.Variants))
		for _, v := range c.Variants {
			out = append(out, v.Text)
		}
		return out
	}
	if c.Fallback.SMS != nil {
		return []string{c.Fallback.SMS.Text}
	}
	return nil
}

func (c Campaign) checkSMSRoute(route *sms.Route) error {
	id := c.SMSRouteID()
	switch {
	case id == "" && route == nil:
		return nil
	case route == nil:
		return ErrSMSRouteRequired
	case id == "" || route.ID != id:
		return ErrSMSRouteMismatch
	}
	return route.Validate()
}

func (c Campaign) ValidateSMSRoute(route sms.Route) error {
	if err := c.checkSMSRoute(&route); err != nil {
		return err
	}
	for _, template := range c.smsTemplates() {
		if err := sms.Validate(route.Compose(template)); err != nil {
			return err
		}
	}
	return nil
}

func (c Campaign) ReadyToLaunch(now time.Time, fields []*customfield.Definition) error {
	if err := c.Validate(now, fields); err != nil {
		return err
	}
	if !c.HasAudience() {
		return ErrAudienceRequired
	}
	return nil
}

func (c Campaign) validateVariants(fields []*customfield.Definition) error {
	switch {
	case len(c.Variants) < MinVariants:
		return ErrVariantsRequired
	case len(c.Variants) > MaxVariants:
		return ErrTooManyVariants
	}
	seen := map[string]bool{}
	for i, v := range c.Variants {
		if c.ABTest() {
			name := strings.ToLower(strings.TrimSpace(v.Name))
			if name == "" {
				return fmt.Errorf("variant %d: %w", i+1, ErrVariantNameRequired)
			}
			if seen[name] {
				return fmt.Errorf("variant %d: %w", i+1, ErrVariantNameDuplicate)
			}
			seen[name] = true
		}
		if err := v.Validate(c.Channel, fields); err != nil {
			return fmt.Errorf("variant %d: %w", i+1, err)
		}
	}
	return nil
}

func (c Campaign) validateSchedule(now time.Time) error {
	if c.Status == StatusScheduled && c.ScheduledAt == nil {
		return ErrScheduleMissing
	}
	if !c.Status.Editable() {
		return nil
	}
	if c.ScheduledAt != nil && !c.ScheduledAt.After(now) {
		return ErrScheduleInPast
	}
	if c.Recurrence == nil {
		return nil
	}
	if err := c.Recurrence.Validate(); err != nil {
		return err
	}
	if _, ok := c.Recurrence.Next(now); !ok {
		return ErrRecurrenceEnded
	}
	return nil
}

func (c Campaign) Schedule(now time.Time, fields []*customfield.Definition) (Campaign, error) {
	status, err := Transition(c.Status, ActionSchedule)
	if err != nil {
		return c, err
	}
	if err := c.ReadyToLaunch(now, fields); err != nil {
		return c, err
	}
	at, err := c.firstOccurrence(now)
	if err != nil {
		return c, err
	}
	c.Status = status
	c.ScheduledAt = &at
	return c, nil
}

func (c Campaign) firstOccurrence(now time.Time) (time.Time, error) {
	if c.Recurrence == nil {
		if c.ScheduledAt == nil {
			return time.Time{}, ErrScheduleMissing
		}
		return *c.ScheduledAt, nil
	}
	from := now
	if c.ScheduledAt != nil {
		from = c.ScheduledAt.Add(-time.Nanosecond)
	}
	next, ok := c.Recurrence.Next(from)
	if !ok {
		return time.Time{}, ErrRecurrenceEnded
	}
	return next, nil
}

func (c Campaign) AfterRun(occurrence, now time.Time) (Campaign, error) {
	if c.Recurrence != nil {
		if next, ok := c.Recurrence.Next(latest(occurrence, now)); ok {
			status, err := Transition(c.Status, ActionRecur)
			if err != nil {
				return c, err
			}
			c.Status = status
			c.ScheduledAt = &next
			return c, nil
		}
	}
	status, err := Transition(c.Status, ActionComplete)
	if err != nil {
		return c, err
	}
	c.Status = status
	c.ScheduledAt = nil
	return c, nil
}

func latest(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

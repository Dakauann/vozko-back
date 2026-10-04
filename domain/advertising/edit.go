package advertising

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrEditNotForLevel  = errors.New("this change is not available for this level")
	ErrBudgetKindLocked = errors.New("meta does not switch between daily and lifetime budgets after creation")
	ErrNothingToChange  = errors.New("nothing to change")
)

type ObjectDetail struct {
	Object     *Object
	Budget     *Budget
	Bid        Bid
	Targeting  *Targeting
	Placements *Placements
	Schedule   []DayPart
	Creative   *CreativeDraft
	Identity   Identity
	MediaURLs  map[string]string
}

type ObjectEdit struct {
	Name       *string        `json:"name,omitempty"`
	Budget     *Budget        `json:"budget,omitempty"`
	Bid        *Bid           `json:"bid,omitempty"`
	EndAt      *time.Time     `json:"endAt,omitempty"`
	Targeting  *Targeting     `json:"targeting,omitempty"`
	Placements *Placements    `json:"placements,omitempty"`
	Schedule   []DayPart      `json:"schedule,omitempty"`
	Creative   *CreativeDraft `json:"creative,omitempty"`
}

func (e ObjectEdit) Empty() bool {
	return e.Name == nil && e.Budget == nil && e.Bid == nil && e.EndAt == nil && e.Targeting == nil &&
		e.Placements == nil && e.Schedule == nil && e.Creative == nil
}

func (e ObjectEdit) NeedsCurrentBudget() bool {
	return e.Budget != nil && e.Budget.Kind == ""
}

func (e ObjectEdit) For(current ObjectDetail) ObjectEdit {
	if !e.NeedsCurrentBudget() || current.Budget == nil {
		return e
	}
	e.Budget = &Budget{Kind: current.Budget.Kind, Amount: e.Budget.Amount}
	return e
}

func (e *ObjectEdit) Normalize() {
	if e.Name != nil {
		trimmed := strings.TrimSpace(*e.Name)
		e.Name = &trimmed
	}
	if e.Bid != nil {
		normalized := e.Bid.Normalized()
		e.Bid = &normalized
	}
	if e.Targeting != nil {
		e.Targeting.Normalize()
	}
	if e.Creative != nil {
		e.Creative.Normalize()
	}
}

func (e ObjectEdit) Validate(current ObjectDetail, restricted bool, now time.Time) error {
	if e.Empty() {
		return ErrNothingToChange
	}
	o := current.Object
	if o.Locked() {
		return ErrObjectLocked
	}
	v := newIssues()
	if e.Name != nil {
		v.text("name", *e.Name, true, maxNameRunes)
	}
	levelOnly := func(field string, ok bool) bool {
		if !ok {
			v.add(field, "not_for_level")
		}
		return ok
	}
	if e.Budget != nil && levelOnly("budget", o.Level != LevelAd) {
		e.validateBudget(v, current, now)
	}
	if e.Bid != nil && levelOnly("bid", o.Level != LevelAd) {
		e.Bid.validate(v.at("bid"), OptimizationGoal(o.OptimizationGoal))
		if o.Level == LevelCampaign {
			e.Bid.validateCampaignEdit(v.at("bid"))
		}
	}
	if e.EndAt != nil && levelOnly("endAt", o.Level != LevelAd) && !e.EndAt.After(now) {
		v.add("endAt", "in_the_past")
	}
	if e.Targeting != nil && levelOnly("targeting", o.Level == LevelAdSet) {
		e.Targeting.validate(v.at("targeting"), restricted)
	}
	if e.Placements != nil && levelOnly("placements", o.Level == LevelAdSet) {
		e.Placements.validate(v.at("placements"), Destination(o.DestinationType))
	}
	if e.Schedule != nil && levelOnly("schedule", o.Level == LevelAdSet) {
		validateSchedule(v.at("schedule"), e.Schedule, current.Budget)
	}
	if e.Creative != nil && levelOnly("creative", o.Level == LevelAd) {
		e.Creative.validate(v.at("creative"), Destination(o.DestinationType))
	}
	return v.err()
}

func (e ObjectEdit) validateBudget(v issues, current ObjectDetail, now time.Time) {
	e.Budget.validate(v.at("budget"))
	if current.Budget == nil {
		v.add("budget", "no_budget_here")
		return
	}
	if current.Budget.Kind != e.Budget.Kind {
		v.add("budget", "kind_locked")
	}
	if err := current.Object.CanChangeBudget(now); err != nil {
		v.add("budget", "too_many_changes")
	}
}

type CopyRequest struct {
	ParentID   string `json:"parentId,omitempty"`
	DeepCopy   bool   `json:"deepCopy"`
	NameSuffix string `json:"nameSuffix,omitempty"`
}

const maxCopySuffixRunes = 60

func (r CopyRequest) Validate(o *Object) error {
	v := newIssues()
	if o.Status == StatusDeleted || o.EffectiveStatus == EffectiveDeleted {
		return ErrObjectLocked
	}
	v.text("nameSuffix", r.NameSuffix, false, maxCopySuffixRunes)
	if r.ParentID != "" && o.Level == LevelCampaign {
		v.add("parentId", "not_for_level")
	}
	return v.err()
}

type Lifecycle string

const (
	LifecycleArchive Lifecycle = "archive"
	LifecycleDelete  Lifecycle = "delete"
)

func (l Lifecycle) Check(o *Object) error {
	switch l {
	case LifecycleArchive:
		if o.Status == StatusArchived || o.Status == StatusDeleted {
			return ErrObjectLocked
		}
	case LifecycleDelete:
		if o.Status == StatusDeleted {
			return ErrObjectLocked
		}
	default:
		return errors.New("advertising: unknown lifecycle action")
	}
	return nil
}

type EditSpec struct {
	Name       *string
	Budget     *Budget
	Bid        *Bid
	EndAt      *time.Time
	Targeting  *Targeting
	Placements *Placements
	Schedule   []DayPart
	CreativeID string
}

func EditSpecOf(e ObjectEdit, newCreativeID string) EditSpec {
	return EditSpec{
		Name: e.Name, Budget: e.Budget, Bid: e.Bid, EndAt: e.EndAt, Targeting: e.Targeting,
		Placements: e.Placements, Schedule: e.Schedule, CreativeID: newCreativeID,
	}
}

package advertising

import "strings"

type AccountEventKind string

const (
	AccountObjectsChanged  AccountEventKind = "objects_changed"
	AccountCreativeFatigue AccountEventKind = "creative_fatigue"
	AccountRecommendation  AccountEventKind = "recommendation"
	AccountProductSetIssue AccountEventKind = "product_set_issue"
	AccountEventUnused     AccountEventKind = "unused"
)

var accountFieldKinds = map[string]AccountEventKind{
	"in_process_ad_objects":  AccountObjectsChanged,
	"with_issues_ad_objects": AccountObjectsChanged,
	"field_changed":          AccountObjectsChanged,
	"effective_status":       AccountObjectsChanged,
	"creative_fatigue":       AccountCreativeFatigue,
	"ad_recommendations":     AccountRecommendation,
	"product_set_issue":      AccountProductSetIssue,
}

var objectLevels = map[string]Level{
	"campaign": LevelCampaign,
	"ad_set":   LevelAdSet,
	"adset":    LevelAdSet,
	"ad":       LevelAd,
}

type ObjectRef struct {
	MetaID string
	Level  Level
}

func ObjectRefOf(id, level string) (ObjectRef, bool) {
	metaID := strings.TrimSpace(id)
	resolved, ok := objectLevels[strings.ToLower(strings.TrimSpace(level))]
	if metaID == "" || !ok {
		return ObjectRef{}, false
	}
	return ObjectRef{MetaID: metaID, Level: resolved}, true
}

type AdAccountChange struct {
	AccountMetaID string
	Field         string
	Objects       []ObjectRef
	Unresolved    bool
}

func (c AdAccountChange) Kind() AccountEventKind {
	if kind, ok := accountFieldKinds[c.Field]; ok {
		return kind
	}
	return AccountEventUnused
}

func (c AdAccountChange) NeedsStructure() bool {
	return c.Kind() == AccountObjectsChanged && (c.Unresolved || len(c.Objects) == 0)
}

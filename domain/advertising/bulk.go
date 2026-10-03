package advertising

import (
	"regexp"
	"slices"
	"strings"
)

const MaxBulkObjects = 50

type BulkField string

const (
	BulkName        BulkField = "name"
	BulkPrimaryText BulkField = "primaryText"
	BulkHeadline    BulkField = "headline"
	BulkDescription BulkField = "description"
	BulkLink        BulkField = "link"
)

type BulkMode string

const (
	BulkSet     BulkMode = "set"
	BulkReplace BulkMode = "replace"
)

type BulkChange struct {
	Field     BulkField `json:"field"`
	Mode      BulkMode  `json:"mode"`
	Value     string    `json:"value,omitempty"`
	Find      string    `json:"find,omitempty"`
	Replace   string    `json:"replace,omitempty"`
	MatchCase bool      `json:"matchCase,omitempty"`
}

func ValidateBulkTargets(metaIDs []string) error {
	if len(metaIDs) == 0 {
		return FieldError("metaIds", "required")
	}
	if len(metaIDs) > MaxBulkObjects {
		return FieldError("metaIds", "too_many")
	}
	seen := make(map[string]bool, len(metaIDs))
	for _, id := range metaIDs {
		if strings.TrimSpace(id) == "" || seen[id] {
			return FieldError("metaIds", "invalid")
		}
		seen[id] = true
	}
	return nil
}

func (c BulkChange) Validate() error {
	switch c.Field {
	case BulkName, BulkPrimaryText, BulkHeadline, BulkDescription, BulkLink:
	default:
		return FieldError("change.field", "invalid")
	}
	switch c.Mode {
	case BulkSet:
		if c.Field == BulkName && strings.TrimSpace(c.Value) == "" {
			return FieldError("change.value", "required")
		}
	case BulkReplace:
		if c.Find == "" {
			return FieldError("change.find", "required")
		}
	default:
		return FieldError("change.mode", "invalid")
	}
	return nil
}

func (c BulkChange) Apply(current string) string {
	if c.Mode == BulkSet {
		return c.Value
	}
	if c.MatchCase {
		return strings.ReplaceAll(current, c.Find, c.Replace)
	}
	pattern := regexp.MustCompile("(?i)" + regexp.QuoteMeta(c.Find))
	return pattern.ReplaceAllLiteralString(current, c.Replace)
}

func (c BulkChange) EditFor(current ObjectDetail) (ObjectEdit, error) {
	if c.Field == BulkName {
		name := c.Apply(current.Object.Name)
		if name == current.Object.Name {
			return ObjectEdit{}, ErrNothingToChange
		}
		return ObjectEdit{Name: &name}, nil
	}
	if current.Object.Level != LevelAd || current.Creative == nil {
		return ObjectEdit{}, ErrEditNotForLevel
	}
	if current.Creative.Format == FormatExistingPost {
		return ObjectEdit{}, FieldError("change.field", "not_for_existing_post")
	}
	creative := current.Creative.clone()
	switch c.Field {
	case BulkPrimaryText:
		creative.PrimaryText = c.Apply(creative.PrimaryText)
		creative.Texts = c.applyAll(creative.Texts)
	case BulkHeadline:
		creative.Headline = c.Apply(creative.Headline)
		creative.Headlines = c.applyAll(creative.Headlines)
	case BulkDescription:
		creative.Description = c.Apply(creative.Description)
		creative.Descriptions = c.applyAll(creative.Descriptions)
	case BulkLink:
		creative.Link = c.Apply(creative.Link)
	}
	if creative.equalText(*current.Creative) {
		return ObjectEdit{}, ErrNothingToChange
	}
	return ObjectEdit{Creative: &creative}, nil
}

func (c BulkChange) applyAll(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = c.Apply(v)
	}
	return out
}

func (d CreativeDraft) clone() CreativeDraft {
	out := d
	out.Cards = append([]CarouselCard(nil), d.Cards...)
	out.Texts = append([]string(nil), d.Texts...)
	out.Headlines = append([]string(nil), d.Headlines...)
	out.Descriptions = append([]string(nil), d.Descriptions...)
	out.Medias = append([]MediaRef(nil), d.Medias...)
	out.IceBreakers = append([]string(nil), d.IceBreakers...)
	return out
}

func (d CreativeDraft) equalText(other CreativeDraft) bool {
	return d.PrimaryText == other.PrimaryText && d.Headline == other.Headline && d.Description == other.Description &&
		d.Link == other.Link && slices.Equal(d.Texts, other.Texts) && slices.Equal(d.Headlines, other.Headlines) &&
		slices.Equal(d.Descriptions, other.Descriptions)
}

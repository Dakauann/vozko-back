package advertising

import (
	"sort"
	"strings"
	"time"
)

const MaxActivities = 200

const defaultMetaLocale = "pt_BR"

var metaLocales = map[string]string{"pt": "pt_BR", "en": "en_US", "es": "es_LA", "de": "de_DE"}

type AdActivity struct {
	EventType  string
	Label      string
	At         time.Time
	ActorName  string
	ObjectID   string
	ObjectName string
	ObjectType string
	From       string
	To         string
}

type ActivityQuery struct {
	ObjectID string
	Since    time.Time
	Until    time.Time
	Locale   string
}

func MetaLocale(language string) string {
	lang, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(language)), "-")
	if locale, ok := metaLocales[lang]; ok {
		return locale
	}
	return defaultMetaLocale
}

func NewestActivities(activities []AdActivity) []AdActivity {
	sort.SliceStable(activities, func(i, j int) bool { return activities[i].At.After(activities[j].At) })
	if len(activities) > MaxActivities {
		return activities[:MaxActivities]
	}
	return activities
}

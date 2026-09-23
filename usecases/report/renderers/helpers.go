package report_renderers

import (
	"math"
	"strings"
	"time"

	"vozko/domain/report"
)

const isoMillis = "2006-01-02T15:04:05.000Z"

type Translate func(key string) string

type LabelResolver interface {
	For(locale, namespace string) Translate
}

type Labels map[string]map[string]string

type StaticLabels struct {
	byLocale map[string]Labels
	fallback string
}

func NewStaticLabels(fallback string) *StaticLabels {
	return &StaticLabels{byLocale: map[string]Labels{}, fallback: fallback}
}

func (l *StaticLabels) Add(locale string, labels Labels) *StaticLabels {
	if l.byLocale == nil {
		l.byLocale = map[string]Labels{}
	}
	l.byLocale[locale] = labels
	return l
}

func (l *StaticLabels) For(locale, namespace string) Translate {
	resolved := strings.ToLower(strings.TrimSpace(locale))
	table, found := l.byLocale[resolved]
	if !found {
		table = l.byLocale[l.fallback]
	}
	return func(key string) string {
		if table != nil {
			if section, ok := table[namespace]; ok {
				if value, ok := section[key]; ok && value != "" {
					return value
				}
			}
		}
		return humanizeKey(key)
	}
}

func humanizeKey(key string) string {
	trimmed := key
	if index := strings.LastIndex(key, "."); index >= 0 && index+1 < len(key) {
		trimmed = key[index+1:]
	}
	var builder strings.Builder
	for i, char := range trimmed {
		if char == '_' {
			builder.WriteRune(' ')
			continue
		}
		if i > 0 && char >= 'A' && char <= 'Z' {
			builder.WriteRune(' ')
		}
		builder.WriteRune(char)
	}
	out := strings.TrimSpace(builder.String())
	if out == "" {
		return key
	}
	return strings.ToUpper(out[:1]) + out[1:]
}

func metricRow(t Translate, key string, value report.CSVCell) []report.CSVCell {
	return []report.CSVCell{report.Text(t(key)), value}
}

func yesNo(t Translate, value bool) string {
	if value {
		return t("yes")
	}
	return t("no")
}

func pctCell(part, whole int64) report.CSVCell {
	if whole <= 0 {
		return report.Empty()
	}
	value := math.Round(float64(part)/float64(whole)*1000) / 10
	return report.Number(value)
}

func totalPctCell(total int64) report.CSVCell {
	if total <= 0 {
		return report.Empty()
	}
	return report.Number(100)
}

func costCell(available bool, micros int64) report.CSVCell {
	if !available {
		return report.Empty()
	}
	return report.Int(micros)
}

func countRows(sections []report.CSVSection) int64 {
	var total int64
	for _, section := range sections {
		total += int64(len(section.Rows))
	}
	return total
}

func parseDayStart(value string) (time.Time, bool) {
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func parseDayEnd(value string) (time.Time, bool) {
	parsed, ok := parseDayStart(value)
	if !ok {
		return time.Time{}, false
	}
	return parsed.Add(24*time.Hour - time.Second), true
}

func parseRFC3339(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

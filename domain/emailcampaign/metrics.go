package emailcampaign

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"vozko/domain/emailsending"
	"vozko/domain/shared"
)

const hourlyTimelineSpan = 48 * time.Hour

var BasisPoints = shared.BasisPoints

type Tally struct {
	Recipients   int64                `json:"recipients"`
	Pending      int64                `json:"pending"`
	Sent         int64                `json:"sent"`
	Failed       int64                `json:"failed"`
	Skipped      map[SkipReason]int64 `json:"skipped"`
	Delivered    int64                `json:"delivered"`
	Opens        int64                `json:"opens"`
	Clicks       int64                `json:"clicks"`
	HardBounces  int64                `json:"hard_bounces"`
	SoftBounces  int64                `json:"soft_bounces"`
	Complaints   int64                `json:"complaints"`
	Unsubscribes int64                `json:"unsubscribes"`
}

type Metrics struct {
	Tally
	SkippedTotal    int64 `json:"skipped_total"`
	DeliveryRate    int   `json:"delivery_rate"`
	OpenRate        int   `json:"open_rate"`
	ClickRate       int   `json:"click_rate"`
	ClickToOpenRate int   `json:"click_to_open_rate"`
	BounceRate      int   `json:"bounce_rate"`
	ComplaintRate   int   `json:"complaint_rate"`
	UnsubscribeRate int   `json:"unsubscribe_rate"`
}

func NewMetrics(t Tally) Metrics {
	m := Metrics{Tally: t}
	for _, n := range t.Skipped {
		m.SkippedTotal += n
	}
	m.DeliveryRate = BasisPoints(t.Delivered, t.Sent)
	m.OpenRate = BasisPoints(t.Opens, t.Delivered)
	m.ClickRate = BasisPoints(t.Clicks, t.Delivered)
	m.ClickToOpenRate = BasisPoints(t.Clicks, t.Opens)
	m.BounceRate = BasisPoints(t.HardBounces+t.SoftBounces, t.Sent)
	m.ComplaintRate = BasisPoints(t.Complaints, t.Delivered)
	m.UnsubscribeRate = BasisPoints(t.Unsubscribes, t.Delivered)
	return m
}

type Reputation struct {
	BounceRate         int                          `json:"bounce_rate"`
	BounceWarning      int                          `json:"bounce_warning"`
	BounceLimit        int                          `json:"bounce_limit"`
	BounceLevel        emailsending.ReputationLevel `json:"bounce_level" enums:"ok,warning,over"`
	BounceOverLimit    bool                         `json:"bounce_over_limit"`
	ComplaintRate      int                          `json:"complaint_rate"`
	ComplaintWarning   int                          `json:"complaint_warning"`
	ComplaintLimit     int                          `json:"complaint_limit"`
	ComplaintLevel     emailsending.ReputationLevel `json:"complaint_level" enums:"ok,warning,over"`
	ComplaintOverLimit bool                         `json:"complaint_over_limit"`
}

func NewReputation(t Tally) Reputation {
	bounce := BasisPoints(t.HardBounces+t.SoftBounces, t.Sent)
	complaint := BasisPoints(t.Complaints, t.Delivered)
	bounceBand, complaintBand := emailsending.BounceBand(), emailsending.ComplaintBand()
	bounceLevel, complaintLevel := bounceBand.Level(bounce), complaintBand.Level(complaint)
	return Reputation{
		BounceRate:         bounce,
		BounceWarning:      bounceBand.WarningBasisPoints,
		BounceLimit:        bounceBand.LimitBasisPoints,
		BounceLevel:        bounceLevel,
		BounceOverLimit:    bounceLevel == emailsending.ReputationOver,
		ComplaintRate:      complaint,
		ComplaintWarning:   complaintBand.WarningBasisPoints,
		ComplaintLimit:     complaintBand.LimitBasisPoints,
		ComplaintLevel:     complaintLevel,
		ComplaintOverLimit: complaintLevel == emailsending.ReputationOver,
	}
}

type Bucket string

const (
	BucketHour Bucket = "hour"
	BucketDay  Bucket = "day"
)

func TimelineBucketFor(start, end time.Time) Bucket {
	if end.Sub(start) <= hourlyTimelineSpan {
		return BucketHour
	}
	return BucketDay
}

type TimelinePoint struct {
	Bucket    time.Time `json:"bucket"`
	Sent      int64     `json:"sent"`
	Delivered int64     `json:"delivered"`
	Opens     int64     `json:"opens"`
	Clicks    int64     `json:"clicks"`
	Bounces   int64     `json:"bounces"`
}

type LinkStat struct {
	URL            string `json:"url"`
	Clicks         int64  `json:"clicks"`
	UniqueClickers int64  `json:"unique_clickers"`
}

type DomainCount struct {
	Domain    string
	Sent      int64
	Delivered int64
	Opens     int64
	Bounces   int64
}

type DomainStat struct {
	Family     string `json:"family"`
	Sent       int64  `json:"sent"`
	Delivered  int64  `json:"delivered"`
	Opens      int64  `json:"opens"`
	Bounces    int64  `json:"bounces"`
	OpenRate   int    `json:"open_rate"`
	BounceRate int    `json:"bounce_rate"`
}

const otherFamily = "other"

var mailboxFamilies = map[string]string{
	"gmail.com":      "gmail",
	"googlemail.com": "gmail",
	"hotmail.com":    "microsoft",
	"hotmail.com.br": "microsoft",
	"outlook.com":    "microsoft",
	"outlook.com.br": "microsoft",
	"live.com":       "microsoft",
	"msn.com":        "microsoft",
	"yahoo.com":      "yahoo",
	"yahoo.com.br":   "yahoo",
	"ymail.com":      "yahoo",
	"icloud.com":     "apple",
	"me.com":         "apple",
	"mac.com":        "apple",
	"uol.com.br":     "uol",
	"bol.com.br":     "uol",
}

func MailboxFamily(domain string) string {
	if family, ok := mailboxFamilies[strings.ToLower(strings.TrimSpace(domain))]; ok {
		return family
	}
	return otherFamily
}

func GroupDomains(counts []DomainCount) []DomainStat {
	byFamily := map[string]*DomainStat{}
	for _, c := range counts {
		family := MailboxFamily(c.Domain)
		stat, ok := byFamily[family]
		if !ok {
			stat = &DomainStat{Family: family}
			byFamily[family] = stat
		}
		stat.Sent += c.Sent
		stat.Delivered += c.Delivered
		stat.Opens += c.Opens
		stat.Bounces += c.Bounces
	}
	out := make([]DomainStat, 0, len(byFamily))
	for _, stat := range byFamily {
		stat.OpenRate = BasisPoints(stat.Opens, stat.Delivered)
		stat.BounceRate = BasisPoints(stat.Bounces, stat.Sent)
		out = append(out, *stat)
	}
	slices.SortFunc(out, func(a, b DomainStat) int {
		return cmp.Or(cmp.Compare(b.Sent, a.Sent), cmp.Compare(a.Family, b.Family))
	})
	return out
}

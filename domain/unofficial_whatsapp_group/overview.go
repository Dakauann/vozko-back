package unofficial_whatsapp_group

import (
	"sort"
	"strings"
	"time"

	"vozko/domain/shared"
)

type LabelCount struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type TrendPoint struct {
	Cycle        int          `json:"cycle"`
	At           time.Time    `json:"at"`
	Positivity   int          `json:"positivity"`
	Messages     int          `json:"messages"`
	Sentiment    RunSentiment `json:"sentiment"`
	Alert        AlertLevel   `json:"alert"`
	Participants int          `json:"participants"`
}

type MentionTotal struct {
	Entity     string                   `json:"entity"`
	Frequency  int                      `json:"frequency"`
	Sentiments map[shared.Sentiment]int `json:"sentiments"`
	Dominant   shared.Sentiment         `json:"dominantSentiment"`
}

type ParticipantTotal struct {
	Key        string               `json:"key"`
	Name       string               `json:"name"`
	JID        string               `json:"jid,omitempty"`
	Messages   int                  `json:"messages"`
	Sentiments map[RunSentiment]int `json:"sentiments"`
}

type Overview struct {
	Cycles           int                  `json:"cycles"`
	Completed        int                  `json:"completed"`
	Messages         int                  `json:"messages"`
	AlertCounts      map[AlertLevel]int   `json:"alertCounts"`
	SentimentCounts  map[RunSentiment]int `json:"sentimentCounts"`
	Dominant         RunSentiment         `json:"dominantSentiment"`
	AvgPositivity    int                  `json:"avgPositivity"`
	HighAlertPct     int                  `json:"highAlertPct"`
	Trend            []TrendPoint         `json:"trend"`
	Themes           []LabelCount         `json:"themes"`
	Mentions         []MentionTotal       `json:"mentions"`
	Participants     []ParticipantTotal   `json:"participants"`
	ParticipantCount int                  `json:"participantCount"`
	Suggestions      []LabelCount         `json:"suggestions"`
	LastRunAt        *time.Time           `json:"lastRunAt,omitempty"`
}

func BuildOverview(runs []Run, top int) Overview {
	o := Overview{
		Cycles:          len(runs),
		AlertCounts:     map[AlertLevel]int{AlertLow: 0, AlertMedium: 0, AlertHigh: 0},
		SentimentCounts: map[RunSentiment]int{},
		Dominant:        SentimentNeutral,
		AvgPositivity:   50,
		Trend:           []TrendPoint{},
	}

	ordered := append([]Run(nil), runs...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].CompletedAt.Before(ordered[j].CompletedAt) })

	themes := newLabelCounter()
	suggestions := newLabelCounter()
	mentions := map[string]*MentionTotal{}
	var mentionOrder []string
	participants := map[string]*ParticipantTotal{}
	var participantOrder []string
	positivity := 0

	for _, r := range ordered {
		if !r.CompletedAt.IsZero() {
			at := r.CompletedAt
			o.LastRunAt = &at
		}
		if !r.Completed() {
			continue
		}
		res := r.Result
		o.Completed++
		o.Messages += r.MessagesAnalyzed
		o.AlertCounts[res.AlertLevel]++
		o.SentimentCounts[res.Sentiment]++
		positivity += res.Sentiment.Score()
		o.Trend = append(o.Trend, TrendPoint{
			Cycle: r.Cycle, At: r.CompletedAt, Positivity: res.Sentiment.Score(), Messages: r.MessagesAnalyzed,
			Sentiment: res.Sentiment, Alert: res.AlertLevel, Participants: len(res.Participants),
		})

		for _, t := range res.KeyThemes {
			themes.add(t, 1)
		}
		for _, s := range res.SuggestedActions {
			suggestions.add(s.Situation, 1)
		}
		for _, m := range res.Mentions {
			freq := m.Frequency
			if freq <= 0 {
				freq = 1
			}
			total, ok := mentions[m.Entity]
			if !ok {
				total = &MentionTotal{Entity: m.Entity, Sentiments: map[shared.Sentiment]int{}}
				mentions[m.Entity] = total
				mentionOrder = append(mentionOrder, m.Entity)
			}
			total.Frequency += freq
			total.Sentiments[m.Sentiment] += freq
		}
		for _, p := range res.Participants {
			key := p.Key()
			total, ok := participants[key]
			if !ok {
				total = &ParticipantTotal{Key: key, Name: firstNonBlank(p.Name, p.JID), JID: p.JID, Sentiments: map[RunSentiment]int{}}
				participants[key] = total
				participantOrder = append(participantOrder, key)
			}
			total.Messages += p.Messages
			total.Sentiments[p.Sentiment] += p.Messages
		}
	}

	if o.Completed > 0 {
		o.AvgPositivity = roundDiv(positivity, o.Completed)
		o.HighAlertPct = roundDiv(o.AlertCounts[AlertHigh]*100, o.Completed)
		o.Dominant = dominantRunSentiment(o.SentimentCounts)
	}
	o.Themes = themes.top(top)
	o.Suggestions = suggestions.top(top)

	o.Mentions = make([]MentionTotal, 0, len(mentionOrder))
	for _, key := range mentionOrder {
		m := *mentions[key]
		m.Dominant = dominantTally(m.Sentiments)
		o.Mentions = append(o.Mentions, m)
	}
	sort.SliceStable(o.Mentions, func(i, j int) bool { return o.Mentions[i].Frequency > o.Mentions[j].Frequency })
	o.Mentions = limit(o.Mentions, top)

	o.ParticipantCount = len(participantOrder)
	o.Participants = make([]ParticipantTotal, 0, len(participantOrder))
	for _, key := range participantOrder {
		o.Participants = append(o.Participants, *participants[key])
	}
	sort.SliceStable(o.Participants, func(i, j int) bool { return o.Participants[i].Messages > o.Participants[j].Messages })
	o.Participants = limit(o.Participants, top)
	return o
}

type SentimentTally struct {
	Positive int64 `json:"positive"`
	Neutral  int64 `json:"neutral"`
	Negative int64 `json:"negative"`
}

func (t SentimentTally) Total() int64 {
	return t.Positive + t.Neutral + t.Negative
}

func (t *SentimentTally) Add(s shared.Sentiment, n int64) {
	switch s {
	case shared.SentimentPositive:
		t.Positive += n
	case shared.SentimentNegative:
		t.Negative += n
	default:
		t.Neutral += n
	}
}

func MessageSentiment(runs []Run) SentimentTally {
	var tally SentimentTally
	for _, r := range runs {
		if !r.Completed() {
			continue
		}
		if len(r.Result.Participants) == 0 {
			tally.Add(r.Result.Sentiment.Tally(), int64(r.MessagesAnalyzed))
			continue
		}
		for _, p := range r.Result.Participants {
			tally.Add(p.Sentiment.Tally(), int64(p.Messages))
		}
	}
	return tally
}

type labelCounter struct {
	order  []string
	counts map[string]int
	labels map[string]map[string]int
}

func newLabelCounter() *labelCounter {
	return &labelCounter{counts: map[string]int{}, labels: map[string]map[string]int{}}
}

func (c *labelCounter) add(label string, n int) {
	label = strings.TrimSpace(label)
	if label == "" {
		return
	}
	key := strings.ToLower(label)
	if _, ok := c.counts[key]; !ok {
		c.order = append(c.order, key)
		c.labels[key] = map[string]int{}
	}
	c.counts[key] += n
	c.labels[key][label] += n
}

func (c *labelCounter) top(n int) []LabelCount {
	out := make([]LabelCount, 0, len(c.order))
	for _, key := range c.order {
		out = append(out, LabelCount{Label: mostUsed(c.labels[key]), Count: c.counts[key]})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return limit(out, n)
}

func mostUsed(spellings map[string]int) string {
	best, bestCount := "", -1
	for label, count := range spellings {
		if count > bestCount || (count == bestCount && label < best) {
			best, bestCount = label, count
		}
	}
	return best
}

func dominantRunSentiment(counts map[RunSentiment]int) RunSentiment {
	best, bestCount := SentimentNeutral, 0
	for _, s := range []RunSentiment{SentimentPositive, SentimentNegative, SentimentMixed, SentimentNeutral} {
		if counts[s] > bestCount {
			best, bestCount = s, counts[s]
		}
	}
	return best
}

func dominantTally(counts map[shared.Sentiment]int) shared.Sentiment {
	best, bestCount := shared.SentimentNeutral, 0
	for _, s := range []shared.Sentiment{shared.SentimentPositive, shared.SentimentNegative, shared.SentimentNeutral} {
		if counts[s] > bestCount {
			best, bestCount = s, counts[s]
		}
	}
	return best
}

func roundDiv(num, den int) int {
	if den == 0 {
		return 0
	}
	return (num*2 + den) / (den * 2)
}

func limit[T any](items []T, n int) []T {
	if n > 0 && len(items) > n {
		return items[:n]
	}
	return items
}

func firstNonBlank(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (r Run) Tally() SentimentTally {
	return MessageSentiment([]Run{r})
}

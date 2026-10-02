package advertising

import (
	"strings"
	"time"
)

type TestLevel string

const (
	TestCampaigns TestLevel = "campaign"
	TestAdSets    TestLevel = "adset"
)

type TestCell struct {
	Name      string   `json:"name"`
	ObjectIDs []string `json:"objectIds"`
	Share     int      `json:"share"`
}

type SplitTest struct {
	MetaID      string     `json:"metaId,omitempty"`
	AdAccountID string     `json:"adAccountId"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Level       TestLevel  `json:"level"`
	Cells       []TestCell `json:"cells"`
	StartAt     time.Time  `json:"startAt"`
	EndAt       time.Time  `json:"endAt"`
	Confidence  int        `json:"confidence"`
}

type TestResult struct {
	CellName   string
	Winner     bool
	Confidence float64
	Metrics    Metrics
}

const (
	minTestCells    = 2
	maxTestCells    = 5
	minTestDuration = 24 * time.Hour
	maxTestDuration = 30 * 24 * time.Hour
)

var testConfidences = []int{65, 80, 90, 95}

func (t *SplitTest) Normalize() {
	t.Name = strings.TrimSpace(t.Name)
	if t.Confidence == 0 {
		t.Confidence = 90
	}
	even := 100 / max(len(t.Cells), 1)
	for i := range t.Cells {
		t.Cells[i].Name = strings.TrimSpace(t.Cells[i].Name)
		if t.Cells[i].Share == 0 {
			t.Cells[i].Share = even
		}
	}
	if len(t.Cells) > 0 {
		sum := 0
		for _, c := range t.Cells {
			sum += c.Share
		}
		if sum == even*len(t.Cells) {
			t.Cells[0].Share += 100 - sum
		}
	}
}

func (t SplitTest) Validate(now time.Time) error {
	v := newIssues()
	v.text("name", t.Name, true, maxNameRunes)
	if strings.TrimSpace(t.AdAccountID) == "" {
		v.add("adAccountId", "required")
	}
	if t.Level != TestCampaigns && t.Level != TestAdSets {
		v.add("level", "invalid")
	}
	if len(t.Cells) < minTestCells || len(t.Cells) > maxTestCells {
		v.add("cells", "count")
	}
	seen := map[string]bool{}
	total := 0
	for i, c := range t.Cells {
		cv := v.item("cells", i)
		cv.text("name", c.Name, true, maxNameRunes)
		if len(c.ObjectIDs) == 0 {
			cv.add("objectIds", "required")
		}
		for _, id := range c.ObjectIDs {
			if seen[id] {
				cv.add("objectIds", "used_twice")
			}
			seen[id] = true
		}
		if c.Share <= 0 {
			cv.add("share", "invalid")
		}
		total += c.Share
	}
	if total != 100 {
		v.add("cells", "shares_must_sum_100")
	}
	if !t.StartAt.After(now.Add(-time.Minute)) {
		v.add("startAt", "in_the_past")
	}
	if d := t.EndAt.Sub(t.StartAt); d < minTestDuration || d > maxTestDuration {
		v.add("endAt", "duration")
	}
	valid := false
	for _, c := range testConfidences {
		valid = valid || c == t.Confidence
	}
	if !valid {
		v.add("confidence", "invalid")
	}
	return v.err()
}

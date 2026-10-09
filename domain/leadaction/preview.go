package leadaction

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"slices"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/crmfilter"
	"vozko/domain/selection"
)

const (
	PreviewChunk     = 5000
	PreviewBudget    = 3 * time.Second
	PreviewRetention = 15 * time.Minute
	MaxExportLeads   = 250000
)

type PreviewStatus string

const (
	PreviewRunning PreviewStatus = "running"
	PreviewDone    PreviewStatus = "done"
	PreviewFailed  PreviewStatus = "failed"
)

type PreviewResult struct {
	Matched       int                `json:"matched"`
	ExpectedCount int                `json:"expectedCount"`
	Fingerprint   string             `json:"fingerprint"`
	Selected      int                `json:"selected"`
	Eligible      int                `json:"eligible"`
	Skipped       map[SkipReason]int `json:"skipped"`
}

func (r *PreviewResult) Add(chunk int, t Tally) {
	if r.Skipped == nil {
		r.Skipped = map[SkipReason]int{}
	}
	r.Selected += chunk
	skipped := 0
	for reason, n := range t.Skipped() {
		r.Skipped[reason] += n
		skipped += n
	}
	r.Eligible += chunk - skipped
}

type Preview struct {
	ID          string              `json:"id"`
	WorkspaceID string              `json:"workspaceId"`
	ActorID     string              `json:"actorId"`
	Action      Action              `json:"action"`
	Status      PreviewStatus       `json:"status"`
	Cursor      string              `json:"cursor,omitempty"`
	Result      PreviewResult       `json:"result"`
	FailureCode string              `json:"failureCode,omitempty"`
	UpdatedAt   time.Time           `json:"updatedAt"`
	Send        *campaign.SendQuote `json:"send,omitempty"`
}

func (p *Preview) Clone() *Preview {
	if p == nil {
		return nil
	}
	out := *p
	out.Result.Skipped = maps.Clone(p.Result.Skipped)
	if p.Send != nil {
		send := *p.Send
		if p.Send.CapRemaining != nil {
			remaining := *p.Send.CapRemaining
			send.CapRemaining = &remaining
		}
		out.Send = &send
	}
	return &out
}

func (p Preview) VisibleTo(userID string) bool {
	return userID != "" && userID == p.ActorID
}

type requestShape struct {
	Action      Action           `json:"a"`
	Params      Params           `json:"p"`
	Mode        selection.Mode   `json:"m"`
	IDs         []string         `json:"i,omitempty"`
	Fingerprint string           `json:"f"`
	Sort        []crmfilter.Sort `json:"s,omitempty"`
	Limit       int              `json:"l,omitempty"`
	ExcludeIDs  []string         `json:"x,omitempty"`
	Expected    int              `json:"e"`
}

func RequestFingerprint(a Action, p Params, s selection.Selection) string {
	ids, excluded := slices.Clone(s.IDs), slices.Clone(s.ExcludeIDs)
	slices.Sort(ids)
	slices.Sort(excluded)
	shape := requestShape{
		Action: a, Params: p.Normalized(), Mode: s.Mode, IDs: ids, Fingerprint: selection.Fingerprint(s.EffectiveFilter()),
		Sort: s.Sort, Limit: s.Limit, ExcludeIDs: excluded, Expected: s.ExpectedCount,
	}
	encoded, err := json.Marshal(shape)
	if err != nil {
		encoded = []byte(err.Error())
	}
	sum := sha256.Sum256(append([]byte("leadaction.v1|"), encoded...))
	return hex.EncodeToString(sum[:])
}

package lead

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"vozko/domain/cache"
	leaddomain "vozko/domain/lead"
)

type aggregateCache struct {
	state cache.SharedState
	ttl   time.Duration
}

const aggregateTTL = 60 * time.Second

const generationTTL = 24 * time.Hour

func newAggregateCache(state cache.SharedState) *aggregateCache {
	return &aggregateCache{state: state, ttl: aggregateTTL}
}

func (c *aggregateCache) enabled() bool {
	return c != nil && c.state != nil
}

func (c *aggregateCache) generation(workspaceID string) string {
	if !c.enabled() {
		return "0"
	}
	value, err := c.state.GetString(c.generationKey(workspaceID))
	if err != nil || strings.TrimSpace(value) == "" {
		return "0"
	}
	return value
}

func (c *aggregateCache) generationKey(workspaceID string) string {
	return "leads:agg:gen:" + workspaceID
}

func (c *aggregateCache) bump(workspaceID string) {
	if !c.enabled() || strings.TrimSpace(workspaceID) == "" {
		return
	}
	key := c.generationKey(workspaceID)
	if _, err := c.state.Incr(key); err != nil {
		return
	}
	_, _ = c.state.Expire(key, generationTTL)
}

func (c *aggregateCache) key(kind, workspaceID string, q *listQuery) string {
	h := sha256.New()
	h.Write([]byte(q.where))
	for _, arg := range q.args {
		fmt.Fprintf(h, "\x00%#v", arg)
	}
	digest := hex.EncodeToString(h.Sum(nil))[:32]

	return "leads:agg:" + kind + ":" + workspaceID + ":" + c.generation(workspaceID) + ":" + digest
}

func (c *aggregateCache) getCount(workspaceID string, q *listQuery) (int64, bool) {
	if !c.enabled() {
		return 0, false
	}
	raw, err := c.state.GetString(c.key("count", workspaceID, q))
	if err != nil || raw == "" {
		return 0, false
	}
	total, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return total, true
}

func (c *aggregateCache) setCount(workspaceID string, q *listQuery, total int64) {
	if !c.enabled() {
		return
	}
	_ = c.state.SetString(c.key("count", workspaceID, q), strconv.FormatInt(total, 10), c.ttl)
}

func (c *aggregateCache) getFacets(workspaceID string, q *listQuery) (*leaddomain.LeadFacets, bool) {
	if !c.enabled() {
		return nil, false
	}
	raw, err := c.state.GetString(c.key("facets", workspaceID, q))
	if err != nil || raw == "" {
		return nil, false
	}
	var facets leaddomain.LeadFacets
	if err := json.Unmarshal([]byte(raw), &facets); err != nil {
		return nil, false
	}
	return &facets, true
}

func (c *aggregateCache) setFacets(workspaceID string, q *listQuery, facets *leaddomain.LeadFacets) {
	if !c.enabled() || facets == nil {
		return
	}
	encoded, err := json.Marshal(facets)
	if err != nil {
		return
	}
	_ = c.state.SetString(c.key("facets", workspaceID, q), string(encoded), c.ttl)
}

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

// aggregateCache memoizes the two answers on the leads page that describe the
// whole filtered set rather than the page on screen: the total row count and
// the facet counts.
//
// Both are expensive for the same reason. They have no LIMIT above them, so
// they read every lead the filter selects, and the facet aggregate evaluates a
// correlated EXISTS per lead on top of that. They are also asked over and over
// with identical arguments: an operator paging through a list, or toggling a
// sort, re-asks a question whose answer cannot have changed.
//
// Staleness is bounded from both ends. Every write through this repository
// bumps a per-workspace generation, so a lead created, imported, renamed,
// blocked or deleted is reflected immediately. The TTL is the backstop for the
// counts this repository does not witness: adding a memory or a campaign entry
// happens in another repository and moves the "com memória" and "com campanha"
// facets, which will therefore lag by up to aggregateTTL. That is the trade
// this cache makes deliberately: a facet badge one minute behind is invisible,
// and a leads page that takes six seconds to open is not.
type aggregateCache struct {
	state cache.SharedState
	ttl   time.Duration
}

// aggregateTTL bounds how long a facet count may lag a write this repository
// did not see. Short enough that nobody reasons from a stale badge, long enough
// that a burst of paging costs one computation.
const aggregateTTL = 60 * time.Second

// generationTTL outlives aggregateTTL by a wide margin. If a generation expired
// while its cached values were still alive, the key would fall back to
// generation 0 and start serving entries written before the last invalidation.
const generationTTL = 24 * time.Hour

func newAggregateCache(state cache.SharedState) *aggregateCache {
	return &aggregateCache{state: state, ttl: aggregateTTL}
}

// enabled reports whether there is a backend to talk to. A repository built
// without one behaves exactly as it did before this file existed.
func (c *aggregateCache) enabled() bool {
	return c != nil && c.state != nil
}

// generation is the invalidation token every key for a workspace is built
// from. Bumping it retires every cached answer for that tenant at once, which
// is the only workable shape here: the filter space is unbounded, so there is
// no enumerable set of keys to delete.
//
// A read failure yields "0", which is a stable value and therefore safe: the
// worst case is that a cached answer stays reachable for the rest of its TTL.
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

// bump retires every cached aggregate for the workspace.
//
// Errors are swallowed on purpose, but note what a failure means: the previous
// generation stays current and a stale count can be served until its TTL
// expires. That is why the TTL is a minute and not an hour.
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

// key identifies one question: which workspace, which kind of answer, which
// filter, at which generation.
//
// The compiled WHERE clause AND its bound arguments both go into the hash. The
// clause alone is not enough, because every value an operator types arrives as
// a placeholder: "blocked = ?" is the same SQL for blocked and unblocked leads,
// and keying on it alone would serve one filter's count under the other's.
//
// Hashed rather than embedded so a filter carrying every predicate the panel
// offers cannot produce a multi-kilobyte key.
func (c *aggregateCache) key(kind, workspaceID string, q *listQuery) string {
	h := sha256.New()
	h.Write([]byte(q.where))
	for _, arg := range q.args {
		// %#v renders the value AND its type, so the integer 1 and the string
		// "1" do not collide into one cache entry.
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

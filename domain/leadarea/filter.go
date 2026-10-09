package leadarea

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"vozko/domain/crmfilter"
)

const MaxAreasPerFilter = crmfilter.MaxAreas

type Stamp struct {
	ID        string
	UpdatedAt time.Time
}

type Stamps []Stamp

func (s Stamps) Key() string {
	if len(s) == 0 {
		return ""
	}
	h := sha256.New()
	for _, stamp := range s {
		h.Write([]byte(stamp.ID + "@" + strconv.FormatInt(stamp.UpdatedAt.UnixNano(), 10) + "\x1f"))
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func IDsIn(f crmfilter.Filter) ([]string, error) {
	var ids []string
	seen := map[string]bool{}
	tested := 0
	for _, g := range f.Groups {
		for _, p := range g.Predicates {
			if !IsAreaField(p.Field) {
				continue
			}
			values, err := predicateAreas(p)
			if err != nil {
				return nil, err
			}
			if tested += len(values); tested > MaxAreasPerFilter {
				return nil, fmt.Errorf("%w: %d area tests, at most %d", ErrTooManyAreas, tested, MaxAreasPerFilter)
			}
			for _, id := range values {
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
	}
	return ids, nil
}

func Bind(f crmfilter.Filter, workspaceID, viewerID string, found []Area) (crmfilter.Filter, Stamps, error) {
	if _, err := IDsIn(f); err != nil {
		return crmfilter.Filter{}, nil, err
	}
	readable := map[string]Area{}
	for _, a := range found {
		if a.WorkspaceID == workspaceID && a.Owned().CanRead(viewerID) {
			readable[a.ID] = a
		}
	}
	bound := crmfilter.Filter{Groups: make([]crmfilter.Group, len(f.Groups))}
	if f.Groups == nil {
		bound.Groups = nil
	}
	used := map[string]Area{}
	for gi, g := range f.Groups {
		preds := make([]crmfilter.Predicate, len(g.Predicates))
		for pi, p := range g.Predicates {
			if !IsAreaField(p.Field) {
				preds[pi] = p
				continue
			}
			values, err := predicateAreas(p)
			if err != nil {
				return crmfilter.Filter{}, nil, err
			}
			bounds := make([]crmfilter.AreaBounds, 0, len(values))
			for _, id := range values {
				a, ok := readable[id]
				if !ok {
					return crmfilter.Filter{}, nil, fmt.Errorf("%w: %s", ErrNotFound, id)
				}
				used[id] = a
				bounds = append(bounds, a.Bounds())
			}
			p.Values = values
			preds[pi] = p.BindAreas(bounds)
		}
		bound.Groups[gi] = crmfilter.Group{Conjunction: g.Conjunction, Predicates: preds}
	}
	stamps := make(Stamps, 0, len(used))
	for _, a := range used {
		stamps = append(stamps, Stamp{ID: a.ID, UpdatedAt: a.UpdatedAt})
	}
	slices.SortFunc(stamps, func(x, y Stamp) int { return strings.Compare(x.ID, y.ID) })
	return bound, stamps, nil
}

func predicateAreas(p crmfilter.Predicate) ([]string, error) {
	if p.Operator != crmfilter.OpIn {
		return nil, fmt.Errorf("%w: %q", ErrOperatorUnsupported, p.Operator)
	}
	var values []string
	seen := map[string]bool{}
	for _, raw := range p.Values {
		id := strings.ToLower(strings.TrimSpace(raw))
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		values = append(values, id)
	}
	if len(values) == 0 {
		return nil, crmfilter.ErrMissingValue
	}
	return values, nil
}

func IsAreaField(field crmfilter.Field) bool {
	return field == crmfilter.FieldArea || field == crmfilter.FieldAreaApproximate
}

func HasArea(f crmfilter.Filter) bool {
	for _, g := range f.Groups {
		if groupTestsAnArea(g) {
			return true
		}
	}
	return false
}

func groupTestsAnArea(g crmfilter.Group) bool {
	return slices.ContainsFunc(g.Predicates, func(p crmfilter.Predicate) bool { return IsAreaField(p.Field) })
}

func groupTestsOnlyAreas(g crmfilter.Group) bool {
	return !slices.ContainsFunc(g.Predicates, func(p crmfilter.Predicate) bool { return !IsAreaField(p.Field) })
}

func LeftOut(f crmfilter.Filter) (crmfilter.Filter, bool, error) {
	out := crmfilter.Filter{Groups: make([]crmfilter.Group, len(f.Groups))}
	found := false
	for gi, g := range f.Groups {
		if groupTestsAnArea(g) && g.Conj() == crmfilter.Or && !groupTestsOnlyAreas(g) {
			return crmfilter.Filter{}, false, fmt.Errorf("%w: group %d", ErrLeftOutUnsupported, gi)
		}
		preds := make([]crmfilter.Predicate, len(g.Predicates))
		for pi, p := range g.Predicates {
			if IsAreaField(p.Field) {
				found = true
				p.Field = crmfilter.FieldAreaApproximate
				p.Key = ""
			}
			preds[pi] = p
		}
		out.Groups[gi] = crmfilter.Group{Conjunction: g.Conjunction, Predicates: preds}
	}
	return out, found, nil
}

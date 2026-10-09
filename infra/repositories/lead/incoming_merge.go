package lead

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"

	"vozko/domain/lead"
	"vozko/domain/shared"
)

const (
	mergeAttempts  = 3
	mergeBatchSize = 500
)

const mergeSQL = "UPDATE leads SET name = v.name, name_source = NULLIF(v.name_source, ''), profile_picture_url = v.profile_picture_url," +
	" age = v.age, updated_at = ?, version = leads.version + 1" +
	" FROM unnest(?::uuid[], ?::bigint[], ?::text[], ?::text[], ?::text[], ?::int[]) AS v(id, version, name, name_source, profile_picture_url, age)" +
	" WHERE leads.id = v.id AND leads.version = v.version AND leads.workspace_id = ? AND leads.deleted_at IS NULL" +
	" RETURNING leads.id::text AS id, leads.version AS version"

type incomingMerge struct {
	lead  *lead.Lead
	apply func(*lead.Lead) []string
}

func mergeOf(update lead.LeadUpdate) func(*lead.Lead) []string {
	return func(l *lead.Lead) []string { return l.MergeIncoming(update) }
}

type mergeGroup struct {
	lead    *lead.Lead
	applies []func(*lead.Lead) []string
}

func (g *mergeGroup) apply() bool {
	changed := false
	for _, apply := range g.applies {
		if len(apply(g.lead)) > 0 {
			changed = true
		}
	}
	return changed
}

func groupMerges(merges []incomingMerge) []*mergeGroup {
	byID := make(map[string]*mergeGroup, len(merges))
	groups := make([]*mergeGroup, 0, len(merges))
	for _, m := range merges {
		if m.lead == nil || m.apply == nil {
			continue
		}
		g, ok := byID[m.lead.ID]
		if !ok {
			g = &mergeGroup{lead: m.lead}
			byID[m.lead.ID] = g
			groups = append(groups, g)
		}
		g.applies = append(g.applies, m.apply)
	}
	return groups
}

func (r *repository) mergeIncoming(workspaceID string, merges []incomingMerge) (bool, error) {
	pending := groupMerges(merges)
	wrote := false
	for attempt := 1; len(pending) > 0; attempt++ {
		changed := make([]*mergeGroup, 0, len(pending))
		for _, g := range pending {
			if g.apply() {
				changed = append(changed, g)
			}
		}
		if len(changed) == 0 {
			return wrote, nil
		}
		stale, err := r.writeMerges(workspaceID, changed)
		if err != nil {
			return wrote, err
		}
		wrote = wrote || len(stale) < len(changed)
		if len(stale) == 0 {
			return wrote, nil
		}
		if attempt == mergeAttempts {
			return wrote, fmt.Errorf("incoming data for %d leads kept losing to concurrent edits: %w", len(stale), shared.ErrVersionConflict)
		}
		if err := r.reloadMerges(workspaceID, stale); err != nil {
			return wrote, err
		}
		pending = stale
	}
	return wrote, nil
}

type mergedRow struct {
	ID      string
	Version int64
}

func (r *repository) writeMerges(workspaceID string, groups []*mergeGroup) ([]*mergeGroup, error) {
	var stale []*mergeGroup
	for start := 0; start < len(groups); start += mergeBatchSize {
		chunk := groups[start:min(start+mergeBatchSize, len(groups))]
		missed, err := r.writeMergeChunk(workspaceID, chunk)
		if err != nil {
			return nil, err
		}
		stale = append(stale, missed...)
	}
	return stale, nil
}

func (r *repository) writeMergeChunk(workspaceID string, chunk []*mergeGroup) ([]*mergeGroup, error) {
	ids := make(pq.StringArray, len(chunk))
	versions := make(pq.Int64Array, len(chunk))
	names := make(pq.StringArray, len(chunk))
	sources := make(pq.StringArray, len(chunk))
	pictures := make(pq.StringArray, len(chunk))
	ages := make([]sql.NullInt64, len(chunk))
	for i, g := range chunk {
		l := g.lead
		ids[i], versions[i], names[i], sources[i], pictures[i] = l.ID, l.Version, l.Name, string(l.NameSource), l.ProfilePictureURL
		if l.StoredAge != nil {
			ages[i] = sql.NullInt64{Int64: int64(*l.StoredAge), Valid: true}
		}
	}

	now := time.Now().UTC()
	var rows []mergedRow
	if err := r.db.Raw(mergeSQL, now, ids, versions, names, sources, pictures, pq.GenericArray{A: ages}, workspaceID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	written := make(map[string]int64, len(rows))
	for _, row := range rows {
		written[row.ID] = row.Version
	}

	var stale []*mergeGroup
	for _, g := range chunk {
		version, ok := written[g.lead.ID]
		if !ok {
			stale = append(stale, g)
			continue
		}
		g.lead.Version, g.lead.UpdatedAt = version, now
	}
	return stale, nil
}

func (r *repository) reloadMerges(workspaceID string, groups []*mergeGroup) error {
	ids := make([]string, len(groups))
	for i, g := range groups {
		ids[i] = g.lead.ID
	}
	fresh, err := r.FindByIDs(workspaceID, ids)
	if err != nil {
		return err
	}
	byID := make(map[string]*lead.Lead, len(fresh))
	for _, l := range fresh {
		byID[l.ID] = l
	}
	for _, g := range groups {
		current, ok := byID[g.lead.ID]
		if !ok {
			return fmt.Errorf("lead %s: %w", g.lead.ID, lead.ErrLeadNotFound)
		}
		*g.lead = *current
	}
	return nil
}

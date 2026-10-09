package lead

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"vozko/domain/actor"
	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	infracrmfilter "vozko/infra/repositories/crmfilter"
)

var sectionSessionSettings = mapSessionSettings

var _ lead.SectionReader = (*repository)(nil)

func (q *listQuery) addressSummarySQL(extra string, extraArgs []interface{}) (string, []interface{}) {
	placed, placedArgs := placementCountsSQL(crmfilter.PlacementOnMap, crmfilter.PlacementApproximate)
	sql := "SELECT " + infracrmfilter.LeadAddressSummarySelect(summaryAddressAlias) + placed +
		extra + " " +
		"FROM leads " + infracrmfilter.LeadPrimaryAddressJoin(summaryAddressAlias) + " " +
		"WHERE " + q.where
	args := append(placedArgs, extraArgs...)
	return sql, append(args, q.args...)
}

func geoStatusCountsSQL() (string, []interface{}) {
	return placementCountsSQL(crmfilter.PlacementNotFound, crmfilter.PlacementQuotaExceeded, crmfilter.PlacementRefused, crmfilter.PlacementPending)
}

func (q *listQuery) summarySQL() (string, []interface{}, error) {
	birthday, birthdayArgs, err := q.desc.BirthdayCondition(crmfilter.BirthdayToday)
	if err != nil {
		return "", nil, err
	}
	geoExtra, geoArgs := geoStatusCountsSQL()
	extra := geoExtra + ", COUNT(*) FILTER (WHERE " + birthday + ") AS birthdays_today, " +
		"COUNT(*) FILTER (WHERE leads.blocked) AS blocked, " +
		"COUNT(*) FILTER (WHERE leads.id IN (" + q.desc.OpenWindowLeadIDs() + ")) AS window_open"
	sql, args := q.addressSummarySQL(extra, append(geoArgs, birthdayArgs...))
	return sql, args, nil
}

func (r *repository) sectionQuery(sq lead.SectionQuery) (*listQuery, error) {
	return r.compile(sq.ListInput())
}

func (r *repository) inSection(ctx context.Context, q *listQuery, fn func(tx *gorm.DB) error) error {
	return inReadSession(ctx, r.db, q.sessionSettings(sectionSessionSettings), fn)
}

func (r *repository) ReadSummary(ctx context.Context, sq lead.SectionQuery) (*lead.SummarySection, error) {
	q, err := r.sectionQuery(sq)
	if err != nil {
		return nil, err
	}
	sql, args, err := q.summarySQL()
	if err != nil {
		return nil, fmt.Errorf("lead summary section: %w", err)
	}
	var row struct {
		infracrmfilter.LeadAddressSummaryRow
		BirthdaysToday int64
		Blocked        int64
		WindowOpen     int64
	}
	if err := r.inSection(ctx, q, func(tx *gorm.DB) error { return tx.Raw(sql, args...).Scan(&row).Error }); err != nil {
		return nil, fmt.Errorf("lead summary section: %w", err)
	}
	geo := row.Summary()
	return &lead.SummarySection{
		Total:          geo.Total,
		WithAddress:    row.WithAddress,
		OnMap:          geo.OnMap,
		Approximate:    geo.Approximate,
		WithoutAddress: geo.WithoutAddress,
		NotFound:       geo.NotFound,
		QuotaExceeded:  geo.QuotaExceeded,
		Refused:        geo.Refused,
		Pending:        geo.Pending,
		BirthdaysToday: row.BirthdaysToday,
		Blocked:        row.Blocked,
		WindowOpen:     row.WindowOpen,
	}, nil
}

func (q *listQuery) distinctCountsSQL(source, keyCol, leadCol, conditions string, sourceArgs []interface{}) (string, []interface{}) {
	sql := "SELECT s.key AS key, COUNT(*) AS count FROM (SELECT DISTINCT " + keyCol + " AS key, " + leadCol + " FROM " + source +
		" WHERE " + conditions + leadCol + " IN (" + q.filteredIDs() + ")) s GROUP BY s.key"
	return sql, append(append([]interface{}{}, sourceArgs...), q.args...)
}

func (q *listQuery) campaignStatusesSQL() (string, []interface{}) {
	return q.distinctCountsSQL("whatsapp_campaign_entries wce_g", "wce_g.status", "wce_g.lead_id",
		"wce_g.deleted_at IS NULL AND wce_g.campaign_id IN (SELECT wc_g.id FROM whatsapp_campaigns wc_g WHERE wc_g.workspace_id = ?) AND ",
		[]interface{}{q.desc.WorkspaceID})
}

func (q *listQuery) channelsSQL() (string, []interface{}) {
	source, sourceArgs := infracrmfilter.LeadChannelsIn(q.desc.WorkspaceID)
	return q.distinctCountsSQL(source, "lead_channels.channel", "lead_channels.lead_id", "", sourceArgs)
}

func (q *listQuery) memoryCategoriesSQL() (string, []interface{}) {
	return q.distinctCountsSQL("lead_memories lm_g", "lm_g.category", "lm_g.lead_id",
		"lm_g.workspace_id = ? AND lm_g.deleted_at IS NULL AND ", []interface{}{q.desc.WorkspaceID})
}

func (q *listQuery) leadColumnsSQL(classificationKey string) (string, []interface{}) {
	sets := "(l.source), (l.owner_id, l.owner_kind)"
	classification, inner, args := "NULL::text", "", []interface{}{}
	if classificationKey != "" {
		sets += ", (l.classification)"
		classification, inner, args = "l.classification", ", leads.custom_fields ->> ?::text AS classification", []interface{}{classificationKey}
	}
	sql := "SELECT GROUPING(l.source) AS skip_source, GROUPING(l.owner_id, l.owner_kind) AS skip_owner, " +
		"l.source AS source, l.owner_id AS owner_id, l.owner_kind AS owner_kind, " + classification + " AS classification, COUNT(*) AS count " +
		"FROM (SELECT leads.source AS source, leads.owner_id::text AS owner_id, leads.owner_kind AS owner_kind" + inner + " " +
		"FROM leads WHERE " + q.where + ") l GROUP BY GROUPING SETS (" + sets + ")"
	return sql, append(args, q.args...)
}

func readDistinctFacets(tx *gorm.DB, q *listQuery) (campaigns, channels, memories map[string]int64, err error) {
	grouped := []struct {
		name string
		into *map[string]int64
		read func() (string, []interface{})
	}{
		{"campaign status", &campaigns, q.campaignStatusesSQL},
		{"channel", &channels, q.channelsSQL},
		{"memory category", &memories, q.memoryCategoriesSQL},
	}
	for _, g := range grouped {
		sql, args := g.read()
		var rows []facetRow
		if err := tx.Raw(sql, args...).Scan(&rows).Error; err != nil {
			return nil, nil, nil, fmt.Errorf("lead %s facet: %w", g.name, err)
		}
		*g.into = countsOf(rows)
	}
	return campaigns, channels, memories, nil
}

type leadColumnRow struct {
	SkipSource     int
	SkipOwner      int
	Source         *string
	OwnerID        *string
	OwnerKind      *string
	Classification *string
	Count          int64
}

func (r *repository) ReadFacets(ctx context.Context, sq lead.SectionQuery) (*lead.FacetsSection, error) {
	q, err := r.sectionQuery(sq)
	if err != nil {
		return nil, err
	}
	out := &lead.FacetsSection{}
	var columns []leadColumnRow
	err = r.inSection(ctx, q, func(tx *gorm.DB) error {
		var err error
		if out.CampaignStatuses, out.Channels, out.MemoryCategories, err = readDistinctFacets(tx, q); err != nil {
			return err
		}
		sql, args := q.leadColumnsSQL(sq.ClassificationKey)
		if err := tx.Raw(sql, args...).Scan(&columns).Error; err != nil {
			return fmt.Errorf("lead source, owner and classification facets: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	fillLeadColumnFacets(out, columns, sq.ClassificationKey)
	return out, nil
}

func fillLeadColumnFacets(out *lead.FacetsSection, rows []leadColumnRow, classificationKey string) {
	out.Sources, out.Owners = map[string]int64{}, []lead.OwnerCount{}
	var classification map[string]int64
	if classificationKey != "" {
		classification = map[string]int64{}
		out.Classification = &lead.ClassificationFacet{Key: classificationKey, Values: classification}
	}
	present := func(s *string) bool { return s != nil && *s != "" }
	for _, row := range rows {
		switch {
		case row.SkipSource == 0:
			if present(row.Source) {
				out.Sources[*row.Source] = row.Count
			}
		case row.SkipOwner == 0:
			if present(row.OwnerID) {
				kind := ""
				if row.OwnerKind != nil {
					kind = *row.OwnerKind
				}
				out.Owners = append(out.Owners, lead.OwnerCount{Owner: actor.Join(*row.OwnerID, actor.Kind(kind)), Count: row.Count})
			}
		case classification != nil:
			if present(row.Classification) {
				classification[*row.Classification] = row.Count
			}
		}
	}
	out.Owners, out.OwnersTruncated = lead.TopOwners(out.Owners)
}

func (q *listQuery) placesFrom(extra string, extraArgs ...interface{}) (string, []interface{}) {
	where := "la.workspace_id = ? AND la.is_primary AND la.city_key IS NOT NULL AND la.lead_id IN (" + q.filteredIDs() + ")" + extra
	args := append([]interface{}{q.desc.WorkspaceID}, q.args...)
	return "FROM lead_addresses la WHERE " + where, append(args, extraArgs...)
}

type placeMatch struct {
	sql  string
	args []interface{}
}

func placeWordStart(column, keyStart, prefix string) placeMatch {
	if prefix == "" {
		return placeMatch{}
	}
	sql, args := infracrmfilter.WordStartMatch(column, keyStart, prefix)
	return placeMatch{sql: " AND " + sql, args: args}
}

func (q *listQuery) citiesSQL() (string, []interface{}) {
	return q.cityCountsSQL("", "", lead.MaxPlaceCities)
}

func (q *listQuery) suggestPlaces(prefix string) {
	q.cityPlace = placeWordStart("la.city_key", ":", prefix)
	q.districtPlace = placeWordStart("la.district_key", "", prefix)
}

func commonestSpelling(column string) string {
	return "(array_agg(g." + column + " ORDER BY g.n DESC, g." + column + "))[1] AS " + column
}

func (q *listQuery) cityCountsSQL(inner, outer string, limit int) (string, []interface{}) {
	from, args := q.placesFrom(q.cityPlace.sql, q.cityPlace.args...)
	sql := "SELECT g.city_key AS city_key, " + commonestSpelling("city") + ", " + commonestSpelling("state") + ", sum(g.n)::bigint AS count" + outer +
		" FROM (SELECT la.city_key, la.city, la.state, COUNT(*) AS n" + inner + " " + from + " GROUP BY la.city_key, la.city, la.state) g" +
		" GROUP BY g.city_key ORDER BY count DESC, g.city_key LIMIT ?"
	return sql, append(args, limit)
}

func (q *listQuery) districtsSQL() (string, []interface{}) {
	return q.districtCountsSQL("", nil, "", "", lead.MaxPlaceDistricts)
}

func (q *listQuery) districtCountsSQL(inner string, innerArgs []interface{}, outer, having string, limit int) (string, []interface{}) {
	from, fromArgs := q.placesFrom(" AND la.district_key IS NOT NULL"+q.districtPlace.sql, q.districtPlace.args...)
	sql := "SELECT g.city_key AS city_key, g.district_key AS district_key, " + commonestSpelling("district") + ", " +
		commonestSpelling("city") + ", " + commonestSpelling("state") + ", sum(g.n)::bigint AS count" + outer +
		" FROM (SELECT la.city_key, la.district_key, la.district, la.city, la.state, COUNT(*) AS n" + inner + " " + from +
		" GROUP BY la.city_key, la.district_key, la.district, la.city, la.state) g" +
		" GROUP BY g.city_key, g.district_key" + having + " ORDER BY count DESC, g.city_key, g.district_key LIMIT ?"
	args := append(append([]interface{}{}, innerArgs...), fromArgs...)
	return sql, append(args, limit)
}

func (r *repository) ReadPlaces(ctx context.Context, sq lead.SectionQuery) (*lead.PlacesSection, error) {
	q, err := r.sectionQuery(sq)
	if err != nil {
		return nil, err
	}
	q.suggestPlaces(sq.PlacePrefix)
	cityLimit, districtLimit := sq.PlaceLimits()
	out := &lead.PlacesSection{Cities: []lead.CityCount{}, Districts: []lead.DistrictCount{}}
	err = r.inSection(ctx, q, func(tx *gorm.DB) error {
		citySQL, cityArgs := q.cityCountsSQL("", "", cityLimit)
		if err := tx.Raw(citySQL, cityArgs...).Scan(&out.Cities).Error; err != nil {
			return fmt.Errorf("lead city places: %w", err)
		}
		districtSQL, districtArgs := q.districtCountsSQL("", nil, "", "", districtLimit)
		if err := tx.Raw(districtSQL, districtArgs...).Scan(&out.Districts).Error; err != nil {
			return fmt.Errorf("lead bairro places: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for i := range out.Districts {
		out.Districts[i].Pair = crmfilter.DistrictPair(out.Districts[i].CityKey, out.Districts[i].DistrictKey)
	}
	return out, nil
}

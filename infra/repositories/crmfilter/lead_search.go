package crmfilter

import (
	"strings"

	"github.com/lib/pq"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	"vozko/infra/database"
)

func (d LeadDescriptor) compileQuery(p crmfilter.Predicate) (string, []interface{}, error) {
	if p.Operator != crmfilter.OpContains {
		return "", nil, unsupported(p)
	}
	ws, err := d.workspace()
	if err != nil {
		return "", nil, err
	}
	search, err := lead.ParseSearch(trimmedValues(p.Values)[0])
	if err != nil {
		return "", nil, err
	}
	driver := drivingTerm(search.Terms)
	set, args := searchSet(ws, search.Terms[driver])
	if len(search.Terms) == 1 {
		return d.id() + " IN (" + set + ")", args, nil
	}
	checks := make([]string, 0, len(search.Terms)-1)
	for i, term := range search.Terms {
		if i == driver {
			continue
		}
		check, checkArgs := searchCheck(term)
		checks = append(checks, check)
		args = append(args, checkArgs...)
	}
	return d.id() + " IN (SELECT s_q.id FROM (" + set + ") s_q WHERE " + strings.Join(checks, " AND ") + ")", args, nil
}

const exactNumberWeight = 1 << 20
const partialNumberWeight = 1 << 10

func drivingTerm(terms []lead.SearchTerm) int {
	best, bestWeight := 0, -1
	for i, term := range terms {
		weight := len([]rune(term.Text))
		switch {
		case len(term.Numbers) > 0:
			weight = exactNumberWeight
		case term.IsPhone():
			weight = partialNumberWeight + len(term.Digits)
		}
		if weight > bestWeight {
			best, bestWeight = i, weight
		}
	}
	return best
}

func searchCheck(term lead.SearchTerm) (string, []interface{}) {
	if term.IsPhone() {
		match, value := phoneMatchOf(term)
		return "(EXISTS (SELECT 1 FROM leads lc_q WHERE lc_q.id = s_q.id AND lc_q.number " + match + ")" +
			" OR EXISTS (SELECT 1 FROM lead_phones lpc_q WHERE lpc_q.lead_id = s_q.id AND lpc_q.number " + match + "))", []interface{}{value, value}
	}
	name, nameArgs := wordMatch(database.SearchFold("lc_q.name"), database.SearchFold("?"), term.Text, term.WordStartOnly(), "")
	nickname, nicknameArgs := wordMatch(database.SearchFold("lc_q.nickname"), database.SearchFold("?"), term.Text, term.WordStartOnly(), "")
	memory, memoryArgs := wordMatch(database.SearchFold("lmc_q.content"), database.SearchFold("?"), term.Text, term.WordStartOnly(), "")
	sql := "(EXISTS (SELECT 1 FROM leads lc_q WHERE lc_q.id = s_q.id AND (" + name + " OR " + nickname + "))"
	args := append(nameArgs, nicknameArgs...)
	if term.Place != "" {
		district, districtArgs := wordMatch("lac_q.district_key", "?", term.Place, term.PlaceWordStartOnly(), "")
		city, cityArgs := wordMatch("lac_q.city_key", "?", term.Place, term.PlaceWordStartOnly(), ":")
		sql += " OR EXISTS (SELECT 1 FROM lead_addresses lac_q WHERE lac_q.lead_id = s_q.id AND lac_q.is_primary AND (" + district + " OR " + city + "))"
		args = append(append(args, districtArgs...), cityArgs...)
	}
	sql += " OR EXISTS (SELECT 1 FROM lead_memories lmc_q WHERE lmc_q.lead_id = s_q.id AND lmc_q.deleted_at IS NULL AND " + memory + "))"
	return sql, append(args, memoryArgs...)
}

func phoneMatchOf(term lead.SearchTerm) (string, interface{}) {
	if len(term.Numbers) > 0 {
		return "= ANY(?)", pq.Array(term.Numbers)
	}
	return "LIKE ?", database.LikeContains(term.Digits)
}

func (d LeadDescriptor) SearchOrder(f crmfilter.Filter) (string, []interface{}, bool) {
	for _, g := range f.Groups {
		for _, p := range g.Predicates {
			if p.Field != crmfilter.FieldQuery || p.Operator != crmfilter.OpContains || len(p.Values) == 0 {
				continue
			}
			search, err := lead.ParseSearch(p.Values[0])
			if err != nil || search.RankText() == "" {
				return "", nil, false
			}
			name := database.SearchFold(d.alias() + ".name")
			order := "(" + name + " LIKE " + database.SearchFold("?") + ") DESC NULLS LAST, public.similarity(" + name + ", " + database.SearchFold("?") + ") DESC NULLS LAST"
			return order, []interface{}{database.LikePrefix(search.RankText()), search.RankText()}, true
		}
	}
	return "", nil, false
}

func searchSet(ws string, term lead.SearchTerm) (string, []interface{}) {
	if term.IsPhone() {
		return phoneSearchSet(ws, term)
	}
	name, nameArgs := wordMatch(database.SearchFold("l_q.name"), database.SearchFold("?"), term.Text, term.WordStartOnly(), "")
	nickname, nicknameArgs := wordMatch(database.SearchFold("l_q.nickname"), database.SearchFold("?"), term.Text, term.WordStartOnly(), "")
	memory, memoryArgs := wordMatch(database.SearchFold("lm_q.content"), database.SearchFold("?"), term.Text, term.WordStartOnly(), "")

	sql := "SELECT l_q.id FROM leads l_q WHERE l_q.workspace_id = ? AND l_q.deleted_at IS NULL AND " + name +
		" UNION SELECT l_q.id FROM leads l_q WHERE l_q.workspace_id = ? AND l_q.nickname IS NOT NULL AND l_q.deleted_at IS NULL AND " + nickname
	args := append(append([]interface{}{ws}, nameArgs...), ws)
	args = append(args, nicknameArgs...)
	if term.Place != "" {
		district, districtArgs := wordMatch("la_q.district_key", "?", term.Place, term.PlaceWordStartOnly(), "")
		city, cityArgs := wordMatch("la_q.city_key", "?", term.Place, term.PlaceWordStartOnly(), ":")
		sql += " UNION SELECT la_q.lead_id FROM lead_addresses la_q WHERE la_q.workspace_id = ? AND la_q.is_primary AND (" + district + " OR " + city + ")"
		args = append(append(append(args, ws), districtArgs...), cityArgs...)
	}
	sql += " UNION SELECT lm_q.lead_id FROM lead_memories lm_q WHERE lm_q.workspace_id = ? AND lm_q.deleted_at IS NULL AND " + memory
	return sql, append(append(args, ws), memoryArgs...)
}

func wordMatch(expr, placeholder, word string, wordStartOnly bool, keyStart string) (string, []interface{}) {
	if !wordStartOnly {
		return expr + " LIKE " + placeholder, []interface{}{database.LikeContains(word)}
	}
	first := database.LikePrefix(word)
	if keyStart != "" {
		first = "%" + keyStart + first
	}
	return "(" + expr + " LIKE " + placeholder + " OR " + expr + " LIKE " + placeholder + ")", []interface{}{first, "% " + database.LikePrefix(word)}
}

func phoneSearchSet(ws string, term lead.SearchTerm) (string, []interface{}) {
	match, value := phoneMatchOf(term)
	sql := "SELECT l_q.id FROM leads l_q WHERE l_q.workspace_id = ? AND l_q.deleted_at IS NULL AND l_q.number " + match +
		" UNION SELECT lp_q.lead_id FROM lead_phones lp_q WHERE lp_q.workspace_id = ? AND lp_q.number " + match
	return sql, []interface{}{ws, value, ws, value}
}

func WordStartMatch(expr, keyStart, word string) (string, []interface{}) {
	return wordMatch(expr, "?", word, true, keyStart)
}

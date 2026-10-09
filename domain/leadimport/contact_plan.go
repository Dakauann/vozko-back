package leadimport

import "vozko/domain/lead"

type ContactPlan struct {
	Ambiguous map[int]bool
	Groups    [][]int
}

func PlanContactLookups(phones [][]string, counts map[string]int, limit int) ContactPlan {
	plan := ContactPlan{Ambiguous: map[int]bool{}}
	var group []int
	loaded := map[string]bool{}
	load := 0
	for idx, row := range phones {
		formats := rowFormats(row)
		if len(formats) == 0 {
			continue
		}
		own := 0
		for _, format := range formats {
			own += counts[format]
		}
		if own > limit {
			plan.Ambiguous[idx] = true
			continue
		}
		extra := 0
		for _, format := range formats {
			if !loaded[format] {
				extra += counts[format]
			}
		}
		if len(group) > 0 && load+extra > limit {
			plan.Groups = append(plan.Groups, group)
			group, loaded, extra = nil, map[string]bool{}, own
			load = 0
		}
		for _, format := range formats {
			loaded[format] = true
		}
		load += extra
		group = append(group, idx)
	}
	if len(group) > 0 {
		plan.Groups = append(plan.Groups, group)
	}
	return plan
}

func (p ContactPlan) PhonesOf(phones [][]string, group int) []string {
	seen := map[string]bool{}
	var out []string
	for _, idx := range p.Groups[group] {
		for _, phone := range phones[idx] {
			if !seen[phone] {
				seen[phone] = true
				out = append(out, phone)
			}
		}
	}
	return out
}

func rowFormats(row []string) []string {
	seen := map[string]bool{}
	var formats []string
	for _, phone := range row {
		for _, format := range lead.NumberFormats(phone) {
			if !seen[format] {
				seen[format] = true
				formats = append(formats, format)
			}
		}
	}
	return formats
}

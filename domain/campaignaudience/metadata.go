package campaignaudience

import "vozko/domain/campaign"

type MetadataRule func(metadata map[string]any) []string

type MetadataGap struct {
	Key     string `json:"key" example:"escola"`
	Missing int    `json:"missing" example:"3"`
	Lines   []int  `json:"lines"`
}

type MetadataGaps struct {
	rule  MetadataRule
	gaps  []MetadataGap
	index map[string]int
}

func NewMetadataGaps(rule MetadataRule) *MetadataGaps {
	return &MetadataGaps{rule: rule, index: map[string]int{}}
}

func (g *MetadataGaps) Add(line int, metadata map[string]any) []string {
	if g.rule == nil {
		return nil
	}
	missing := g.rule(metadata)
	for _, key := range missing {
		i, seen := g.index[key]
		if !seen {
			i = len(g.gaps)
			g.index[key] = i
			g.gaps = append(g.gaps, MetadataGap{Key: key, Lines: []int{}})
		}
		g.gaps[i].Missing++
		if line > 0 && len(g.gaps[i].Lines) < campaign.MaxImportIssues {
			g.gaps[i].Lines = append(g.gaps[i].Lines, line)
		}
	}
	return missing
}

func (g *MetadataGaps) Any() bool {
	return len(g.gaps) > 0
}

func (g *MetadataGaps) Gaps() []MetadataGap {
	out := make([]MetadataGap, len(g.gaps))
	for i, gap := range g.gaps {
		out[i] = MetadataGap{Key: gap.Key, Missing: gap.Missing, Lines: append([]int{}, gap.Lines...)}
	}
	return out
}

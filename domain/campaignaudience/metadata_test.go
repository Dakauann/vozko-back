package campaignaudience

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/campaign"
)

func needsSchoolAndClass(metadata map[string]any) []string {
	var missing []string
	for _, key := range []string{"escola", "turma"} {
		if text, _ := metadata[key].(string); strings.TrimSpace(text) == "" {
			missing = append(missing, key)
		}
	}
	return missing
}

func TestMetadataGapsCountEveryRowPerKeyAndKeepItsLine(t *testing.T) {
	gaps := NewMetadataGaps(needsSchoolAndClass)
	gaps.Add(2, map[string]any{"escola": "Prisma", "turma": "A"})
	gaps.Add(3, map[string]any{"escola": "Prisma"})
	gaps.Add(4, nil)
	want := []MetadataGap{{Key: "turma", Missing: 2, Lines: []int{3, 4}}, {Key: "escola", Missing: 1, Lines: []int{4}}}
	if got := gaps.Gaps(); !reflect.DeepEqual(got, want) {
		t.Fatalf("gaps = %+v\nwant %+v", got, want)
	}
	if !gaps.Any() {
		t.Fatal("rows lack metadata")
	}
}

func TestMetadataGapsKeepAtMostTheImportIssueLinesPerKey(t *testing.T) {
	gaps := NewMetadataGaps(needsSchoolAndClass)
	for line := 1; line <= campaign.MaxImportIssues+5; line++ {
		gaps.Add(line, map[string]any{"escola": "Prisma"})
	}
	got := gaps.Gaps()
	if len(got) != 1 || got[0].Missing != campaign.MaxImportIssues+5 || len(got[0].Lines) != campaign.MaxImportIssues {
		t.Fatalf("gaps = %d keys, missing %d, %d lines", len(got), got[0].Missing, len(got[0].Lines))
	}
}

func TestMetadataGapsWithoutARuleReportNothing(t *testing.T) {
	gaps := NewMetadataGaps(nil)
	gaps.Add(2, nil)
	if gaps.Any() || gaps.Gaps() == nil || len(gaps.Gaps()) != 0 {
		t.Fatalf("gaps = %+v", gaps.Gaps())
	}
}

func TestTallyOfAListBlocksRowsMissingTheMetadataTheAutomationNeeds(t *testing.T) {
	tally := NewTally(ChannelWhatsApp, SourceList, nil).Requiring(needsSchoolAndClass)
	tally.Add(Candidate{Line: 2, Contact: "5511900000001", HasContact: true, Metadata: map[string]any{"escola": "Prisma", "turma": "A"}})
	tally.Add(Candidate{Line: 3, Contact: "5511900000002", HasContact: true, Refusal: string(campaign.SkipBlocked), Metadata: map[string]any{"escola": "Prisma"}})
	count := tally.Count()
	if !reflect.DeepEqual(count.Blockers, []Blocker{BlockerListMetadataMissing}) {
		t.Fatalf("blockers = %v", count.Blockers)
	}
	if !reflect.DeepEqual(count.MetadataMissing, []MetadataGap{{Key: "turma", Missing: 1, Lines: []int{3}}}) {
		t.Fatalf("metadata = %+v", count.MetadataMissing)
	}
	if err := tally.Creatable(); !errors.Is(err, ErrListMetadataMissing) {
		t.Fatalf("Creatable = %v", err)
	}
}

func TestTallyOfLeadsBlocksAnAutomationThatNeedsMetadata(t *testing.T) {
	tally := NewTally(ChannelWhatsApp, SourceLeads, nil).Requiring(needsSchoolAndClass)
	tally.Add(Candidate{LeadID: "lead-1", Contact: "5511900000001", HasContact: true})
	count := tally.Count()
	if !reflect.DeepEqual(count.Blockers, []Blocker{BlockerLeadsMetadataUnavailable}) {
		t.Fatalf("blockers = %v", count.Blockers)
	}
	want := []MetadataGap{{Key: "escola", Missing: 1, Lines: []int{}}, {Key: "turma", Missing: 1, Lines: []int{}}}
	if !reflect.DeepEqual(count.MetadataMissing, want) {
		t.Fatalf("metadata = %+v", count.MetadataMissing)
	}
	if err := tally.Creatable(); !errors.Is(err, ErrLeadsMetadataUnavailable) {
		t.Fatalf("Creatable = %v", err)
	}
}

func TestTallyWithoutAnAutomationReportsNoMetadata(t *testing.T) {
	count := NewTally(ChannelWhatsApp, SourceList, nil).Count()
	if count.MetadataMissing == nil || len(count.MetadataMissing) != 0 || len(count.Blockers) != 0 {
		t.Fatalf("count = %+v", count)
	}
}

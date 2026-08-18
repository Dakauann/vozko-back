package conversation_usecase

import "testing"

// WantsWork gates the whole debounce pass: a subject it rejects is never
// analysed, tagged or memorized. The memory case is the subtle one, because
// auto-memory alone only counts when a lead is actually linked.
func TestAnalysisSubjectWantsWork(t *testing.T) {
	cases := []struct {
		name    string
		subject *AnalysisSubject
		want    bool
	}{
		{"nil subject", nil, false},
		{"everything off", &AnalysisSubject{}, false},
		{"analysis only", &AnalysisSubject{EnableAnalysis: true}, true},
		{"auto-staging only", &AnalysisSubject{EnableAutoStaging: true}, true},
		{"auto-memory with lead", &AnalysisSubject{EnableAutoMemory: true, LeadID: "lead-1"}, true},
		{"auto-memory without lead", &AnalysisSubject{EnableAutoMemory: true}, false},
		{"auto-memory without lead but analysis on", &AnalysisSubject{EnableAutoMemory: true, EnableAnalysis: true}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.subject.WantsWork(); got != tc.want {
				t.Fatalf("WantsWork() = %v, want %v", got, tc.want)
			}
		})
	}
}

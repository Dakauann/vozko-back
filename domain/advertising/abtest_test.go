package advertising

import (
	"testing"
	"time"
)

func validTest() SplitTest {
	t := SplitTest{
		AdAccountID: "acc-1", Name: "Criativo A x B", Level: TestAdSets,
		Cells:   []TestCell{{Name: "A", ObjectIDs: []string{"s-1"}}, {Name: "B", ObjectIDs: []string{"s-2"}}},
		StartAt: draftNow.Add(time.Hour), EndAt: draftNow.Add(8 * 24 * time.Hour),
	}
	t.Normalize()
	return t
}

func TestSplitTestDefaultsToEvenSharesAndNinetyPercentConfidence(t *testing.T) {
	st := validTest()
	if st.Cells[0].Share != 50 || st.Cells[1].Share != 50 || st.Confidence != 90 {
		t.Fatalf("defaults %+v", st)
	}
	if err := st.Validate(draftNow); err != nil {
		t.Fatalf("valid test refused: %v", err)
	}
}

func TestThreeCellsStillSumToAHundred(t *testing.T) {
	st := validTest()
	st.Cells = append(st.Cells, TestCell{Name: "C", ObjectIDs: []string{"s-3"}})
	for i := range st.Cells {
		st.Cells[i].Share = 0
	}
	st.Normalize()
	if st.Cells[0].Share+st.Cells[1].Share+st.Cells[2].Share != 100 {
		t.Fatalf("shares %+v", st.Cells)
	}
}

func TestSplitTestRules(t *testing.T) {
	st := validTest()
	st.Cells[1].ObjectIDs = []string{"s-1"}
	st.EndAt = st.StartAt.Add(time.Hour)
	st.Confidence = 99
	requireIssues(t, st.Validate(draftNow),
		FieldIssue{"cells[1].objectIds", "used_twice"},
		FieldIssue{"endAt", "duration"},
		FieldIssue{"confidence", "invalid"},
	)
	one := validTest()
	one.Cells = one.Cells[:1]
	one.Cells[0].Share = 100
	requireIssues(t, one.Validate(draftNow), FieldIssue{"cells", "count"})
}

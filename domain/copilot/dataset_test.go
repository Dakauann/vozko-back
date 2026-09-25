package copilot

import (
	"errors"
	"fmt"
	"math"
	"testing"
)

func nanValue() float64 { return math.NaN() }

func teamDataset(t *testing.T, rows int) *Dataset {
	t.Helper()
	d := NewDataset("Equipe", []Column{
		{Key: "name", Label: "Nome", Kind: ColumnText},
		{Key: "resolved", Label: "Resolvidas", Kind: ColumnNumber},
		{Key: "frt", Label: "FRT", Kind: ColumnMinutes},
	})
	for i := 0; i < rows; i++ {
		var frt any
		if i%2 == 0 {
			frt = float64(i)
		}
		if err := d.AddRow(fmt.Sprintf("m%02d", i), int64(i*10), frt); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func TestAddRowRejectsWhatTheColumnCannotHold(t *testing.T) {
	d := NewDataset("x", []Column{{Key: "n", Kind: ColumnNumber}, {Key: "s", Kind: ColumnText}})
	cases := map[string][]any{
		"wrong arity":       {1.0},
		"text in a number":  {"12", "a"},
		"number in text":    {1.0, 2.0},
		"not a finite num":  {nanValue(), "a"},
		"unsupported value": {struct{}{}, "a"},
	}
	for name, row := range cases {
		t.Run(name, func(t *testing.T) {
			if err := d.AddRow(row...); !errors.Is(err, ErrInvalidDataset) {
				t.Fatalf("err = %v, want ErrInvalidDataset", err)
			}
		})
	}
	ptr := 3.5
	if err := d.AddRow(&ptr, "ok"); err != nil {
		t.Fatalf("a *float64 is a number: %v", err)
	}
	var missing *float64
	if err := d.AddRow(missing, "ok"); err != nil {
		t.Fatalf("a nil *float64 is a missing value: %v", err)
	}
}

func TestAddRowCapsTheTable(t *testing.T) {
	d := teamDataset(t, MaxDatasetRows)
	if err := d.AddRow("one-too-many", int64(1), nil); !errors.Is(err, ErrInvalidDataset) {
		t.Fatalf("err = %v, want the row cap enforced", err)
	}
}

func TestPreviewOfASmallTableIsTheWholeTable(t *testing.T) {
	p := teamDataset(t, 3).Preview()
	if p.RowCount != 3 || len(p.Rows) != 3 || p.Truncated {
		t.Fatalf("preview = %+v", p)
	}
}

func TestPreviewOfABigTableIsRowsPlusStatistics(t *testing.T) {
	p := teamDataset(t, 300).Preview()
	// The model must see the SHAPE of 300 rows without 300 rows landing in its context.
	if p.RowCount != 300 || len(p.Rows) != PreviewRows || !p.Truncated {
		t.Fatalf("rows = %d of %d truncated=%v", len(p.Rows), p.RowCount, p.Truncated)
	}
	if len(p.Stats) != 2 {
		t.Fatalf("stats = %+v, want one per numeric column", p.Stats)
	}
	resolved := p.Stats[0]
	if resolved.Column != "resolved" || resolved.Count != 300 || resolved.Min != 0 || resolved.Max != 2990 || resolved.Sum != 448500 || resolved.Avg != 1495 {
		t.Fatalf("resolved stats = %+v", resolved)
	}
	// Missing values are counted out, not averaged in as zero.
	if frt := p.Stats[1]; frt.Count != 150 || frt.Missing != 150 {
		t.Fatalf("frt stats = %+v", frt)
	}
}

func TestSliceSortsAndPagesWithinTheCap(t *testing.T) {
	d := teamDataset(t, 40)
	got, err := d.Slice(SliceQuery{SortBy: "resolved", Descending: true, Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != 3 || got.Rows[0][0] != "m39" || got.Rows[2][0] != "m37" {
		t.Fatalf("rows = %v", got.Rows)
	}
	// Sorting a slice must never reorder the stored table: a later chart reads the original order.
	if d.Rows[0][0] != "m00" {
		t.Fatal("Slice mutated the dataset")
	}
	page, _ := d.Slice(SliceQuery{Offset: 38, Limit: 500})
	if len(page.Rows) != 2 || page.Offset != 38 {
		t.Fatalf("page = %+v", page)
	}
	capped, _ := d.Slice(SliceQuery{Limit: 500})
	if len(capped.Rows) != MaxSliceRows {
		t.Fatalf("rows = %d, want the cap %d", len(capped.Rows), MaxSliceRows)
	}
	sparse, _ := d.Slice(SliceQuery{SortBy: "frt", Descending: true, Limit: 3})
	if sparse.Rows[0][2] == nil {
		t.Fatal("missing values must sort last, not first")
	}
	if _, err := d.Slice(SliceQuery{SortBy: "nope"}); !errors.Is(err, ErrInvalidDataset) {
		t.Fatalf("err = %v, want an unknown column refused", err)
	}
}

func TestStoreHandsOutHandlesAndForgetsNothingItPromised(t *testing.T) {
	s := NewDatasetStore()
	first := s.Put(teamDataset(t, 2))
	if first.ID == "" {
		t.Fatal("Put must mint an id")
	}
	got, err := s.Get(first.ID)
	if err != nil || got != first {
		t.Fatalf("Get = %v, %v", got, err)
	}
	if _, err := s.Get("ds-unknown"); !errors.Is(err, ErrDatasetNotFound) {
		t.Fatalf("err = %v, want ErrDatasetNotFound", err)
	}
	var nilStore *DatasetStore
	if _, err := nilStore.Get(first.ID); !errors.Is(err, ErrDatasetNotFound) {
		t.Fatalf("a turn without a store has no datasets: %v", err)
	}
}

func TestStoreEvictsTheOldestBeyondItsCapacity(t *testing.T) {
	s := NewDatasetStore()
	first := s.Put(teamDataset(t, 1))
	for i := 0; i < MaxDatasetsPerTurn; i++ {
		s.Put(teamDataset(t, 1))
	}
	if _, err := s.Get(first.ID); !errors.Is(err, ErrDatasetNotFound) {
		t.Fatalf("err = %v, want the oldest evicted", err)
	}
}

func TestPreviewShowsATableThatFitsInOneSliceWhole(t *testing.T) {
	// A 24-month trend is read end to end; cutting it at ten rows would hide the recent months.
	p := teamDataset(t, MaxSliceRows).Preview()
	if len(p.Rows) != MaxSliceRows || p.Truncated {
		t.Fatalf("rows = %d truncated = %v", len(p.Rows), p.Truncated)
	}
}

func TestHandleDescribesWithoutRows(t *testing.T) {
	h := teamDataset(t, 300).Handle()
	if h.Rows != nil || h.RowCount != 300 || len(h.Stats) != 2 || !h.Truncated {
		t.Fatalf("handle = %+v", h)
	}
}

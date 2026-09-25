package copilot

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"
)

type ColumnKind string

const (
	ColumnText    ColumnKind = "text"
	ColumnDate    ColumnKind = "date"
	ColumnNumber  ColumnKind = "number"
	ColumnPercent ColumnKind = "percent"
	ColumnMinutes ColumnKind = "minutes"
	ColumnMoney   ColumnKind = "money"
)

func (k ColumnKind) Numeric() bool {
	switch k {
	case ColumnNumber, ColumnPercent, ColumnMinutes, ColumnMoney:
		return true
	}
	return false
}

const (
	MaxDatasetRows     = 5000
	MaxDatasetsPerTurn = 16
	PreviewRows        = 10
	MaxSliceRows       = 25
)

var (
	ErrInvalidDataset  = errors.New("copilot: invalid dataset")
	ErrDatasetNotFound = errors.New("copilot: dataset not found")
)

type Column struct {
	Key   string     `json:"key"`
	Label string     `json:"label"`
	Kind  ColumnKind `json:"kind"`
}

type Dataset struct {
	ID      string
	Title   string
	Columns []Column
	Rows    [][]any
}

func NewDataset(title string, columns []Column) *Dataset {
	return &Dataset{Title: title, Columns: columns, Rows: [][]any{}}
}

func (d *Dataset) AddRow(values ...any) error {
	if len(values) != len(d.Columns) {
		return fmt.Errorf("%w: row has %d values for %d columns", ErrInvalidDataset, len(values), len(d.Columns))
	}
	if len(d.Rows) >= MaxDatasetRows {
		return fmt.Errorf("%w: more than %d rows", ErrInvalidDataset, MaxDatasetRows)
	}
	row := make([]any, len(values))
	for i, raw := range values {
		v, err := normalizeCell(d.Columns[i], raw)
		if err != nil {
			return err
		}
		row[i] = v
	}
	d.Rows = append(d.Rows, row)
	return nil
}

func normalizeCell(col Column, raw any) (any, error) {
	if col.Kind.Numeric() {
		n, present, err := toNumber(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: column %s: %v", ErrInvalidDataset, col.Key, err)
		}
		if !present {
			return nil, nil
		}
		return n, nil
	}
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case string:
		return v, nil
	}
	return nil, fmt.Errorf("%w: column %s holds text, got %T", ErrInvalidDataset, col.Key, raw)
}

func toNumber(raw any) (float64, bool, error) {
	var n float64
	switch v := raw.(type) {
	case nil:
		return 0, false, nil
	case *float64:
		if v == nil {
			return 0, false, nil
		}
		n = *v
	case float64:
		n = v
	case int:
		n = float64(v)
	case int64:
		n = float64(v)
	default:
		return 0, false, fmt.Errorf("expected a number, got %T", raw)
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false, errors.New("not a finite number")
	}
	return n, true, nil
}

func (d *Dataset) columnIndex(key string) (int, error) {
	for i, c := range d.Columns {
		if c.Key == key {
			return i, nil
		}
	}
	return -1, fmt.Errorf("%w: unknown column %q", ErrInvalidDataset, key)
}

type ColumnStats struct {
	Column  string  `json:"column"`
	Count   int     `json:"count"`
	Missing int     `json:"missing,omitempty"`
	Min     float64 `json:"min"`
	Max     float64 `json:"max"`
	Sum     float64 `json:"sum"`
	Avg     float64 `json:"avg"`
}

type DatasetPreview struct {
	DatasetID string        `json:"dataset_id"`
	Title     string        `json:"title"`
	Columns   []Column      `json:"columns"`
	RowCount  int           `json:"row_count"`
	Offset    int           `json:"offset,omitempty"`
	Rows      [][]any       `json:"rows"`
	Truncated bool          `json:"truncated"`
	Stats     []ColumnStats `json:"stats,omitempty"`
}

func (d *Dataset) Preview() DatasetPreview {
	rows := d.Rows
	if len(rows) > MaxSliceRows {
		rows = rows[:PreviewRows]
	}
	return DatasetPreview{
		DatasetID: d.ID,
		Title:     d.Title,
		Columns:   d.Columns,
		RowCount:  len(d.Rows),
		Rows:      rows,
		Truncated: len(rows) < len(d.Rows),
		Stats:     d.stats(),
	}
}

func (d *Dataset) Handle() DatasetPreview {
	return DatasetPreview{
		DatasetID: d.ID,
		Title:     d.Title,
		Columns:   d.Columns,
		RowCount:  len(d.Rows),
		Truncated: len(d.Rows) > 0,
		Stats:     d.stats(),
	}
}

func (d *Dataset) stats() []ColumnStats {
	var out []ColumnStats
	for i, col := range d.Columns {
		if !col.Kind.Numeric() {
			continue
		}
		s := ColumnStats{Column: col.Key}
		for _, row := range d.Rows {
			v, ok := row[i].(float64)
			if !ok {
				s.Missing++
				continue
			}
			if s.Count == 0 || v < s.Min {
				s.Min = v
			}
			if s.Count == 0 || v > s.Max {
				s.Max = v
			}
			s.Sum += v
			s.Count++
		}
		if s.Count > 0 {
			s.Avg = math.Round(s.Sum/float64(s.Count)*100) / 100
		}
		s.Sum = math.Round(s.Sum*100) / 100
		out = append(out, s)
	}
	return out
}

type SliceQuery struct {
	SortBy     string
	Descending bool
	Offset     int
	Limit      int
}

func (d *Dataset) Slice(q SliceQuery) (DatasetPreview, error) {
	rows, err := d.sortedRows(q.SortBy, q.Descending)
	if err != nil {
		return DatasetPreview{}, err
	}
	limit := q.Limit
	if limit <= 0 || limit > MaxSliceRows {
		limit = MaxSliceRows
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > len(rows) {
		offset = len(rows)
	}
	end := offset + limit
	if end > len(rows) {
		end = len(rows)
	}
	return DatasetPreview{
		DatasetID: d.ID,
		Title:     d.Title,
		Columns:   d.Columns,
		RowCount:  len(d.Rows),
		Offset:    offset,
		Rows:      rows[offset:end],
		Truncated: end < len(rows) || offset > 0,
	}, nil
}

func (d *Dataset) sortedRows(sortBy string, descending bool) ([][]any, error) {
	rows := append([][]any(nil), d.Rows...)
	if sortBy == "" {
		return rows, nil
	}
	idx, err := d.columnIndex(sortBy)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(a, b int) bool {
		return cellLess(rows[a][idx], rows[b][idx], descending)
	})
	return rows, nil
}

func cellLess(a, b any, descending bool) bool {
	if a == nil || b == nil {
		return a != nil && b == nil
	}
	if x, ok := a.(float64); ok {
		y, _ := b.(float64)
		if descending {
			return x > y
		}
		return x < y
	}
	x, y := fmt.Sprint(a), fmt.Sprint(b)
	if descending {
		return x > y
	}
	return x < y
}

type DatasetStore struct {
	mu    sync.Mutex
	next  int
	order []string
	byID  map[string]*Dataset
}

func NewDatasetStore() *DatasetStore {
	return &DatasetStore{byID: make(map[string]*Dataset)}
}

func (s *DatasetStore) Put(d *Dataset) *Dataset {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	d.ID = "ds" + strconv.Itoa(s.next)
	s.byID[d.ID] = d
	s.order = append(s.order, d.ID)
	if len(s.order) > MaxDatasetsPerTurn {
		delete(s.byID, s.order[0])
		s.order = s.order[1:]
	}
	return d
}

func (s *DatasetStore) Get(id string) (*Dataset, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: %s", ErrDatasetNotFound, id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, found := s.byID[id]
	if !found {
		return nil, fmt.Errorf("%w: %s (datasets live for one answer; query again)", ErrDatasetNotFound, id)
	}
	return d, nil
}

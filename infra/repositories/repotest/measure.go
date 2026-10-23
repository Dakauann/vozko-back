package repotest

import (
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

type Measurement struct {
	Wall       time.Duration
	Statements int
	PeakHeap   uint64
	Allocated  uint64
}

type Statements struct {
	mu   sync.Mutex
	sqls []string
}

func CountStatements(t testing.TB, db *gorm.DB) *Statements {
	t.Helper()
	s := &Statements{}
	record := func(tx *gorm.DB) {
		s.mu.Lock()
		s.sqls = append(s.sqls, tx.Statement.SQL.String())
		s.mu.Unlock()
	}
	callbacks := db.Callback()
	for name, err := range map[string]error{
		"query":  callbacks.Query().After("*").Register("repotest:count_query", record),
		"row":    callbacks.Row().After("*").Register("repotest:count_row", record),
		"raw":    callbacks.Raw().After("*").Register("repotest:count_raw", record),
		"create": callbacks.Create().After("*").Register("repotest:count_create", record),
		"update": callbacks.Update().After("*").Register("repotest:count_update", record),
		"delete": callbacks.Delete().After("*").Register("repotest:count_delete", record),
	} {
		if err != nil {
			t.Fatalf("count %s statements: %v", name, err)
		}
	}
	return s
}

func (s *Statements) mark() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sqls)
}

func (s *Statements) Since(mark int, containing string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, sql := range s.sqls[mark:] {
		if strings.Contains(sql, containing) {
			n++
		}
	}
	return n
}

func (s *Statements) Measure(fn func()) (Measurement, int) {
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	mark := s.mark()
	stop := make(chan struct{})
	peak := make(chan uint64)
	go func() {
		highest := before.HeapAlloc
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			var now runtime.MemStats
			runtime.ReadMemStats(&now)
			highest = max(highest, now.HeapAlloc)
			select {
			case <-stop:
				peak <- highest
				return
			case <-tick.C:
			}
		}
	}()
	started := time.Now()
	fn()
	wall := time.Since(started)
	close(stop)
	highest := <-peak
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	return Measurement{Wall: wall, Statements: s.mark() - mark, PeakHeap: highest - before.HeapAlloc, Allocated: after.TotalAlloc - before.TotalAlloc}, mark
}

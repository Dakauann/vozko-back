package googlesheet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/google/uuid"

	"vozko/domain/cache"
	"vozko/domain/googleaccount"
	"vozko/domain/googlesheet"
	"vozko/domain/metrics"
)

const (
	busyPressure = 0.7
	dispatchPage = 200
	leaseProbe   = 250 * time.Millisecond
	maxJitter    = time.Second
)

type WriterConfig struct {
	BatchRows   int
	BatchBytes  int
	Linger      time.Duration
	BusyLinger  time.Duration
	LeaseTTL    time.Duration
	DrainBudget time.Duration
	AccountWait time.Duration
	HeaderCheck time.Duration
	Concurrency int
}

func DefaultWriterConfig(concurrency int) WriterConfig {
	return WriterConfig{
		BatchRows:   googlesheet.DefaultBatchRows,
		BatchBytes:  googlesheet.DefaultBatchBytes,
		Linger:      2 * time.Second,
		BusyLinger:  10 * time.Second,
		LeaseTTL:    2 * time.Minute,
		DrainBudget: 50 * time.Second,
		AccountWait: 5 * time.Minute,
		HeaderCheck: 10 * time.Minute,
		Concurrency: concurrency,
	}
}

type WriterDeps struct {
	Sheets  googlesheet.SheetRepository
	Rows    googlesheet.RowRepository
	API     googlesheet.SheetsAPI
	Errors  googlesheet.ErrorClassifier
	Access  googleaccount.AccessSource
	Limiter googlesheet.Limiter
	Leases  cache.Locker
	Marks   Marker
	Alerts  Alerts
	Metrics metrics.GoogleSheetsMetricsRecorder
	Config  WriterConfig
}

type Writer struct {
	sheets  googlesheet.SheetRepository
	rows    googlesheet.RowRepository
	api     googlesheet.SheetsAPI
	errs    googlesheet.ErrorClassifier
	access  googleaccount.AccessSource
	limiter googlesheet.Limiter
	leases  cache.Locker
	marks   Marker
	alerts  Alerts
	metrics metrics.GoogleSheetsMetricsRecorder
	cfg     WriterConfig
	now     Clock
	jitter  func() time.Duration
	slots   chan struct{}
	nudged  sync.Map
}

var _ Nudger = (*Writer)(nil)

func NewWriter(d WriterDeps) *Writer {
	if d.Sheets == nil || d.Rows == nil || d.API == nil || d.Errors == nil || d.Access == nil || d.Limiter == nil ||
		d.Leases == nil || d.Marks == nil || d.Alerts == nil || d.Metrics == nil {
		panic("googlesheet: the writer requires sheets, rows, the sheets api, an error classifier, google access, a limiter, leases, marks, alerts and metrics")
	}
	if d.Config.Concurrency < 1 || d.Config.BatchRows < 1 || d.Config.BatchBytes < 1 || d.Config.LeaseTTL <= d.Config.DrainBudget {
		panic(fmt.Sprintf("googlesheet: invalid writer config %+v", d.Config))
	}
	return &Writer{
		sheets: d.Sheets, rows: d.Rows, api: d.API, errs: d.Errors, access: d.Access, limiter: d.Limiter,
		leases: d.Leases, marks: d.Marks, alerts: d.Alerts, metrics: d.Metrics, cfg: d.Config,
		now:    utcNow,
		jitter: func() time.Duration { return time.Duration(rand.Int64N(int64(maxJitter))) },
		slots:  make(chan struct{}, d.Config.Concurrency),
	}
}

func (w *Writer) Nudge(sheetID string) {
	if _, scheduled := w.nudged.LoadOrStore(sheetID, struct{}{}); scheduled {
		return
	}
	time.AfterFunc(w.linger(context.Background()), func() {
		w.nudged.Delete(sheetID)
		w.run(context.Background(), sheetID)
	})
}

func (w *Writer) DispatchPending(ctx context.Context) error {
	now := w.now()
	pending, err := w.rows.PendingSheets(ctx, now, dispatchPage)
	if err != nil {
		return err
	}
	linger := w.linger(ctx)
	var wg sync.WaitGroup
	for _, p := range pending {
		if now.Sub(p.OldestPendingAt) < linger {
			continue
		}
		wg.Add(1)
		go func(sheetID string) {
			defer wg.Done()
			w.run(ctx, sheetID)
		}(p.SheetID)
	}
	wg.Wait()
	return nil
}

func (w *Writer) run(ctx context.Context, sheetID string) {
	select {
	case w.slots <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-w.slots }()
	runCtx, cancel := context.WithTimeout(ctx, w.cfg.LeaseTTL)
	defer cancel()
	if err := w.WriteSheet(runCtx, sheetID); err != nil {
		w.metrics.AddGoogleSheetsEvent("write", "error")
		log.Printf("[googlesheet] writing sheet %s failed: %v", sheetID, err)
	}
}

func (w *Writer) linger(ctx context.Context) time.Duration {
	pressure, err := w.limiter.WritePressure(ctx)
	if err != nil || pressure >= busyPressure {
		return w.cfg.BusyLinger
	}
	return w.cfg.Linger
}

func (w *Writer) WriteSheet(ctx context.Context, sheetID string) error {
	probe, cancel := context.WithTimeout(ctx, leaseProbe)
	release, err := w.leases.Acquire(probe, "gs:sheet:"+sheetID)
	cancel()
	if err != nil {
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			w.metrics.AddGoogleSheetsEvent("lease", "busy")
			return nil
		}
		return err
	}
	defer func() {
		if _, err := release(); err != nil {
			log.Printf("[googlesheet] lease of sheet %s not released, it expires on its own: %v", sheetID, err)
		}
	}()
	sheet, err := w.sheets.Get(ctx, sheetID)
	if errors.Is(err, googlesheet.ErrSheetNotFound) {
		return w.rows.DeleteUnwritten(ctx, sheetID)
	}
	if err != nil {
		return err
	}
	if sheet.Writable() != nil {
		return nil
	}
	access, err := w.access.Access(ctx, sheet.WorkspaceID, googleaccount.ProductSheets)
	if errors.Is(err, googleaccount.ErrNotConnected) {
		w.metrics.AddGoogleSheetsEvent("access", "waiting_for_account")
		return nil
	}
	if err != nil {
		return err
	}
	if access.AccountID != sheet.AccountID {
		return w.attention(ctx, sheet, googlesheet.ReasonAccountChanged, "a conta Google conectada ao Planilhas não é a que criou esta planilha")
	}
	s := &session{w: w, sheet: sheet, access: access}
	if ok, err := s.recover(ctx); err != nil || !ok {
		return err
	}
	if ok, err := s.verify(ctx); err != nil || !ok {
		return err
	}
	deadline := w.now().Add(w.cfg.DrainBudget)
	for w.now().Before(deadline) {
		more, err := s.writeNext(ctx)
		if err != nil || !more {
			return err
		}
	}
	return nil
}

func (w *Writer) attention(ctx context.Context, sheet *googlesheet.Sheet, reason googlesheet.AttentionReason, detail string) error {
	sheet.NeedsAttention(reason, detail)
	if err := w.sheets.SaveStatus(ctx, sheet); err != nil {
		return err
	}
	w.metrics.AddGoogleSheetsEvent("attention", string(reason))
	if err := w.alerts.NeedsAttention(ctx, sheet); err != nil {
		log.Printf("[googlesheet] attention alert of %s not published: %v", sheet.ID, err)
	}
	return nil
}

type session struct {
	w      *Writer
	sheet  *googlesheet.Sheet
	access googleaccount.Access
}

func (s *session) take(ctx context.Context, write bool) (bool, error) {
	take, stage := s.w.limiter.TakeRead, "read_wait"
	if write {
		take, stage = s.w.limiter.TakeWrite, "write_wait"
	}
	granted, err := take(ctx, s.access.Email)
	if err != nil {
		return false, err
	}
	if !granted {
		s.w.metrics.AddGoogleSheetsEvent("limiter", stage)
	}
	return granted, nil
}

func (s *session) recover(ctx context.Context) (bool, error) {
	w := s.w
	stuck, err := w.rows.StuckInFlight(ctx, s.sheet.ID, w.now().Add(-w.cfg.LeaseTTL))
	if err != nil || len(stuck) == 0 {
		return err == nil, err
	}
	if ok, err := s.take(ctx, false); !ok || err != nil {
		return false, err
	}
	refs, err := w.api.ReadRange(ctx, s.access.Token, s.sheet.Target(), "A:A")
	if err != nil {
		return false, s.readFailed(ctx, err)
	}
	positions := make(map[string]int, len(refs))
	for i, row := range refs {
		if len(row) > 0 {
			positions[fmt.Sprint(row[0])] = i + 1
		}
	}
	var written []googlesheet.WrittenRow
	var missing []string
	for _, row := range stuck {
		if number, ok := positions[row.ID]; ok {
			written = append(written, googlesheet.WrittenRow{ID: row.ID, RowNumber: number})
			continue
		}
		missing = append(missing, row.ID)
	}
	if err := w.rows.MarkWritten(ctx, written, w.now()); err != nil {
		return false, err
	}
	if err := w.rows.Release(ctx, missing, w.now(), ""); err != nil {
		return false, err
	}
	w.metrics.AddGoogleSheetsEvent("recover", fmt.Sprintf("found_%d_of_%d", len(written), len(stuck)))
	return true, nil
}

func (s *session) verify(ctx context.Context) (bool, error) {
	w := s.w
	key := headerMarkKey(s.sheet.ID)
	due, err := w.marks.SetNX(key, "1", w.cfg.HeaderCheck)
	if err != nil || !due {
		return err == nil, err
	}
	ok, err := s.checkHeaderAndRoom(ctx)
	if !ok {
		if delErr := w.marks.Del(key); delErr != nil {
			log.Printf("[googlesheet] header check mark of %s not cleared: %v", s.sheet.ID, delErr)
		}
	}
	return ok, err
}

func (s *session) checkHeaderAndRoom(ctx context.Context) (bool, error) {
	w := s.w
	if ok, err := s.take(ctx, false); !ok || err != nil {
		return false, err
	}
	header, err := w.api.ReadRange(ctx, s.access.Token, s.sheet.Target(), "1:1")
	if err != nil {
		return false, s.readFailed(ctx, err)
	}
	if difference := googlesheet.HeaderDifference(s.sheet.HeaderRow(), header); difference != "" {
		return false, w.attention(ctx, s.sheet, googlesheet.ReasonHeaderMismatch, difference)
	}
	if ok, err := s.take(ctx, false); !ok || err != nil {
		return false, err
	}
	info, err := w.api.TabInfo(ctx, s.access.Token, s.sheet.Target())
	if err != nil {
		return false, s.readFailed(ctx, err)
	}
	if info.NearlyFull(0, len(s.sheet.Columns)+1) {
		return false, w.attention(ctx, s.sheet, googlesheet.ReasonSheetFull, fmt.Sprintf("%d linhas em %d colunas", info.Rows, info.Columns))
	}
	return true, nil
}

func (s *session) readFailed(ctx context.Context, err error) error {
	failure := s.w.errs.Classify(err)
	switch failure.Kind {
	case googlesheet.FailurePermission, googlesheet.FailureNotFound:
		return s.w.attention(ctx, s.sheet, failure.AttentionReason(), failure.Detail)
	}
	s.w.metrics.AddGoogleSheetsEvent("read", string(failure.Kind))
	return nil
}

func (s *session) writeNext(ctx context.Context) (bool, error) {
	w := s.w
	if ok, err := s.take(ctx, true); !ok || err != nil {
		return false, err
	}
	claimed, err := w.rows.Claim(ctx, s.sheet.ID, uuid.NewString(), w.cfg.BatchRows, w.now())
	if err != nil || len(claimed) == 0 {
		return false, err
	}
	batch, values, sizes := s.encode(ctx, claimed)
	fits := googlesheet.PlanBatch(sizes, w.cfg.BatchRows, w.cfg.BatchBytes)
	if fits < len(batch) {
		if err := w.rows.Unclaim(ctx, rowIDs(batch[fits:])); err != nil {
			return false, err
		}
		batch, values = batch[:fits], values[:fits]
	}
	if len(batch) == 0 {
		return true, nil
	}
	return s.send(ctx, batch, values)
}

func (s *session) encode(ctx context.Context, claimed []*googlesheet.Row) ([]*googlesheet.Row, [][]any, []int) {
	batch := make([]*googlesheet.Row, 0, len(claimed))
	values := make([][]any, 0, len(claimed))
	sizes := make([]int, 0, len(claimed))
	for _, row := range claimed {
		encoded, err := s.sheet.EncodeRow(row.ID, row.Cells)
		if err == nil {
			var raw []byte
			raw, err = json.Marshal(encoded)
			if err == nil {
				batch, values, sizes = append(batch, row), append(values, encoded), append(sizes, len(raw))
				continue
			}
		}
		if failErr := s.w.rows.Fail(ctx, row.ID, googlesheet.ErrorCode(err), err.Error()); failErr != nil {
			log.Printf("[googlesheet] row %s could not be encoded and not marked failed: %v", row.ID, failErr)
		}
	}
	return batch, values, sizes
}

func (s *session) send(ctx context.Context, batch []*googlesheet.Row, values [][]any) (bool, error) {
	w := s.w
	started := time.Now()
	result, err := w.api.AppendRows(ctx, s.access.Token, s.sheet.Target(), values)
	w.metrics.ObserveGoogleSheetsBatch(len(batch), time.Since(started).Seconds())
	if err == nil {
		return true, s.written(ctx, batch, result)
	}
	failure := w.errs.Classify(err)
	w.metrics.AddGoogleSheetsEvent("write", string(failure.Kind))
	ids := rowIDs(batch)
	switch googlesheet.Decide(failure, batch[0].Attempts) {
	case googlesheet.OutcomeRetry:
		wait := googlesheet.Backoff(batch[0].Attempts, w.jitter())
		if failure.RetryAfter > wait {
			wait = failure.RetryAfter
		}
		return false, w.rows.Release(ctx, ids, w.now().Add(wait), failure.Code())
	case googlesheet.OutcomeWaitForAccount:
		return false, w.rows.Release(ctx, ids, w.now().Add(w.cfg.AccountWait), failure.Code())
	case googlesheet.OutcomeHold:
		if err := w.rows.Hold(ctx, ids, failure.Code(), failure.Detail); err != nil {
			return false, err
		}
		return false, w.attention(ctx, s.sheet, failure.AttentionReason(), failure.Detail)
	}
	return s.isolate(ctx, batch, values, failure)
}

func (s *session) written(ctx context.Context, batch []*googlesheet.Row, result googlesheet.AppendResult) error {
	w := s.w
	rows := make([]googlesheet.WrittenRow, len(batch))
	for i, row := range batch {
		rows[i] = googlesheet.WrittenRow{ID: row.ID, RowNumber: result.FirstRow + i}
	}
	at := w.now()
	if err := w.rows.MarkWritten(ctx, rows, at); err != nil {
		return err
	}
	w.metrics.AddGoogleSheetsEvent("write", "written")
	return w.sheets.TouchWritten(ctx, s.sheet.ID, at)
}

func (s *session) isolate(ctx context.Context, batch []*googlesheet.Row, values [][]any, failure googlesheet.Failure) (bool, error) {
	w := s.w
	if len(batch) == 1 {
		return true, w.rows.Fail(ctx, batch[0].ID, failure.Code(), failure.Detail)
	}
	half := len(batch) / 2
	for _, part := range [][2]int{{0, half}, {half, len(batch)}} {
		start, end := part[0], part[1]
		if ok, err := s.take(ctx, true); !ok || err != nil {
			return false, errors.Join(err, w.rows.Unclaim(ctx, rowIDs(batch[start:])))
		}
		more, err := s.send(ctx, batch[start:end], values[start:end])
		if err != nil || !more {
			return false, errors.Join(err, w.rows.Unclaim(ctx, rowIDs(batch[end:])))
		}
	}
	return true, nil
}

func rowIDs(rows []*googlesheet.Row) []string {
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	return ids
}

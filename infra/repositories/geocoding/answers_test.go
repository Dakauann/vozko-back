package geocoding_repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"

	"vozko/domain/geo"
	"vozko/domain/geocoding"
)

var answeredAt = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func locatedAnswer(ws, fingerprint string) geocoding.Answer {
	fix := geo.Fix{Point: geo.Point{Lat: -23.5613, Lng: -46.6565}, Precision: geo.PrecisionAddress, Source: geo.SourceProvider, Provider: "opencage", FixedAt: answeredAt}
	answer, _ := geocoding.AnswerOf(geocoding.ProviderOpenCage, geocoding.AnswerKey{WorkspaceID: ws, Fingerprint: fingerprint}, geo.Located(fix), answeredAt)
	return answer
}

func TestAnswerStatementsBindEveryPlaceholder(t *testing.T) {
	for sql, want := range map[string]int{readAnswersSQL: 2, rememberAnswerSQL: 10} {
		if got := strings.Count(sql, "?"); got != want {
			t.Errorf("%q has %d placeholders, want %d", sql, got, want)
		}
	}
	for _, fragment := range []string{"holder.deleted_at IS NULL", "FOR SHARE OF a, holder", "a.workspace_id = ?::uuid AND a.fingerprint = ?", "ON CONFLICT (workspace_id, fingerprint) DO UPDATE"} {
		if !strings.Contains(rememberAnswerSQL, fragment) {
			t.Errorf("remember SQL misses %q: %s", fragment, rememberAnswerSQL)
		}
	}
}

func TestAnswersAreReadForTheWholeBatchInOneStatementPerWorkspaceAndText(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(exact(readAnswersSQL)).
		WithArgs(pq.StringArray{"ws-1", "ws-2"}, pq.StringArray{"f-1", "f-1"}).
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "fingerprint", "outcome", "latitude", "longitude", "geo_precision", "provider", "resolved_at"}).
			AddRow("ws-1", "f-1", "located", -23.5613, -46.6565, "address", "opencage", answeredAt).
			AddRow("ws-2", "f-1", "refused", nil, nil, nil, "opencage", answeredAt))
	got, err := NewAnswerStore(db).Answers(context.Background(), []geocoding.AnswerKey{
		{WorkspaceID: "ws-1", Fingerprint: "f-1"}, {WorkspaceID: "ws-2", Fingerprint: "f-1"}, {WorkspaceID: "ws-1", Fingerprint: "f-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	located := got[geocoding.AnswerKey{WorkspaceID: "ws-1", Fingerprint: "f-1"}]
	if located.Kind != geocoding.AnswerLocated || located.Fix == nil || located.Fix.Precision != geo.PrecisionAddress || located.Fix.Source != geo.SourceProvider ||
		located.Fix.Provider != "opencage" || !located.Fix.FixedAt.Equal(answeredAt) {
		t.Fatalf("located answer = %+v", located)
	}
	if refused := got[geocoding.AnswerKey{WorkspaceID: "ws-2", Fingerprint: "f-1"}]; refused.Kind != geocoding.AnswerRefused || refused.Fix != nil {
		t.Fatalf("refused answer = %+v", refused)
	}
}

func TestAnswersSkipARowThatIsNotAValidAnswer(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(exact(readAnswersSQL)).
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "fingerprint", "outcome", "latitude", "longitude", "geo_precision", "provider", "resolved_at"}).
			AddRow("ws-1", "f-1", "located", nil, nil, nil, "opencage", answeredAt).
			AddRow("ws-1", "f-2", "maybe", nil, nil, nil, "opencage", answeredAt))
	got, err := NewAnswerStore(db).Answers(context.Background(), []geocoding.AnswerKey{{WorkspaceID: "ws-1", Fingerprint: "f-1"}, {WorkspaceID: "ws-1", Fingerprint: "f-2"}})
	if err != nil || len(got) != 0 {
		t.Fatalf("Answers() = %+v, %v, want unusable rows treated as unknown", got, err)
	}
}

func TestAnswersOfNoKeysReadNothing(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	got, err := NewAnswerStore(db).Answers(context.Background(), nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("Answers(nil) = %+v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRememberWritesTheAnswerOnlyWhileALiveLeadHoldsTheText(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	answer := locatedAnswer("ws-1", "f-1")
	expectBoundedWait(mock)
	mock.ExpectExec(exact(rememberAnswerSQL)).
		WithArgs("ws-1", "f-1", "located", -23.5613, -46.6565, "address", "opencage", answeredAt, "ws-1", "f-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	refused, _ := geocoding.AnswerOf(geocoding.ProviderOpenCage, geocoding.AnswerKey{WorkspaceID: "ws-1", Fingerprint: "f-2"}, geo.Unavailable(geo.ReasonQueryRefused, 0), answeredAt)
	expectBoundedWait(mock)
	mock.ExpectExec(exact(rememberAnswerSQL)).
		WithArgs("ws-1", "f-2", "refused", nil, nil, nil, "opencage", answeredAt, "ws-1", "f-2").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	store := NewAnswerStore(db)
	if err := store.Remember(context.Background(), answer); err != nil {
		t.Fatal(err)
	}
	if err := store.Remember(context.Background(), refused); err != nil {
		t.Fatalf("a text no live lead holds any more is simply not stored: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRememberRefusesAnInvalidAnswerWithoutWriting(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	broken := locatedAnswer("ws-1", "f-1")
	broken.Fix = nil
	if err := NewAnswerStore(db).Remember(context.Background(), broken); err == nil {
		t.Fatal("an answer that is not valid must be refused")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func expectBoundedWait(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectExec(exact(answerLockTimeoutSQL)).WithArgs(answerLockTimeout).WillReturnResult(sqlmock.NewResult(0, 0))
}

func TestRememberGivesUpWhenTheHolderStaysLockedAndReportsIt(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	expectBoundedWait(mock)
	mock.ExpectExec(exact(rememberAnswerSQL)).WillReturnError(errors.New("pq: canceling statement due to lock timeout"))
	mock.ExpectRollback()
	if err := NewAnswerStore(db).Remember(context.Background(), locatedAnswer("ws-1", "f-1")); err == nil {
		t.Fatal("an answer that could not be stored in time must be reported as not stored")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if answerLockTimeout != "2s" || answerLockTimeoutSQL != "SELECT set_config('lock_timeout', ?, true)" {
		t.Fatalf("lock timeout = %q via %q, want a short wait local to the write", answerLockTimeout, answerLockTimeoutSQL)
	}
}

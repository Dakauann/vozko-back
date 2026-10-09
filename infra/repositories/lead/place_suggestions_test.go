package lead

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/lead"
)

func TestReadPlacesWithAPrefixSuggestsPlacesWhoseWordsStartWithIt(t *testing.T) {
	db, mock, _ := newMockDB(t)
	r := &repository{db: db, agg: newAggregateCache(nil)}
	membership := `la\.workspace_id = \$1 AND la\.is_primary AND la\.city_key IS NOT NULL AND la\.lead_id IN \(SELECT leads\.id FROM leads WHERE leads\.workspace_id = \$2 AND leads\.deleted_at IS NULL\)`
	expectMapSession(mock)
	mock.ExpectQuery(`FROM lead_addresses la WHERE `+membership+` AND \(la\.city_key LIKE \$3 OR la\.city_key LIKE \$4\) GROUP BY la\.city_key, la\.city, la\.state\) g GROUP BY g\.city_key ORDER BY count DESC, g\.city_key LIMIT \$5$`).
		WithArgs(pageWorkspace, pageWorkspace, "%:santo%", "% santo%", lead.MaxPlaceSuggestions).
		WillReturnRows(sqlmock.NewRows([]string{"city_key", "city", "state", "count"}).AddRow("sp:santo andre", "Santo André", "SP", 3))
	mock.ExpectQuery(`FROM lead_addresses la WHERE `+membership+` AND la\.district_key IS NOT NULL AND \(la\.district_key LIKE \$3 OR la\.district_key LIKE \$4\) GROUP BY .* LIMIT \$5$`).
		WithArgs(pageWorkspace, pageWorkspace, "santo%", "% santo%", lead.MaxPlaceSuggestions).
		WillReturnRows(sqlmock.NewRows([]string{"city_key", "district_key", "district", "city", "state", "count"}).AddRow("rn:natal", "santo antonio", "Santo Antônio", "Natal", "RN", 2))
	mock.ExpectCommit()

	q := sectionQuery()
	q.PlacePrefix = "santo"
	got, err := r.ReadPlaces(context.Background(), q)
	if err != nil {
		t.Fatalf("ReadPlaces() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if len(got.Districts) != 1 || got.Districts[0].Pair != "rn:natal/santo antonio" || len(got.Cities) != 1 {
		t.Fatalf("suggestions = %+v", got)
	}
}

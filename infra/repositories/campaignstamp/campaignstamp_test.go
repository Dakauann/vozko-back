package campaignstamp

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"gorm.io/gorm/clause"

	"vozko/domain/campaign"
)

func keys(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestStampKeepsTheFirstMomentOfEveryMilestoneTheStatusProves(t *testing.T) {
	at := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	got := Stamp(campaign.SendStatusRead, at)

	if want := []string{"delivered_at", "read_at", "sent_at"}; !reflect.DeepEqual(keys(got), want) {
		t.Fatalf("Stamp(READ) columns = %v, want %v", keys(got), want)
	}
	for column, value := range got {
		expr, ok := value.(clause.Expr)
		if !ok {
			t.Fatalf("%s = %T, want a COALESCE expression so a replayed webhook cannot move it", column, value)
		}
		if want := "COALESCE(" + column + ", ?)"; expr.SQL != want {
			t.Fatalf("%s SQL = %q, want %q", column, expr.SQL, want)
		}
		if !reflect.DeepEqual(expr.Vars, []interface{}{at}) {
			t.Fatalf("%s vars = %v, want [%v]", column, expr.Vars, at)
		}
	}
}

func TestStampIsEmptyForAStatusThatProvesNoMoment(t *testing.T) {
	if got := Stamp(campaign.SendStatusPending, time.Now()); len(got) != 0 {
		t.Fatalf("Stamp(PENDING) = %v, want no columns", got)
	}
}

func TestClearNullsEveryMilestoneColumn(t *testing.T) {
	got := Clear()

	if want := []string{"delivered_at", "failed_at", "read_at", "sent_at"}; !reflect.DeepEqual(keys(got), want) {
		t.Fatalf("Clear() columns = %v, want %v", keys(got), want)
	}
	for column, value := range got {
		if value != nil {
			t.Fatalf("Clear()[%s] = %v, want nil", column, value)
		}
	}
}

func TestWithStampMergesTheStatusUpdateAndTheMilestones(t *testing.T) {
	at := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	got := WithStamp(map[string]interface{}{"status": "FAILED"}, campaign.SendStatusFailed, at)

	if want := []string{"failed_at", "status"}; !reflect.DeepEqual(keys(got), want) {
		t.Fatalf("WithStamp columns = %v, want %v", keys(got), want)
	}
}

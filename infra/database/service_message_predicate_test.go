package database

import (
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

// Postgres only uses a partial index when it can prove the query's WHERE
// implies the index's predicate. The query predicate must therefore be the
// index predicate plus extra conditions, never a different one, and both must
// be spelled identically.

// The query predicate must literally contain the index predicate. Anything else
// and the planner has nothing to match, which costs a full scan of three
// million rows that no test would notice.
func TestQueryPredicateContainsTheIndexPredicate(t *testing.T) {
	index := ServiceMessageIndexPredicateSQL("cm")
	query := ServiceMessagePredicateSQL("cm")

	if !strings.Contains(query, index) {
		t.Errorf("the query predicate no longer contains the index predicate.\nindex:\n%s\nquery:\n%s", index, query)
	}
}

// The query narrows further than the index, on delivery status, and that is
// deliberate: delivery_status mutates on every status webhook, so keying the
// index on it would churn the index on the busiest write path we have.
func TestQueryNarrowsOnDeliveryButTheIndexDoesNot(t *testing.T) {
	index := ServiceMessageIndexPredicateSQL("cm")
	query := ServiceMessagePredicateSQL("cm")

	if strings.Contains(index, "delivery_status") {
		t.Error("delivery_status is in the index predicate; it mutates per webhook and would churn the index")
	}
	if !strings.Contains(query, "delivery_status") {
		t.Error("the query does not filter on delivery_status; Meta charges on delivery")
	}
	for _, status := range conversation.BillableDeliveryStatusStrings() {
		if !strings.Contains(query, "'"+status+"'") {
			t.Errorf("delivery status %q missing from the query predicate", status)
		}
	}
}

// Coexistence puts the WhatsApp Business app and the Cloud API on one number.
// A reply the owner types in the app is echoed to us and stored exactly like
// one of ours, and Meta does not bill it: only what goes out through the API is
// billed. Counting it invents a cost.
func TestCoexistenceEchoesAreExcluded(t *testing.T) {
	for _, alias := range []string{"", "cm"} {
		predicate := ServiceMessageIndexPredicateSQL(alias)
		if !strings.Contains(predicate, "'"+string(conversation.MessageTransportBusinessApp)+"'") {
			t.Errorf("alias %q: business app echoes are not excluded; Meta does not charge for them", alias)
		}
		// NULL <> 'business_app' is NULL, not true, so a bare comparison would
		// silently drop every row written before the column existed.
		if !strings.Contains(predicate, "COALESCE") {
			t.Errorf("alias %q: the transport test must survive NULL, or all history vanishes", alias)
		}
	}
}

// The index is written against the bare table and cannot use an alias; the
// query joins four tables and must. Same rule, two renderings, and the aliased
// one has to qualify every column or it is ambiguous SQL.
func TestAliasQualifiesEveryColumn(t *testing.T) {
	aliased := ServiceMessagePredicateSQL("cm")
	for _, column := range []string{
		"deleted_at", "entry_type", "direction", "message_type", "sent_via", "delivery_status",
	} {
		if !strings.Contains(aliased, "cm."+column) {
			t.Errorf("%q is not qualified with the alias in the aliased rendering", column)
		}
	}

	bare := ServiceMessageIndexPredicateSQL("")
	if strings.Contains(bare, "cm.") {
		t.Error("the bare rendering carries an alias; an index predicate cannot")
	}
	if !strings.Contains(bare, "deleted_at IS NULL") {
		t.Error("the bare rendering lost its columns")
	}
}

// Nothing in the predicate may be a bind parameter. A bound value cannot be
// proven to imply a constant index predicate under a generic plan, which a
// prepared statement reaches after a handful of executions.
func TestPredicateBindsNothing(t *testing.T) {
	for _, predicate := range []string{
		ServiceMessageIndexPredicateSQL(""),
		ServiceMessagePredicateSQL("cm"),
	} {
		if strings.Contains(predicate, "?") || strings.Contains(predicate, "$1") {
			t.Errorf("predicate binds a parameter, which defeats the partial index:\n%s", predicate)
		}
	}
}

// The values come from the domain, not from literals typed here.
func TestPredicateUsesTheDomainConstants(t *testing.T) {
	predicate := ServiceMessagePredicateSQL("cm")

	if !strings.Contains(predicate, "'"+string(shared.EntryTypeWhatsApp)+"'") {
		t.Error("entry type is not the domain's constant")
	}
	if !strings.Contains(predicate, "'"+string(conversation.MessageDirectionOutbound)+"'") {
		t.Error("direction is not the domain's constant")
	}
	for _, mt := range conversation.ServiceMessageTypeStrings() {
		if !strings.Contains(predicate, "'"+mt+"'") {
			t.Errorf("message type %q missing from the predicate", mt)
		}
	}
	// Unofficial WhatsApp is a linked device session and Meta does not bill it;
	// Instagram and Telegram are not Meta WhatsApp traffic at all. All three are
	// excluded by entry_type alone, so the predicate must never widen to them.
	for _, other := range []shared.EntryType{
		shared.EntryTypeUnofficialWhatsApp,
		shared.EntryTypeInstagram,
		shared.EntryTypeTelegram,
	} {
		if strings.Contains(predicate, "'"+string(other)+"'") {
			t.Errorf("%q appears in the predicate; only official WhatsApp is billed by Meta", other)
		}
	}
}

// Quote doubling, so a domain constant containing an apostrophe could never
// terminate the literal it is rendered into.
func TestLiteralRenderingEscapes(t *testing.T) {
	if got := SQLStringLiteral("o'brien"); got != "'o''brien'" {
		t.Errorf("SQLStringLiteral = %q, want the apostrophe doubled", got)
	}
	if got := SQLStringLiteralList([]string{"a", "b'c"}); got != "'a', 'b''c'" {
		t.Errorf("SQLStringLiteralList = %q", got)
	}
}

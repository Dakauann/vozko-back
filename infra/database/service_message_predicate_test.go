package database

import (
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

func TestQueryPredicateContainsTheIndexPredicate(t *testing.T) {
	index := ServiceMessageIndexPredicateSQL("cm")
	query := ServiceMessagePredicateSQL("cm")

	if !strings.Contains(query, index) {
		t.Errorf("the query predicate no longer contains the index predicate.\nindex:\n%s\nquery:\n%s", index, query)
	}
}

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

func TestCoexistenceEchoesAreExcluded(t *testing.T) {
	for _, alias := range []string{"", "cm"} {
		predicate := ServiceMessageIndexPredicateSQL(alias)
		if !strings.Contains(predicate, "'"+string(conversation.MessageTransportBusinessApp)+"'") {
			t.Errorf("alias %q: business app echoes are not excluded; Meta does not charge for them", alias)
		}
		if !strings.Contains(predicate, "COALESCE") {
			t.Errorf("alias %q: the transport test must survive NULL, or all history vanishes", alias)
		}
	}
}

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

func TestLiteralRenderingEscapes(t *testing.T) {
	if got := SQLStringLiteral("o'brien"); got != "'o''brien'" {
		t.Errorf("SQLStringLiteral = %q, want the apostrophe doubled", got)
	}
	if got := SQLStringLiteralList([]string{"a", "b'c"}); got != "'a', 'b''c'" {
		t.Errorf("SQLStringLiteralList = %q", got)
	}
}

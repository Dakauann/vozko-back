package conversation_repository

import (
	"strings"
	"testing"

	"vozko/domain/shared"
)

func TestMercadoLivreReadPathsUseTheDescriptorTables(t *testing.T) {
	src := sourceFor(t, shared.EntryTypeMercadoLivre)
	if src.From != "mercadolivre_conversations mlc" || !strings.Contains(src.WorkspaceJoin, "mercadolivre_accounts mla") || src.Department != "mla.department_id" {
		t.Fatalf("entry source = %+v", src)
	}
	q, ok := channelQueryFor(shared.EntryTypeMercadoLivre)
	if !ok {
		t.Fatal("no channel query for mercadolivre")
	}
	if q.EntryTable != "mercadolivre_conversations" || q.StatusColumn != "mlc.conversation_status" || q.AutomationColumn != "mlc.automation_enabled" {
		t.Fatalf("channel query = %+v", q)
	}
	for _, fragment := range []string{"window_open", "outbound_budget", "mla_w.status = 'CONNECTED'", "deleted_at IS NULL"} {
		if !strings.Contains(q.windowOpenOn("x.entry_id"), fragment) {
			t.Errorf("window subquery misses %q: %s", fragment, q.windowOpenOn("x.entry_id"))
		}
	}
	if strings.Contains(q.windowOpenOn("x.entry_id"), "INTERVAL") {
		t.Error("the window comes from the mirrored thread status, never a clock")
	}
	if strings.Count(q.windowOpenOn("x.entry_id"), "?") != 0 || strings.Count(q.ContainerCTE, "?") != 1 || strings.Count(q.ContainerFilter, "?") != 1 {
		t.Errorf("placeholder counts are off: %+v", q)
	}
	if contactRefUUID(shared.EntryTypeMercadoLivre) != "mlc.contact_id" {
		t.Errorf("contact slot = %q", contactRefUUID(shared.EntryTypeMercadoLivre))
	}
	if !strings.Contains(q.ContactJoin, "mercadolivre_contacts") || !strings.Contains(q.ContactJoin, "AS blocked") {
		t.Errorf("contact join = %s", q.ContactJoin)
	}
}

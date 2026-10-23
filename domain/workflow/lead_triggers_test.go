package workflow

import (
	"testing"
	"time"
)

func TestBirthdayIsALeadTrigger(t *testing.T) {
	if !TriggerBirthday.Valid() || !NodeType(TriggerBirthday).Valid() || !NodeType(TriggerBirthday).IsTrigger() || TriggerBirthday.WorkflowType() != WorkflowTypeMessages {
		t.Fatal("the birthday trigger must be a valid message trigger node")
	}
	if _, ok := EntryKindOfTrigger(TriggerBirthday); !ok {
		t.Fatal("the birthday trigger needs an entry point kind")
	}
	event := BirthdayEvent(LeadSubject{WorkspaceID: "ws-1", LeadID: "lead-1"}, time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC))
	if !event.StartsOnLead() || !event.StartsRunPerEvent() {
		t.Fatal("a birthday starts one run per event on a lead")
	}
	for _, conversation := range []TriggerEvent{
		{TriggerType: TriggerFormSubmitted, EntryType: "whatsapp"},
		{TriggerType: TriggerMessageReceived, EntryType: "whatsapp"},
		{TriggerType: TriggerWebhook, EntryType: "whatsapp"},
	} {
		if conversation.StartsOnLead() {
			t.Fatalf("%s on a conversation does not start on a lead", conversation.TriggerType)
		}
	}
	if !(TriggerEvent{TriggerType: TriggerFormSubmitted, EntryType: "whatsapp"}).StartsRunPerEvent() {
		t.Fatal("form answers still start one run per event")
	}
}

func TestBirthdayDueWaitsForTheConfiguredHour(t *testing.T) {
	loc := time.FixedZone("BRT", -3*3600)
	at := func(hour int) time.Time { return time.Date(2026, 10, 21, hour, 30, 0, 0, loc) }
	cases := []struct {
		name   string
		config map[string]interface{}
		now    time.Time
		due    bool
	}{
		{"default hour not reached", map[string]interface{}{}, at(8), false},
		{"default hour reached", map[string]interface{}{}, at(9), true},
		{"later in the day still fires", nil, at(22), true},
		{"configured hour as a JSON number", map[string]interface{}{BirthdayHourKey: float64(14)}, at(13), false},
		{"configured hour reached", map[string]interface{}{BirthdayHourKey: float64(14)}, at(14), true},
		{"midnight", map[string]interface{}{BirthdayHourKey: float64(0)}, at(0), true},
		{"an hour out of range never fires", map[string]interface{}{BirthdayHourKey: float64(24)}, at(23), false},
		{"a fractional hour never fires", map[string]interface{}{BirthdayHourKey: 9.5}, at(23), false},
		{"an unreadable hour never fires", map[string]interface{}{BirthdayHourKey: "nove"}, at(23), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BirthdayDue(tc.config, tc.now); got != tc.due {
				t.Fatalf("BirthdayDue = %v, want %v", got, tc.due)
			}
		})
	}
}

func TestBirthdayEventStartsOnTheLeadOncePerDay(t *testing.T) {
	subject := LeadSubject{WorkspaceID: "ws-1", LeadID: "lead-1", LeadName: "Ana"}
	day := time.Date(2026, 10, 21, 9, 0, 0, 0, time.FixedZone("BRT", -3*3600))
	event := BirthdayEvent(subject, day)
	if event.EntryID != "lead-1" || event.EntryType != EntryTypeLead || event.WorkspaceID != "ws-1" || event.TriggerType != TriggerBirthday {
		t.Fatalf("event = %+v", event)
	}
	if event.Data[DataKeyLeadID] != "lead-1" || event.Data[DataKeyLeadName] != "Ana" || event.Data[DataKeyBirthday] != "2026-10-21" {
		t.Fatalf("data = %+v", event.Data)
	}
	if event.OccurrenceKey() != "birthday/lead-1/2026-10-21" {
		t.Fatalf("key = %q", event.OccurrenceKey())
	}
	if BirthdayEvent(subject, day.Add(24*time.Hour)).OccurrenceKey() == event.OccurrenceKey() {
		t.Fatal("another day is another occurrence")
	}
}

func TestALeadEventWithoutItsDedupFactsHasNoOccurrenceKey(t *testing.T) {
	cases := []TriggerEvent{
		{TriggerType: TriggerBirthday, EntryType: EntryTypeLead, EntryID: "lead-1", Data: map[string]interface{}{}},
		{TriggerType: TriggerBirthday, EntryType: EntryTypeLead, Data: map[string]interface{}{DataKeyBirthday: "2026-10-21"}},
		{TriggerType: TriggerWebhook, EntryType: EntryTypeLead, EntryID: "lead-1"},
	}
	for _, event := range cases {
		if key := event.OccurrenceKey(); key != "" {
			t.Fatalf("key = %q for %+v", key, event)
		}
	}
}

func TestALeadRunHasNoConversation(t *testing.T) {
	run := &WorkflowRun{EntryType: EntryTypeLead}
	if run.OnConversation() || !run.OnLead() {
		t.Fatal("a lead run is not on a conversation")
	}
	if (&WorkflowRun{EntryType: "whatsapp"}).OnLead() {
		t.Fatal("a conversation run is not a lead run")
	}
}

func TestLeadTriggerDefinitionsAreBuiltIn(t *testing.T) {
	defs := NodeCatalogMap(BuiltinDefinitions())
	birthday, ok := defs[NodeTypeTriggerBirthday]
	if !ok || birthday.Category != NodeCategoryTrigger {
		t.Fatal("the birthday trigger needs a definition")
	}
	if birthday.DefaultConfig[BirthdayHourKey] != DefaultBirthdayHour {
		t.Fatalf("default hour = %v", birthday.DefaultConfig[BirthdayHourKey])
	}
}

func TestLintRefusesABirthdayHourOutOfRange(t *testing.T) {
	g := &Graph{Nodes: []Node{
		{ID: "t", Type: NodeTypeTriggerBirthday, Config: map[string]interface{}{BirthdayHourKey: float64(25)}},
		{ID: "e", Type: NodeTypeEnd},
	}, Edges: []Edge{{Source: "t", Target: "e"}}}
	report := LintGraph(g, WorkflowTypeMessages, BuiltinDefinitions(), nil, nil)
	if !hasIssue(report, LintFieldOutOfRange, "t") {
		t.Fatalf("issues = %+v", report.Issues)
	}
}

func hasIssue(report LintReport, code LintIssueCode, nodeID string) bool {
	for _, issue := range report.Issues {
		if issue.Code == code && issue.NodeID == nodeID {
			return true
		}
	}
	return false
}

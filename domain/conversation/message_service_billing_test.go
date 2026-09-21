package conversation

import "testing"

// From 1 October 2026 Meta charges for SERVICE messages: "any non-template
// message that is not powered by Meta Business Agent", sent by a human agent or
// by a third-party AI. We absorb that cost, so we have to be able to count it
// per workspace before we can price it.
//
// The rule lives here, once, because two very different consumers read it: the
// reporting SQL, and the partial index that serves the reporting SQL. If those
// two ever disagree the index silently stops matching and the report goes back
// to a full table scan.

// The trap this file exists for. tool_call and tool_result are AI internals: a
// model asking for a price lookup and the answer coming back. They are stored
// with direction OUTBOUND and no delivery status, and they never left the
// building, so Meta cannot bill them. Production writes about 25.600 of them a
// month on the official WhatsApp channel, which is what a naive "outbound and
// not a template" count would invent as cost.
func TestAIInternalsAreNotServiceMessages(t *testing.T) {
	for _, mt := range []MessageType{MessageTypeToolCall, MessageTypeToolResult} {
		if mt.IsMetaServiceBillable() {
			t.Errorf("%q counted as a service message; it never left the building", mt)
		}
	}
}

// The same trap, smaller: call lifecycle markers are a phone log written into
// the conversation, and several of them are persisted OUTBOUND. They are events,
// not messages, and IsCallEvent already knows it.
func TestCallMarkersAreNotServiceMessages(t *testing.T) {
	markers := []MessageType{
		MessageTypeCallReceived,
		MessageTypeCallAnswered,
		MessageTypeCallMissed,
		MessageTypeCallEnded,
		MessageTypeCallPermissionRequest,
		MessageTypeCallPermissionGranted,
		MessageTypeCallPermissionRejected,
	}
	for _, mt := range markers {
		if mt.IsMetaServiceBillable() {
			t.Errorf("%q counted as a service message; it is a call log marker", mt)
		}
	}
	// Belt and braces: anything IsCallEvent knows about must be excluded, so a
	// marker added later cannot leak into the bill by being forgotten here.
	for _, mt := range markers {
		if !mt.IsCallEvent() {
			t.Errorf("%q is no longer an IsCallEvent; this test is now lying", mt)
		}
	}
}

// A template send is already billed to the customer as a campaign send, and
// Meta prices it on the template rate card, not the service rate. Counting it
// here would double count the same message on both sides of the comparison the
// report exists to make.
func TestTemplatesAreNotServiceMessages(t *testing.T) {
	if MessageTypeTemplate.IsMetaServiceBillable() {
		t.Error("template counted as a service message; it is already a billed campaign send")
	}
}

// Inbound is free at Meta, always. These types can only arrive, so even though
// the reporting query also filters on direction, the type predicate must not
// claim them.
func TestInboundOnlyTypesAreNotServiceMessages(t *testing.T) {
	for _, mt := range InboundMessageTypes() {
		// media and audio are the exception: they are genuinely two-way, an
		// agent attaching a file is a service message.
		if mt == MessageTypeMedia || mt == MessageTypeAudio {
			continue
		}
		if mt.IsMetaServiceBillable() {
			t.Errorf("%q counted as a service message; inbound is never billed", mt)
		}
	}
}

// What we actually pay for. operator is an agent typing, ai_response is our AI
// replying (Meta bills third-party AI exactly like a human), media is an agent
// attaching a file. Together these are 319.134 messages a month in production
// and they are the entire number this report reports.
func TestAgentAndAIRepliesAreServiceMessages(t *testing.T) {
	want := []MessageType{
		MessageTypeOperator,
		MessageTypeAIResponse,
		MessageTypeMedia,
		MessageTypeAudio,
	}
	for _, mt := range want {
		if !mt.IsMetaServiceBillable() {
			t.Errorf("%q not counted as a service message; we pay Meta for it", mt)
		}
	}
}

// ServiceMessageTypes is what the SQL and the index predicate are both built
// from, so it has to agree with the predicate rather than be a second list
// maintained by hand.
func TestServiceMessageTypesAgreesWithThePredicate(t *testing.T) {
	listed := make(map[MessageType]bool)
	for _, mt := range ServiceMessageTypes() {
		if listed[mt] {
			t.Errorf("%q listed twice; a duplicate would widen the index predicate for nothing", mt)
		}
		listed[mt] = true
		if !mt.IsMetaServiceBillable() {
			t.Errorf("%q is listed but the predicate rejects it", mt)
		}
	}

	// Every type the predicate accepts must appear in the list, or the report
	// undercounts exactly the messages the index was told to skip. Driven from
	// the registry, which message_type_registry_test.go proves is complete, so a
	// type added later cannot slip past by not being written out here.
	for _, mt := range AllMessageTypes() {
		if mt.IsMetaServiceBillable() && !listed[mt] {
			t.Errorf("%q is billable but missing from ServiceMessageTypes()", mt)
		}
	}
}

// The strings feed a SQL IN list and a partial index predicate. They must be
// the literal column values, in the same order, with nothing lost in the
// conversion. Mirrors InboundMessageTypeStrings, which exists for the same
// reason.
func TestServiceMessageTypeStringsMirrorsTheTypes(t *testing.T) {
	types := ServiceMessageTypes()
	strs := ServiceMessageTypeStrings()

	if len(types) != len(strs) {
		t.Fatalf("got %d strings for %d types", len(strs), len(types))
	}
	for i := range types {
		if strs[i] != string(types[i]) {
			t.Errorf("index %d: string %q does not match type %q", i, strs[i], types[i])
		}
	}
}

// Meta charges on DELIVERY. A message the provider rejected costs nothing, and
// production shows 5.480 failed sends a month among exactly these types, so
// including them would bill us for messages that never arrived.
func TestFailedDeliveryIsNotBillable(t *testing.T) {
	if DeliveryStatusFailed.IsMetaBillable() {
		t.Error("failed delivery counted as billable; Meta charges on delivery")
	}
}

// The empty status is what an internal row carries (tool_call, tool_result and
// the call markers all have it) and also what a row written before the column
// existed carries. Either way we cannot say Meta delivered it, and the safe
// reading of "not stated" is not to bill for it.
func TestUnstatedDeliveryIsNotBillable(t *testing.T) {
	if DeliveryStatusNone.IsMetaBillable() {
		t.Error("empty delivery status counted as billable; nothing says it was delivered")
	}
}

// sent, delivered and read are the three states a message that reached Meta
// passes through. All three are billable: "sent" already means Meta accepted it.
func TestAcceptedDeliveryStatusesAreBillable(t *testing.T) {
	for _, s := range []DeliveryStatus{DeliveryStatusSent, DeliveryStatusDelivered, DeliveryStatusRead} {
		if !s.IsMetaBillable() {
			t.Errorf("%q not billable; Meta charges once it accepts the message", s)
		}
	}
}

// Same contract as the type list: the statuses that feed the SQL IN list must
// be exactly the statuses the predicate accepts.
func TestBillableDeliveryStatusesAgreeWithThePredicate(t *testing.T) {
	listed := make(map[DeliveryStatus]bool)
	for _, s := range BillableDeliveryStatuses() {
		listed[s] = true
		if !s.IsMetaBillable() {
			t.Errorf("%q is listed but the predicate rejects it", s)
		}
	}
	for _, s := range []DeliveryStatus{
		DeliveryStatusNone, DeliveryStatusSent, DeliveryStatusDelivered,
		DeliveryStatusRead, DeliveryStatusFailed,
	} {
		if s.IsMetaBillable() && !listed[s] {
			t.Errorf("%q is billable but missing from BillableDeliveryStatuses()", s)
		}
	}

	strs := BillableDeliveryStatusStrings()
	statuses := BillableDeliveryStatuses()
	if len(strs) != len(statuses) {
		t.Fatalf("got %d strings for %d statuses", len(strs), len(statuses))
	}
	for i := range statuses {
		if strs[i] != string(statuses[i]) {
			t.Errorf("index %d: string %q does not match status %q", i, strs[i], statuses[i])
		}
	}
}

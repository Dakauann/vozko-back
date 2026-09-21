package conversation

import "testing"

func TestAIInternalsAreNotServiceMessages(t *testing.T) {
	for _, mt := range []MessageType{MessageTypeToolCall, MessageTypeToolResult} {
		if mt.IsMetaServiceBillable() {
			t.Errorf("%q counted as a service message; it never left the building", mt)
		}
	}
}

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
	for _, mt := range markers {
		if !mt.IsCallEvent() {
			t.Errorf("%q is no longer an IsCallEvent; this test is now lying", mt)
		}
	}
}

func TestTemplatesAreNotServiceMessages(t *testing.T) {
	if MessageTypeTemplate.IsMetaServiceBillable() {
		t.Error("template counted as a service message; it is already a billed campaign send")
	}
}

func TestInboundOnlyTypesAreNotServiceMessages(t *testing.T) {
	for _, mt := range InboundMessageTypes() {
		if mt == MessageTypeMedia || mt == MessageTypeAudio {
			continue
		}
		if mt.IsMetaServiceBillable() {
			t.Errorf("%q counted as a service message; inbound is never billed", mt)
		}
	}
}

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

	for _, mt := range AllMessageTypes() {
		if mt.IsMetaServiceBillable() && !listed[mt] {
			t.Errorf("%q is billable but missing from ServiceMessageTypes()", mt)
		}
	}
}

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

func TestFailedDeliveryIsNotBillable(t *testing.T) {
	if DeliveryStatusFailed.IsMetaBillable() {
		t.Error("failed delivery counted as billable; Meta charges on delivery")
	}
}

func TestUnstatedDeliveryIsNotBillable(t *testing.T) {
	if DeliveryStatusNone.IsMetaBillable() {
		t.Error("empty delivery status counted as billable; nothing says it was delivered")
	}
}

func TestAcceptedDeliveryStatusesAreBillable(t *testing.T) {
	for _, s := range []DeliveryStatus{DeliveryStatusSent, DeliveryStatusDelivered, DeliveryStatusRead} {
		if !s.IsMetaBillable() {
			t.Errorf("%q not billable; Meta charges once it accepts the message", s)
		}
	}
}

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

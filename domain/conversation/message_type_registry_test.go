package conversation

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// Go has no way to enumerate the members of a string enum, so a rule written as
// an allowlist over MessageType silently ignores any type added after it.
//
// That matters here more than usual. ServiceMessageTypes decides what we bill
// ourselves for from 1 October 2026, and a type it has not been told about is
// excluded by default. The failure is invisible: the report simply reports a
// smaller number, every test stays green, and nobody finds out until the
// invoice does not match.
//
// So this parses the source and makes the compiler's own list of constants the
// thing tests check against, instead of a list somebody has to remember to
// update.

// declaredMessageTypes reads message.go and returns every constant declared
// with the MessageType type.
func declaredMessageTypes(t *testing.T) []string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "message.go", nil, 0)
	if err != nil {
		t.Fatalf("parse message.go: %v", err)
	}

	var declared []string
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST {
			continue
		}
		// A const block states its type once, on the first spec, and the rest
		// inherit it. Carrying it forward is what makes the grouped
		// declarations in message.go readable here.
		currentType := ""
		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if ident, ok := valueSpec.Type.(*ast.Ident); ok {
				currentType = ident.Name
			}
			if currentType != "MessageType" {
				continue
			}
			for _, name := range valueSpec.Names {
				declared = append(declared, name.Name)
			}
		}
	}

	if len(declared) == 0 {
		t.Fatal("parsed no MessageType constants; this test can no longer protect anything")
	}
	return declared
}

// The registry has to list every declared constant. When it does, every other
// rule in this package can iterate it and be sure it saw everything.
//
// If this fails you have just added a MessageType. Add it to AllMessageTypes,
// then decide whether Meta charges us for it: see IsMetaServiceBillable.
func TestEveryDeclaredMessageTypeIsInTheRegistry(t *testing.T) {
	registered := make(map[MessageType]bool)
	for _, mt := range AllMessageTypes() {
		registered[mt] = true
	}

	// Constant NAME to the value it holds, so the failure can name the
	// identifier a developer just wrote rather than its string value.
	byName := map[string]MessageType{
		"MessageTypeUserMessage":            MessageTypeUserMessage,
		"MessageTypeAIResponse":             MessageTypeAIResponse,
		"MessageTypeToolCall":               MessageTypeToolCall,
		"MessageTypeToolResult":             MessageTypeToolResult,
		"MessageTypeAudio":                  MessageTypeAudio,
		"MessageTypeSystem":                 MessageTypeSystem,
		"MessageTypeMedia":                  MessageTypeMedia,
		"MessageTypeOperator":               MessageTypeOperator,
		"MessageTypeTemplate":               MessageTypeTemplate,
		"MessageTypeCallPermissionRequest":  MessageTypeCallPermissionRequest,
		"MessageTypeCallPermissionGranted":  MessageTypeCallPermissionGranted,
		"MessageTypeCallPermissionRejected": MessageTypeCallPermissionRejected,
		"MessageTypeCallReceived":           MessageTypeCallReceived,
		"MessageTypeCallAnswered":           MessageTypeCallAnswered,
		"MessageTypeCallMissed":             MessageTypeCallMissed,
		"MessageTypeCallEnded":              MessageTypeCallEnded,
		"MessageTypeStoryReply":             MessageTypeStoryReply,
		"MessageTypeStoryMention":           MessageTypeStoryMention,
		"MessageTypeReaction":               MessageTypeReaction,
		"MessageTypeUnsupported":            MessageTypeUnsupported,
		"MessageTypePostShare":              MessageTypePostShare,
	}

	for _, name := range declaredMessageTypes(t) {
		value, known := byName[name]
		if !known {
			t.Errorf("%s is declared in message.go but this test does not know it.\n"+
				"Add it to AllMessageTypes() and to the byName map here, then decide "+
				"whether Meta bills us for it in IsMetaServiceBillable.", name)
			continue
		}
		if !registered[value] {
			t.Errorf("%s is declared but missing from AllMessageTypes().\n"+
				"Until it is listed, the service message rule cannot see it and we "+
				"under-report what Meta charges us.", name)
		}
	}
}

// The registry must also not name a type twice, which would make any rule that
// counts by iterating it double count.
func TestRegistryHasNoDuplicates(t *testing.T) {
	seen := make(map[MessageType]bool)
	for _, mt := range AllMessageTypes() {
		if seen[mt] {
			t.Errorf("%q appears twice in AllMessageTypes()", mt)
		}
		seen[mt] = true
	}
}

// Every type in the registry must be a deliberate yes or a deliberate no, and
// the no's must be justified by a reason this package already understands.
//
// This is the test that would have caught a sticker type. A new MessageType is
// in the registry (the test above forces that), reaches here, matches none of
// the known exclusions, is not billable, and fails with a message saying so.
func TestEveryMessageTypeIsDeliberatelyClassified(t *testing.T) {
	// The reasons a type is legitimately not a billable service message. Each
	// is a property the type already has, not a second list to maintain.
	excused := func(mt MessageType) (string, bool) {
		switch {
		case mt == MessageTypeTemplate:
			return "billed to the customer as a campaign send, on Meta's template rate card", true
		case mt == MessageTypeToolCall, mt == MessageTypeToolResult:
			return "AI internals that never leave the building", true
		case mt.IsCallEvent():
			return "a call log marker, not a message", true
		case mt == MessageTypeSystem:
			return "a platform notice we generate, never sent to the contact", true
		case mt.IsInbound():
			return "inbound, and Meta never charges for what arrives", true
		case mt == MessageTypeReaction, mt == MessageTypeUnsupported:
			return "not a message a business sends on the official WhatsApp channel", true
		}
		return "", false
	}

	for _, mt := range AllMessageTypes() {
		if mt.IsMetaServiceBillable() {
			continue
		}
		if _, ok := excused(mt); !ok {
			t.Errorf("%q is neither billable nor excused.\n"+
				"Decide explicitly: either add it to ServiceMessageTypes (and rebuild "+
				"idx_cm_service_exposure, whose predicate is built from that list), or "+
				"add the reason it is not billable to the excused switch in this test.", mt)
		}
	}
}

// A type cannot be both billable and excused, which would mean the rule and its
// justification disagree about the same message.
func TestNoTypeIsBothBillableAndExcused(t *testing.T) {
	for _, mt := range ServiceMessageTypes() {
		if mt.IsInbound() && mt != MessageTypeMedia && mt != MessageTypeAudio {
			t.Errorf("%q is billed as a service message but is inbound only", mt)
		}
		if mt.IsCallEvent() {
			t.Errorf("%q is billed as a service message but is a call marker", mt)
		}
		if mt == MessageTypeTemplate {
			t.Error("template is billed as a service message and as a campaign send")
		}
	}
}

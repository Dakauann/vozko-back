package workflow

import (
	"errors"
	"fmt"
	"time"
)

type NodeTestBehavior string

const (
	NodeTestPure         NodeTestBehavior = "pure"
	NodeTestReadsData    NodeTestBehavior = "reads_data"
	NodeTestWritesData   NodeTestBehavior = "writes_data"
	NodeTestExternalCall NodeTestBehavior = "external_call"
	NodeTestSendsMessage NodeTestBehavior = "sends_message"
	NodeTestWaits        NodeTestBehavior = "waits"
	NodeTestAIModel      NodeTestBehavior = "ai_model"
	NodeTestVoiceOnly    NodeTestBehavior = "voice_only"
	NodeTestTrigger      NodeTestBehavior = "trigger"
	NodeTestEnd          NodeTestBehavior = "end"
	NodeTestDecoration   NodeTestBehavior = "decoration"
)

var (
	ErrTestBehaviorMissing  = errors.New("workflow: the node type declares no known test behaviour")
	ErrTestBehaviorMismatch = errors.New("workflow: the declared test behaviour does not match the node type")
)

func (b NodeTestBehavior) Valid() bool {
	switch b {
	case NodeTestPure, NodeTestReadsData, NodeTestWritesData, NodeTestExternalCall, NodeTestSendsMessage,
		NodeTestWaits, NodeTestAIModel, NodeTestVoiceOnly, NodeTestTrigger, NodeTestEnd, NodeTestDecoration:
		return true
	}
	return false
}

func onlyVoiceScoped(def NodeDefinition) bool {
	return len(def.Scopes) > 0 && !def.acceptedBy(WorkflowTypeMessages)
}

func TestBehaviorConsistent(def NodeDefinition) error {
	b, t := def.TestBehavior, def.Type.Canonical()
	if !b.Valid() {
		return fmt.Errorf("%w: %s has %q", ErrTestBehaviorMissing, t, b)
	}
	mismatch := func(reason string) error {
		return fmt.Errorf("%w: %s declares %q but %s", ErrTestBehaviorMismatch, t, b, reason)
	}
	checks := []struct {
		is       bool
		declared NodeTestBehavior
		reason   string
	}{
		{t.IsTrigger(), NodeTestTrigger, "only triggers declare trigger"},
		{t.IsEnd(), NodeTestEnd, "only the end node declares end"},
		{t.IsDecoration(), NodeTestDecoration, "only decorations declare decoration"},
	}
	for _, c := range checks {
		if c.is != (b == c.declared) {
			return mismatch(c.reason)
		}
	}
	if t.IsTrigger() || t.IsEnd() || t.IsDecoration() {
		return nil
	}
	if onlyVoiceScoped(def) != (b == NodeTestVoiceOnly) {
		return mismatch("voice_only is declared exactly by nodes that only voice workflows accept")
	}
	if b == NodeTestVoiceOnly {
		return nil
	}
	if MayCallOut(t) != (b == NodeTestExternalCall) {
		return mismatch("external_call is declared exactly by nodes that can make a real call")
	}
	if t.IsWait() && b != NodeTestWaits {
		return mismatch("a wait node declares waits")
	}
	if b == NodeTestSendsMessage && !t.IsMessaging() {
		return mismatch("sends_message is declared only by messaging nodes")
	}
	if t.IsMessaging() && !MayCallOut(t) && b != NodeTestSendsMessage && b != NodeTestWaits {
		return mismatch("a messaging node declares sends_message, or waits when it parks for an answer")
	}
	return nil
}

type WaitPreview struct {
	Reason      WaitReason `json:"reason"`
	Seconds     int64      `json:"seconds,omitempty"`
	Description string     `json:"description"`
}

var waitDescriptions = map[WaitReason]string{
	WaitReasonReply:   "aguardaria a resposta do contato",
	WaitReasonForm:    "aguardaria o envio do formulário",
	WaitReasonEvent:   "aguardaria um evento",
	WaitReasonBalance: "aguardaria saldo disponível",
	WaitReasonRetry:   "aguardaria uma nova tentativa",
}

func PreviewWait(w WaitInstruction, now time.Time) WaitPreview {
	var seconds int64
	if w.WakeAt > 0 {
		if left := w.WakeAt - now.UnixMilli(); left > 0 {
			seconds = (left + 999) / 1000
		}
	}
	preview := WaitPreview{Reason: w.Reason, Seconds: seconds}
	if w.Reason == WaitReasonDuration {
		preview.Description = fmt.Sprintf("aguardaria %d s", seconds)
		return preview
	}
	description, ok := waitDescriptions[w.Reason]
	if !ok {
		description = fmt.Sprintf("aguardaria (%s)", w.Reason)
	}
	if seconds > 0 {
		description = fmt.Sprintf("%s (até %d s)", description, seconds)
	}
	preview.Description = description
	return preview
}

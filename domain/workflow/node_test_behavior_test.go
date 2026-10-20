package workflow

import (
	"errors"
	"testing"
	"time"
)

func TestEveryTestBehaviourMustMatchWhatTheNodeTypeIs(t *testing.T) {
	voiceOnly := []NodeScope{NodeScopeVoice}
	shared := []NodeScope{NodeScopeShared, NodeScopeVoice}
	cases := []struct {
		name string
		def  NodeDefinition
		want error
	}{
		{"no declaration", NodeDefinition{Type: NodeTypeActionSetVariable, Scopes: shared}, ErrTestBehaviorMissing},
		{"unknown declaration", NodeDefinition{Type: NodeTypeActionSetVariable, Scopes: shared, TestBehavior: "magic"}, ErrTestBehaviorMissing},
		{"pure action", NodeDefinition{Type: NodeTypeActionSetVariable, Scopes: shared, TestBehavior: NodeTestPure}, nil},
		{"trigger declared as trigger", NodeDefinition{Type: NodeTypeTriggerWebhook, TestBehavior: NodeTestTrigger}, nil},
		{"trigger declared as pure", NodeDefinition{Type: NodeTypeTriggerWebhook, TestBehavior: NodeTestPure}, ErrTestBehaviorMismatch},
		{"action declared as trigger", NodeDefinition{Type: NodeTypeActionSetVariable, Scopes: shared, TestBehavior: NodeTestTrigger}, ErrTestBehaviorMismatch},
		{"end declared as end", NodeDefinition{Type: NodeTypeEnd, TestBehavior: NodeTestEnd}, nil},
		{"end declared as pure", NodeDefinition{Type: NodeTypeEnd, TestBehavior: NodeTestPure}, ErrTestBehaviorMismatch},
		{"decoration declared as decoration", NodeDefinition{Type: NodeTypeDecorationBackground, TestBehavior: NodeTestDecoration}, nil},
		{"voice only node with only the voice scope", NodeDefinition{Type: NodeTypeWaitDTMF, Scopes: voiceOnly, TestBehavior: NodeTestVoiceOnly}, nil},
		{"voice only declared on a shared node", NodeDefinition{Type: NodeTypeActionSendEmail, Scopes: shared, TestBehavior: NodeTestVoiceOnly}, ErrTestBehaviorMismatch},
		{"node with only the voice scope declared as pure", NodeDefinition{Type: NodeTypeWaitDTMF, Scopes: voiceOnly, TestBehavior: NodeTestPure}, ErrTestBehaviorMismatch},
		{"http declared as an external call", NodeDefinition{Type: NodeTypeActionHTTPRequest, Scopes: shared, TestBehavior: NodeTestExternalCall}, nil},
		{"http declared as pure hides a real call", NodeDefinition{Type: NodeTypeActionHTTPRequest, Scopes: shared, TestBehavior: NodeTestPure}, ErrTestBehaviorMismatch},
		{"code may fetch, so it declares an external call", NodeDefinition{Type: NodeTypeActionCode, Scopes: shared, TestBehavior: NodeTestExternalCall}, nil},
		{"code declared as pure hides fetch", NodeDefinition{Type: NodeTypeActionCode, Scopes: shared, TestBehavior: NodeTestPure}, ErrTestBehaviorMismatch},
		{"a pure node declared as an external call would ask for a confirmation it never needs", NodeDefinition{Type: NodeTypeActionSetVariable, Scopes: shared, TestBehavior: NodeTestExternalCall}, ErrTestBehaviorMismatch},
		{"a wait node declared as waits", NodeDefinition{Type: NodeTypeWaitDuration, Scopes: shared, TestBehavior: NodeTestWaits}, nil},
		{"a wait node declared as pure", NodeDefinition{Type: NodeTypeWaitDuration, Scopes: shared, TestBehavior: NodeTestPure}, ErrTestBehaviorMismatch},
		{"a sender declared as sends message", NodeDefinition{Type: NodeTypeActionSendText, Scopes: shared, TestBehavior: NodeTestSendsMessage}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := TestBehaviorConsistent(tc.def); !errors.Is(err, tc.want) {
				t.Fatalf("TestBehaviorConsistent = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestAWaitIsDescribedInsteadOfWaited(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	in := func(d time.Duration) int64 { return now.Add(d).UnixMilli() }
	cases := []struct {
		name string
		wait WaitInstruction
		want WaitPreview
	}{
		{"timer", WaitInstruction{WakeAt: in(300 * time.Second), Reason: WaitReasonDuration}, WaitPreview{Reason: WaitReasonDuration, Seconds: 300, Description: "aguardaria 300 s"}},
		{"reply without a timeout", WaitInstruction{Reason: WaitReasonReply}, WaitPreview{Reason: WaitReasonReply, Description: "aguardaria a resposta do contato"}},
		{"reply with a timeout", WaitInstruction{WakeAt: in(time.Hour), Reason: WaitReasonReply}, WaitPreview{Reason: WaitReasonReply, Seconds: 3600, Description: "aguardaria a resposta do contato (até 3600 s)"}},
		{"form", WaitInstruction{Reason: WaitReasonForm}, WaitPreview{Reason: WaitReasonForm, Description: "aguardaria o envio do formulário"}},
		{"balance", WaitInstruction{WakeAt: in(time.Minute), Reason: WaitReasonBalance}, WaitPreview{Reason: WaitReasonBalance, Seconds: 60, Description: "aguardaria saldo disponível (até 60 s)"}},
		{"a wake time already past is never negative", WaitInstruction{WakeAt: in(-time.Minute), Reason: WaitReasonDuration}, WaitPreview{Reason: WaitReasonDuration, Description: "aguardaria 0 s"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PreviewWait(tc.wait, now); got != tc.want {
				t.Fatalf("PreviewWait = %+v, want %+v", got, tc.want)
			}
		})
	}
}

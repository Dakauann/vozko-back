package workspaceconfig

import (
	"encoding/json"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/working_hours"
	workspaceconfigdomain "vozko/domain/workspace_config"
)

func TestResponseCarriesTheOutcomeCapturePolicy(t *testing.T) {
	enabled := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	capture := &conversation.OutcomeCapture{
		Enabled:          true,
		EnabledAt:        &enabled,
		RequireOnFinish:  true,
		DurableThreshold: 30,
		Outcomes: []conversation.Outcome{
			{Code: "sale", Label: "Venda fechada", IsDurable: true, Position: 1},
		},
	}

	body, err := json.Marshal(toWorkspaceConfigResponse(&workspaceconfigdomain.WorkspaceConfig{
		ID:             "cfg-1",
		WorkspaceID:    "ws-1",
		OutcomeCapture: capture,
		WorkingHours:   &working_hours.Spec{Timezone: "America/Sao_Paulo"},
	}))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded struct {
		OutcomeCapture *conversation.OutcomeCapture `json:"outcomeCapture"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded.OutcomeCapture == nil {
		t.Fatalf("response dropped outcomeCapture; the client would read every save back as empty")
	}
	if !decoded.OutcomeCapture.Enabled || len(decoded.OutcomeCapture.Outcomes) != 1 {
		t.Fatalf("response carried %+v, want the stored policy", decoded.OutcomeCapture)
	}
	if decoded.OutcomeCapture.Outcomes[0].Code != "sale" {
		t.Fatalf("response outcome = %q, want sale", decoded.OutcomeCapture.Outcomes[0].Code)
	}
	if decoded.OutcomeCapture.EnabledAt == nil {
		t.Fatalf("response dropped enabledAt; the quality denominator needs it")
	}
}

func TestDecodeOutcomeCapturePatch(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantNil   bool
		wantClear bool
		wantErr   bool
	}{
		{name: "absent leaves the policy untouched", body: `{"skipAdminAssignment":true}`, wantNil: true},
		{name: "null clears the policy", body: `{"outcomeCapture":null}`, wantNil: true, wantClear: true},
		{
			name: "object is parsed",
			body: `{"outcomeCapture":{"enabled":true,"outcomes":[{"code":"sale","label":"Venda","isDurable":true}]}}`,
		},
		{name: "a non-object is a bad request", body: `{"outcomeCapture":42}`, wantNil: true, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture, clear, err := conversation.DecodeOutcomeCapturePatch([]byte(tc.body), "outcomeCapture")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("DecodeOutcomeCapturePatch() err = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeOutcomeCapturePatch() err = %v, want nil", err)
			}
			if (capture == nil) != tc.wantNil {
				t.Fatalf("DecodeOutcomeCapturePatch() capture = %+v, wantNil = %v", capture, tc.wantNil)
			}
			if clear != tc.wantClear {
				t.Fatalf("DecodeOutcomeCapturePatch() clear = %v, want %v", clear, tc.wantClear)
			}
		})
	}
}

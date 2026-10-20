package workflow

import (
	"strings"
	"testing"
	"time"
)

func TestTheAgentCallContextLineSaysWhatTheAgentStartedIsDoing(t *testing.T) {
	now := time.Date(2026, 10, 19, 20, 30, 0, 0, time.UTC)
	started := now.Add(-3 * time.Minute)
	finished := now.Add(-time.Minute)
	wake := now.Add(5 * time.Minute).UnixMilli()
	base := AgentCallRun{WorkflowName: "Matrícula", StartedAt: started, CurrentStepName: "Requisição HTTP"}

	cases := []struct {
		name    string
		mutate  func(r *AgentCallRun)
		want    []string
		without []string
	}{
		{
			name:   "running",
			mutate: func(r *AgentCallRun) { r.Status = RunStatusRunning },
			want:   []string{`Fluxo "Matrícula"`, "em execução há 3 min", "etapa: Requisição HTTP", "Não afirme"},
		},
		{
			name: "waiting for the customer's reply",
			mutate: func(r *AgentCallRun) {
				r.Status, r.WaitReason, r.LastQuestion = RunStatusWaiting, WaitReasonReply, "Qual o seu CPF?"
			},
			want:    []string{`Fluxo "Matrícula"`, `aguardando a resposta do cliente a: "Qual o seu CPF?"`},
			without: []string{"Requisição HTTP"},
		},
		{
			name:   "waiting for a reply with no question on record",
			mutate: func(r *AgentCallRun) { r.Status, r.WaitReason = RunStatusWaiting, WaitReasonForm },
			want:   []string{"aguardando a resposta do cliente."},
		},
		{
			name: "a timed wait",
			mutate: func(r *AgentCallRun) {
				r.Status, r.WaitReason, r.WakeAt, r.CurrentStepName = RunStatusWaiting, WaitReasonDuration, &wake, "Aguardar"
			},
			want: []string{"em execução há 3 min", "aguardando na etapa: Aguardar", "retoma em 5 min"},
		},
		{
			name: "completed with a result",
			mutate: func(r *AgentCallRun) {
				r.Status, r.FinishedAt = RunStatusCompleted, &finished
				r.AgentResult = map[string]interface{}{"status": "aprovado", "mensagem": "Matrícula feita"}
			},
			want: []string{`Fluxo "Matrícula" concluído há 1 min`, `Resultado do fluxo: {"mensagem":"Matrícula feita","status":"aprovado"}`},
		},
		{
			name:   "completed without a result",
			mutate: func(r *AgentCallRun) { r.Status, r.FinishedAt = RunStatusCompleted, &finished },
			want:   []string{"concluído há 1 min", "sem resultado para você"},
		},
		{
			name: "failed",
			mutate: func(r *AgentCallRun) {
				r.Status, r.Error, r.FinishedAt = RunStatusError, "login recusado", &finished
				r.AgentResult = map[string]interface{}{"status": "parcial"}
			},
			want: []string{`Fluxo "Matrícula" falhou: login recusado`, `Resultado do fluxo: {"status":"parcial"}`, "Não prometa"},
		},
		{
			name:   "cancelled",
			mutate: func(r *AgentCallRun) { r.Status, r.FinishedAt = RunStatusCancelled, &finished },
			want:   []string{`Fluxo "Matrícula" cancelado há 1 min`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := base
			tc.mutate(&run)
			line := AgentCallContextLine(&run, now)
			for _, fragment := range tc.want {
				if !strings.Contains(line, fragment) {
					t.Fatalf("line misses %q:\n%s", fragment, line)
				}
			}
			for _, fragment := range tc.without {
				if strings.Contains(line, fragment) {
					t.Fatalf("line must not carry %q:\n%s", fragment, line)
				}
			}
		})
	}
}

func TestNoAgentCallRunMeansNoContextLine(t *testing.T) {
	if got := AgentCallContextLine(nil, time.Now()); got != "" {
		t.Fatalf("line = %q", got)
	}
}

func TestALongAgentResultIsCutForThePrompt(t *testing.T) {
	run := &AgentCallRun{WorkflowName: "X", Status: RunStatusCompleted, AgentResult: map[string]interface{}{"texto": strings.Repeat("a", 5000)}}
	line := AgentCallContextLine(run, time.Now())
	if len([]rune(line)) > maxAgentResultRunes+200 || !strings.Contains(line, "…") {
		t.Fatalf("line was not cut: %d runes", len([]rune(line)))
	}
}

func TestElapsedTimeReadsLikeAPerson(t *testing.T) {
	cases := map[time.Duration]string{
		20 * time.Second:                "menos de 1 min",
		3 * time.Minute:                 "3 min",
		2*time.Hour + 5*time.Minute:     "2 h 5 min",
		3 * time.Hour:                   "3 h",
		26 * time.Hour:                  "1 dia",
		50 * time.Hour:                  "2 dias",
		-5 * time.Minute:                "menos de 1 min",
		59*time.Minute + 59*time.Second: "59 min",
	}
	for d, want := range cases {
		if got := ElapsedPT(d); got != want {
			t.Errorf("ElapsedPT(%v) = %q, want %q", d, got, want)
		}
	}
}

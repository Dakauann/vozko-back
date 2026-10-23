package agentloop

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/ai"
)

type closingDriver struct {
	fakeDriver
	note    func(reply string) string
	replies []string
}

func (d *closingDriver) ClosingNote(_ context.Context, reply string) string {
	d.replies = append(d.replies, reply)
	return d.note(reply)
}

func runClosing(t *testing.T, prov *fakeAI, drv Driver) Outcome {
	t.Helper()
	return (&Engine{AI: prov}).Run(context.Background(), (&capture{}).emit, drv, Config{FinishToolName: "finish"}, &Session{}, "faça X")
}

func TestAClosingNoteReachesTheModelOnceBeforeTheAnswerEnds(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{fullText: "Pronto, montei o e-mail."}, {fullText: "Falta o link do botão."}, {fullText: "nunca"}}}
	drv := &closingDriver{note: func(string) string { return "o botão ainda não tem link" }}
	out := runClosing(t, prov, drv)
	if out.Kind != OutcomeIdle || len(prov.inputs) != 2 || len(drv.replies) != 1 {
		t.Fatalf("outcome %+v with %d calls and %d checks, want one note and one more answer", out, len(prov.inputs), len(drv.replies))
	}
	last := prov.inputs[1].Messages
	seen := false
	for _, m := range last {
		if m.Role == ai.RoleUser && strings.Contains(m.Content, "o botão ainda não tem link") && strings.HasPrefix(m.Content, AppNotice("")) {
			seen = true
		}
	}
	if !seen {
		t.Fatal("the note arrives as an app notice after her reply")
	}
}

func TestNoClosingNoteEndsTheAnswerRightAway(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{fullText: "Pronto."}, {fullText: "nunca"}}}
	drv := &closingDriver{note: func(string) string { return "" }}
	out := runClosing(t, prov, drv)
	if out.Kind != OutcomeIdle || len(prov.inputs) != 1 || drv.replies[0] != "Pronto." {
		t.Fatalf("outcome %+v with %d calls, want the answer to end at once", out, len(prov.inputs))
	}
}

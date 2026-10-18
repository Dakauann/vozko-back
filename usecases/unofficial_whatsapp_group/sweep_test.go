package unofficial_whatsapp_group_usecase

import (
	"context"
	"testing"

	uwg "vozko/domain/unofficial_whatsapp_group"
)

func TestTheSweepRunsDueJoinsAndDueAnalyses(t *testing.T) {
	w := newWorld()
	if _, err := w.svc.JoinGroup(context.Background(), everyone(), JoinInput{InviteLink: invite, InstanceIDs: []string{"n1", "n2"}}); err != nil {
		t.Fatal(err)
	}
	_, a := w.analysedGroup(t, uwg.TriggerMessageCount, 1)
	w.ai.content = validModelOutput
	w.say("Fila enorme")
	w.pend(a, 1)
	w.at(testNow.Add(uwg.JoinSpacing))

	if err := w.svc.Sweep().Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(w.api.joined) != 2 || w.ai.calls() != 1 {
		t.Fatalf("joined = %v analyses run = %d", w.api.joined, w.ai.calls())
	}
}

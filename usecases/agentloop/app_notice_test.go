package agentloop

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/ai"
)

func TestRun_AnAppNoticeStartsTheAnswerWithoutPosingAsTheUser(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{tokens: []string{"continuo"}}}}
	e := Engine{AI: prov}
	sess := &Session{Frame: AppNotice}
	e.Run(context.Background(), (&capture{}).emit, &fakeDriver{}, Config{FinishToolName: "finish"}, sess, "o editor abriu")
	first := sess.History[0]
	if first.Role != ai.RoleUser || first.Content != AppNotice("o editor abriu") {
		t.Fatalf("the notice must reach the model framed as the app, got %+v", first)
	}
	if strings.HasPrefix(first.Content, userRequestPrefix) || !strings.Contains(AppNotice("x"), "não é uma mensagem do usuário") {
		t.Fatalf("an app notice must never read as a request from the person, got %q", first.Content)
	}
}

func TestRun_WithoutAFrameThePromptIsTheUsersRequest(t *testing.T) {
	prov := &fakeAI{turns: []aiTurn{{tokens: []string{"ok"}}}}
	e := Engine{AI: prov}
	sess := &Session{}
	e.Run(context.Background(), (&capture{}).emit, &fakeDriver{}, Config{FinishToolName: "finish"}, sess, "faça X")
	if sess.History[0].Content != UserRequest("faça X") {
		t.Fatalf("got %q", sess.History[0].Content)
	}
}

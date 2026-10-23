package notification_service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/emailapi"
	emailapi_usecase "vozko/usecases/emailapi"
)

type systemSenderFake struct {
	got []emailapi.Request
	err error
}

func (f *systemSenderFake) Send(_ context.Context, req emailapi.Request) (emailapi_usecase.Result, error) {
	f.got = append(f.got, req)
	return emailapi_usecase.Result{}, f.err
}

type loaderFake struct{ err error }

func (l loaderFake) LoadTemplate(path string, data map[string]interface{}) (string, error) {
	if l.err != nil {
		return "", l.err
	}
	return "<p>" + path + " " + data["Name"].(string) + "</p>", nil
}

func TestSystemEmailsGoThroughTheEmailAPIOfTheSystemWorkspace(t *testing.T) {
	sender := &systemSenderFake{}
	svc, err := NewSystemEmailService(sender, loaderFake{}, SystemEmailConfig{WorkspaceID: "sys-ws", SenderID: "sender-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SendTemplate("ana@x.com, bia@x.com", "Redefina sua senha", "reset.html", map[string]interface{}{"Name": "Ana"}); err != nil {
		t.Fatal(err)
	}
	got := sender.got[0]
	if got.WorkspaceID != "sys-ws" || got.From != "sender-1" || got.Origin != emailapi.OriginSystem || len(got.To) != 2 || got.To[1] != "bia@x.com" ||
		got.Content.HTML != "<p>reset.html Ana</p>" || got.Content.Subject != "Redefina sua senha" || !strings.HasPrefix(got.IdempotencyKey, "system:") || got.APIKeyID != "" {
		t.Fatalf("request = %+v", got)
	}
	_ = svc.SendEmail("ana@x.com", "Oi", "<p>x</p>")
	if sender.got[1].IdempotencyKey == got.IdempotencyKey {
		t.Fatal("every system email is its own send")
	}
}

func TestSystemEmailFailuresReachTheQueueForItsRetry(t *testing.T) {
	sender := &systemSenderFake{err: errors.New("balance")}
	svc, _ := NewSystemEmailService(sender, loaderFake{}, SystemEmailConfig{WorkspaceID: "w", SenderID: "s"})
	if err := svc.SendEmail("ana@x.com", "Oi", "x"); err == nil {
		t.Fatal("a refused send must fail so the email consumer retries it")
	}
	if _, err := NewSystemEmailService(sender, loaderFake{}, SystemEmailConfig{WorkspaceID: "w"}); !errors.Is(err, ErrSystemEmailIncomplete) {
		t.Fatalf("missing sender = %v", err)
	}
	failing, _ := NewSystemEmailService(sender, loaderFake{err: errors.New("no file")}, SystemEmailConfig{WorkspaceID: "w", SenderID: "s"})
	if err := failing.SendTemplate("a@x.com", "x", "y", nil); err == nil {
		t.Fatal("an unreadable template must fail")
	}
}

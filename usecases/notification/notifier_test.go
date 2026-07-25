package notification_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/notification"
	"vozko/domain/user"
	"vozko/domain/workspace"
)

type capturePublisher struct {
	calls []string
	err   error
}

func (p *capturePublisher) Publish(email, subject, template string, _ map[string]interface{}) error {
	p.calls = append(p.calls, email+"|"+subject+"|"+template)
	return p.err
}

type fakeDedup struct {
	seen    map[string]bool
	err     error
	cleared []string
}

func (d *fakeDedup) FirstTime(k string, _ time.Duration) (bool, error) {
	if d.err != nil {
		return false, d.err
	}
	if d.seen[k] {
		return false, nil
	}
	d.seen[k] = true
	return true, nil
}
func (d *fakeDedup) Clear(k string) error {
	d.cleared = append(d.cleared, k)
	delete(d.seen, k)
	return nil
}

type fakeResolver struct {
	email string
	err   error
}

func (r fakeResolver) OwnerEmail(string) (string, error) { return r.email, r.err }

func newDedup() *fakeDedup { return &fakeDedup{seen: map[string]bool{}} }

func TestNotifier_ResolvesOwnerAndPublishes(t *testing.T) {
	pub := &capturePublisher{}
	n := NewNotifier(pub, newDedup(), fakeResolver{email: "owner@x.com"})
	if err := n.Notify(notification.Notification{WorkspaceID: "ws1", Subject: "S", Template: "t.html"}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(pub.calls) != 1 || pub.calls[0] != "owner@x.com|S|t.html" {
		t.Fatalf("expected one publish to resolved owner, got %v", pub.calls)
	}
}

func TestNotifier_DedupSuppressesSecond(t *testing.T) {
	pub := &capturePublisher{}
	n := NewNotifier(pub, newDedup(), fakeResolver{email: "o@x.com"})
	req := notification.Notification{Email: "o@x.com", Subject: "S", Template: "t", DedupKey: "evt:1", DedupTTL: time.Hour}
	_ = n.Notify(req)
	_ = n.Notify(req)
	if len(pub.calls) != 1 {
		t.Fatalf("dedup must collapse repeats to one publish, got %d", len(pub.calls))
	}
}

func TestNotifier_ClearsDedupOnPublishErrorSoRetryCanResend(t *testing.T) {
	pub := &capturePublisher{err: errors.New("queue down")}
	d := newDedup()
	n := NewNotifier(pub, d, nil)
	err := n.Notify(notification.Notification{Email: "o@x.com", Subject: "S", Template: "t", DedupKey: "evt:1", DedupTTL: time.Hour})
	if err == nil {
		t.Fatal("expected the publish error to propagate")
	}
	if len(d.cleared) != 1 {
		t.Fatal("a failed publish must clear the dedup key so a retry can re-send")
	}
}

func TestNotifier_NoRecipientIsNoOp(t *testing.T) {
	pub := &capturePublisher{}
	n := NewNotifier(pub, nil, fakeResolver{email: ""})
	if err := n.Notify(notification.Notification{WorkspaceID: "unassigned", Subject: "S", Template: "t"}); err != nil {
		t.Fatalf("missing recipient must not be an error: %v", err)
	}
	if len(pub.calls) != 0 {
		t.Fatal("must not publish without a recipient")
	}
}

type fakeWsReader struct {
	ws  *workspace.Workspace
	err error
}

func (f fakeWsReader) GetWorkspaceByID(string) (*workspace.Workspace, error) { return f.ws, f.err }

type fakeUserReader struct {
	u   *user.User
	err error
}

func (f fakeUserReader) FindByID(string) (*user.User, error) { return f.u, f.err }

func TestOwnerEmailResolver_ResolvesOwnerEmail(t *testing.T) {
	r := NewOwnerEmailResolver(
		fakeWsReader{ws: &workspace.Workspace{OwnerID: "u1"}},
		fakeUserReader{u: &user.User{Email: "owner@x.com"}},
	)
	got, err := r.OwnerEmail("ws1")
	if err != nil || got != "owner@x.com" {
		t.Fatalf("expected owner@x.com, got %q err=%v", got, err)
	}
}

func TestOwnerEmailResolver_EmptyWhenNoOwner(t *testing.T) {
	r := NewOwnerEmailResolver(fakeWsReader{ws: &workspace.Workspace{OwnerID: ""}}, fakeUserReader{})
	got, err := r.OwnerEmail("ws1")
	if err != nil || got != "" {
		t.Fatalf("expected empty, got %q err=%v", got, err)
	}
}

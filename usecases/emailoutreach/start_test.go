package emailoutreach

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	em "vozko/domain/emailinbox"
	es "vozko/domain/emailsending"
	ia "vozko/domain/inbox_assignment"
	"vozko/usecases/transactionalemail"
)

const (
	operator      = "8a1f3c2e-6b7d-4e5f-9a0b-1c2d3e4f5a6b"
	defaultSender = "5e000000-0000-0000-0000-00000000000a"
	salesSender   = "5e000000-0000-0000-0000-00000000000b"
)

type sendFake struct {
	requests []transactionalemail.Request
	result   transactionalemail.Result
	err      error
}

func (f *sendFake) Send(_ context.Context, req transactionalemail.Request) (transactionalemail.Result, error) {
	f.requests = append(f.requests, req)
	return f.result, f.err
}

type sendersFake struct {
	byID map[string]es.Sender
	err  error
}

func (f sendersFake) Get(_ context.Context, workspaceID, id string) (*es.Sender, error) {
	if f.err != nil {
		return nil, f.err
	}
	s, ok := f.byID[id]
	if !ok || s.WorkspaceID != workspaceID {
		return nil, es.ErrSenderNotFound
	}
	return &s, nil
}

func (f sendersFake) GetDefault(_ context.Context, workspaceID string) (*es.Sender, error) {
	if f.err != nil {
		return nil, f.err
	}
	for _, s := range f.byID {
		if s.WorkspaceID == workspaceID && s.IsDefault {
			return &s, nil
		}
	}
	return nil, es.ErrNoDefaultSender
}

type inboxesFake struct {
	departments map[string]string
	err         error
}

func (f inboxesFake) FindByID(_ context.Context, workspaceID, id string) (*em.Inbox, error) {
	if f.err != nil {
		return nil, f.err
	}
	department, ok := f.departments[id]
	if !ok {
		return nil, em.ErrInboxNotFound
	}
	inbox := em.Inbox{ID: id, WorkspaceID: workspaceID}
	if department != "" {
		inbox.DepartmentID = &department
	}
	return &inbox, nil
}

type conversationsFake struct {
	senderOf map[string]string
}

func (f conversationsFake) FindByID(_ context.Context, id string) (*em.Conversation, error) {
	sender, ok := f.senderOf[id]
	if !ok {
		return nil, em.ErrConversationNotFound
	}
	return &em.Conversation{ID: id, SenderID: sender}, nil
}

type claimsFake struct {
	calls []string
	err   error
}

func (f *claimsFake) ClaimIfUnassigned(entryID, entryType, accountID, workspaceID, userID, trigger string) (bool, error) {
	f.calls = append(f.calls, entryID+"|"+entryType+"|"+accountID+"|"+workspaceID+"|"+userID+"|"+trigger)
	return f.err == nil, f.err
}

type rig struct {
	send    *sendFake
	senders sendersFake
	inboxes inboxesFake
	convs   conversationsFake
	claims  *claimsFake
}

func newRig() *rig {
	return &rig{
		send: &sendFake{result: transactionalemail.Result{MessageID: "msg-1", ProviderMessageID: "ses-1", To: "ana@acme.com.br", ConversationID: "conv-1"}},
		senders: sendersFake{byID: map[string]es.Sender{
			defaultSender: {ID: defaultSender, WorkspaceID: "ws-1", IsDefault: true},
			salesSender:   {ID: salesSender, WorkspaceID: "ws-1"},
		}},
		inboxes: inboxesFake{departments: map[string]string{defaultSender: "", salesSender: "dep-sales"}},
		convs:   conversationsFake{senderOf: map[string]string{"conv-1": defaultSender}},
		claims:  &claimsFake{},
	}
}

func (r *rig) useCase(t *testing.T) *StartConversation {
	t.Helper()
	uc, err := NewStartConversation(Deps{Sender: r.send, Senders: r.senders, Inboxes: r.inboxes, Conversations: r.convs, Assignments: r.claims})
	if err != nil {
		t.Fatal(err)
	}
	return uc
}

func input() Input {
	return Input{
		WorkspaceID: "ws-1", UserID: operator, LeadID: "lead-1",
		Subject: "Proposta para {{first_name}}", Text: "Oi {{first_name}}", IdempotencyKey: "compose-1",
	}
}

func TestNewStartConversationRefusesMissingDependencies(t *testing.T) {
	r := newRig()
	full := Deps{Sender: r.send, Senders: r.senders, Inboxes: r.inboxes, Conversations: r.convs, Assignments: r.claims}
	for i, drop := range []func(*Deps){
		func(d *Deps) { d.Sender = nil }, func(d *Deps) { d.Senders = nil }, func(d *Deps) { d.Inboxes = nil },
		func(d *Deps) { d.Conversations = nil }, func(d *Deps) { d.Assignments = nil },
	} {
		deps := full
		drop(&deps)
		if _, err := NewStartConversation(deps); !errors.Is(err, ErrIncomplete) {
			t.Errorf("drop %d: err = %v", i, err)
		}
	}
}

func TestStartSendsThroughTheSharedOneToOneSend(t *testing.T) {
	r := newRig()
	started, err := r.useCase(t).Execute(context.Background(), input())
	if err != nil {
		t.Fatal(err)
	}
	req := r.send.requests[0]
	want := transactionalemail.Request{
		WorkspaceID: "ws-1", SenderID: defaultSender, LeadID: "lead-1", Subject: "Proposta para {{first_name}}",
		Body: transactionalemail.Body{Text: "Oi {{first_name}}"}, IdempotencyKey: "outreach:compose-1",
		Origin: transactionalemail.OriginOutreach, SentBy: conversation.SentByPerson(operator),
	}
	if req.WorkspaceID != want.WorkspaceID || req.SenderID != want.SenderID || req.LeadID != want.LeadID || req.Subject != want.Subject ||
		req.Body != want.Body || req.IdempotencyKey != want.IdempotencyKey || req.Origin != want.Origin || req.SentBy != want.SentBy || req.To != "" {
		t.Fatalf("request = %+v", req)
	}
	if started.ConversationID != "conv-1" || started.EntryType != "email" || started.MessageID != "msg-1" || !started.Recorded || started.Repeated {
		t.Fatalf("started = %+v", started)
	}
	if len(r.claims.calls) != 1 || r.claims.calls[0] != "conv-1|email|"+defaultSender+"|ws-1|"+operator+"|"+ia.TriggerOutreachSent {
		t.Fatalf("claims = %v", r.claims.calls)
	}
}

func TestStartToAnAddressWithADesignAndAChosenSender(t *testing.T) {
	r := newRig()
	in := input()
	in.LeadID, in.Address, in.Text, in.DesignID, in.SenderID = "", "Novo@Cliente.com", "", "design-1", salesSender
	in.IsAdmin = true
	if _, err := r.useCase(t).Execute(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	req := r.send.requests[0]
	if req.To != "Novo@Cliente.com" || req.LeadID != "" || req.SenderID != salesSender || req.Body != (transactionalemail.Body{DesignID: "design-1"}) {
		t.Fatalf("request = %+v", req)
	}
}

func TestStartRefusesBeforeSending(t *testing.T) {
	restricted := conversation.DepartmentAccessScope{Restrict: true, DepartmentIDs: []string{"dep-support"}}
	cases := []struct {
		name   string
		mutate func(*Input, *rig)
		code   string
	}{
		{"no idempotency key", func(in *Input, _ *rig) { in.IdempotencyKey = " " }, CodeIdempotencyKeyRequired},
		{"no recipient", func(in *Input, _ *rig) { in.LeadID, in.Address = "", " " }, transactionalemail.CodeRecipientMissing},
		{"no operator", func(in *Input, _ *rig) { in.UserID = "" }, transactionalemail.CodeRequestInvalid},
		{"no workspace", func(in *Input, _ *rig) { in.WorkspaceID = "" }, transactionalemail.CodeRequestInvalid},
		{"unknown sender", func(in *Input, _ *rig) { in.SenderID = "ghost" }, transactionalemail.CodeSenderMissing},
		{"no default sender", func(_ *Input, r *rig) { r.senders.byID = nil }, transactionalemail.CodeSenderMissing},
		{"a sender outside the operator's departments", func(in *Input, _ *rig) {
			in.SenderID, in.Departments = salesSender, restricted
		}, CodeForbidden},
	}
	for _, tc := range cases {
		r := newRig()
		in := input()
		tc.mutate(&in, r)
		_, err := r.useCase(t).Execute(context.Background(), in)
		if code := transactionalemail.CodeOf(err); code != tc.code {
			t.Errorf("%s: code = %s (%v), want %s", tc.name, code, err, tc.code)
		}
		if len(r.send.requests) != 0 || len(r.claims.calls) != 0 {
			t.Errorf("%s: nothing is sent or claimed", tc.name)
		}
	}
}

func TestStartLetsDepartmentScopeThroughWhereItReaches(t *testing.T) {
	cases := []struct {
		name   string
		in     func(*Input)
		reader inboxesFake
	}{
		{"an inbox without a department", func(in *Input) {
			in.Departments = conversation.DepartmentAccessScope{Restrict: true, DepartmentIDs: []string{"dep-support"}}
		}, newRig().inboxes},
		{"an inbox not created yet", func(in *Input) {
			in.Departments = conversation.DepartmentAccessScope{Restrict: true, DepartmentIDs: []string{"dep-support"}}
		}, inboxesFake{departments: map[string]string{}}},
		{"the operator's own department", func(in *Input) {
			in.SenderID, in.Departments = salesSender, conversation.DepartmentAccessScope{Restrict: true, DepartmentIDs: []string{"dep-sales"}}
		}, newRig().inboxes},
		{"an admin", func(in *Input) {
			in.SenderID, in.IsAdmin, in.Departments = salesSender, true, conversation.DepartmentAccessScope{Restrict: true}
		}, newRig().inboxes},
	}
	for _, tc := range cases {
		r := newRig()
		r.inboxes = tc.reader
		in := input()
		tc.in(&in)
		if _, err := r.useCase(t).Execute(context.Background(), in); err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	r := newRig()
	r.inboxes.err = errors.New("db down")
	if _, err := r.useCase(t).Execute(context.Background(), input()); err == nil || len(r.send.requests) != 0 {
		t.Fatalf("an unreadable inbox fails closed: %v", err)
	}
}

func TestStartPassesTheSendRefusalThrough(t *testing.T) {
	r := newRig()
	r.send.err = &transactionalemail.Refusal{Code: transactionalemail.CodeLeadOptedOut, Err: transactionalemail.ErrLeadOptedOut}
	_, err := r.useCase(t).Execute(context.Background(), input())
	if transactionalemail.CodeOf(err) != transactionalemail.CodeLeadOptedOut || len(r.claims.calls) != 0 {
		t.Fatalf("err = %v claims %v", err, r.claims.calls)
	}
}

func TestStartRepeatedKeyReturnsTheSameConversation(t *testing.T) {
	r := newRig()
	r.send.result.Repeated = true
	started, err := r.useCase(t).Execute(context.Background(), input())
	if err != nil || !started.Repeated || started.ConversationID != "conv-1" || started.MessageID != "msg-1" {
		t.Fatalf("started = %+v, %v", started, err)
	}
}

func TestStartReportsASentEmailMissingFromTheInbox(t *testing.T) {
	r := newRig()
	r.send.result.ConversationID = ""
	started, err := r.useCase(t).Execute(context.Background(), input())
	if err != nil || started.Recorded || started.ConversationID != "" || started.MessageID != "msg-1" || len(r.claims.calls) != 0 {
		t.Fatalf("started = %+v, %v, claims %v", started, err, r.claims.calls)
	}
}

func TestStartClaimsForTheMailboxThatOwnsTheConversation(t *testing.T) {
	r := newRig()
	r.convs.senderOf["conv-1"] = salesSender
	in := input()
	in.IsAdmin = true
	if _, err := r.useCase(t).Execute(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if r.claims.calls[0] != "conv-1|email|"+salesSender+"|ws-1|"+operator+"|"+ia.TriggerOutreachSent {
		t.Fatalf("claims = %v", r.claims.calls)
	}

	failing := newRig()
	failing.claims.err = errors.New("db down")
	if started, err := failing.useCase(t).Execute(context.Background(), input()); err != nil || started.ConversationID != "conv-1" {
		t.Fatalf("a failed claim never fails a sent email: %+v, %v", started, err)
	}
}

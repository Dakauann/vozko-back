package sendapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"vozko/infra/meta"
)

func assertJSON(t *testing.T, got any, want string) {
	t.Helper()
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var gotMap, wantMap any
	if err := json.Unmarshal(raw, &gotMap); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &wantMap); err != nil {
		t.Fatalf("bad want json: %v", err)
	}
	gotNorm, _ := json.Marshal(gotMap)
	wantNorm, _ := json.Marshal(wantMap)
	if string(gotNorm) != string(wantNorm) {
		t.Fatalf("body mismatch\n got: %s\nwant: %s", gotNorm, wantNorm)
	}
}

func TestEnvelopes(t *testing.T) {
	cases := []struct {
		name string
		env  Envelope
		want string
	}{
		{
			"instagram text keeps its historical shape",
			Text(ToUser("igsid"), "hi"),
			`{"recipient":{"id":"igsid"},"message":{"text":"hi"}}`,
		},
		{
			"reply to a message is top level",
			Text(ToUser("igsid"), "hi").ReplyingTo("m_1"),
			`{"recipient":{"id":"igsid"},"message":{"text":"hi"},"reply_to":{"mid":"m_1"}}`,
		},
		{
			"messenger response inside the window",
			Text(ToUser("psid"), "hi").As(MessagingResponse, "").WithMetadata("vozko:operator"),
			`{"recipient":{"id":"psid"},"messaging_type":"RESPONSE","message":{"text":"hi","metadata":"vozko:operator"}}`,
		},
		{
			"human agent tag",
			Text(ToUser("psid"), "hi").As(MessagingTag, TagHumanAgent),
			`{"recipient":{"id":"psid"},"messaging_type":"MESSAGE_TAG","tag":"HUMAN_AGENT","message":{"text":"hi"}}`,
		},
		{
			"quick replies",
			Text(ToUser("psid"), "pick").WithQuickReplies([]QuickReply{{ContentType: "text", Title: "A", Payload: "a"}}),
			`{"recipient":{"id":"psid"},"message":{"text":"pick","quick_replies":[{"content_type":"text","title":"A","payload":"a"}]}}`,
		},
		{
			"media by url",
			Media(ToUser("psid"), "image", "https://x/y.jpg"),
			`{"recipient":{"id":"psid"},"message":{"attachment":{"type":"image","payload":{"url":"https://x/y.jpg"}}}}`,
		},
		{
			"media by attachment id",
			MediaByID(ToUser("psid"), "file", "123"),
			`{"recipient":{"id":"psid"},"message":{"attachment":{"type":"file","payload":{"attachment_id":"123"}}}}`,
		},
		{
			"button template",
			Buttons(ToUser("psid"), "how can we help", []Button{{Type: "postback", Title: "Sales", Payload: "opt:1"}}),
			`{"recipient":{"id":"psid"},"message":{"attachment":{"type":"template","payload":{"template_type":"button","text":"how can we help","buttons":[{"type":"postback","title":"Sales","payload":"opt:1"}]}}}}`,
		},
		{
			"typing carries no message",
			Action(ToUser("psid"), ActionTypingOn),
			`{"recipient":{"id":"psid"},"sender_action":"typing_on"}`,
		},
		{
			"reaction",
			React(ToUser("psid"), "m_1", "love"),
			`{"recipient":{"id":"psid"},"sender_action":"react","payload":{"message_id":"m_1","reaction":"love"}}`,
		},
		{
			"unreact omits the reaction",
			Unreact(ToUser("psid"), "m_1"),
			`{"recipient":{"id":"psid"},"sender_action":"unreact","payload":{"message_id":"m_1"}}`,
		},
		{
			"private reply to a comment",
			Text(ToComment("c_1"), "see your inbox"),
			`{"recipient":{"comment_id":"c_1"},"message":{"text":"see your inbox"}}`,
		},
		{
			"private reply to a post",
			Text(ToPost("p_1"), "see your inbox"),
			`{"recipient":{"post_id":"p_1"},"message":{"text":"see your inbox"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { assertJSON(t, tc.env, tc.want) })
	}
}

func TestQuickRepliesAreCappedAndTruncated(t *testing.T) {
	opts := make([]Option, 0, 15)
	for i := 0; i < 15; i++ {
		opts = append(opts, Option{Title: "a very long option title here", Payload: "p"})
	}
	got := QuickReplies(opts, 13, 20)
	if len(got) != 13 {
		t.Fatalf("len = %d, want 13", len(got))
	}
	if n := len([]rune(got[0].Title)); n != 20 {
		t.Fatalf("title runes = %d, want 20", n)
	}
}

func TestAttachmentTypeFor(t *testing.T) {
	cases := map[string]string{"image": "image", "video": "video", "audio": "audio", "document": "file", "file": "file"}
	for kind, want := range cases {
		got, err := AttachmentTypeFor(kind)
		if err != nil || got != want {
			t.Fatalf("%s: got %q %v", kind, got, err)
		}
	}
	if _, err := AttachmentTypeFor("gif"); err == nil {
		t.Fatal("unknown kind accepted")
	}
}

type recordingPoster struct {
	req meta.Request
	out string
}

func (p *recordingPoster) Do(_ context.Context, req meta.Request, out any) error {
	p.req = req
	if out != nil && p.out != "" {
		return json.Unmarshal([]byte(p.out), out)
	}
	return nil
}

func TestSendMarksOnlySenderActionsIdempotent(t *testing.T) {
	p := &recordingPoster{out: `{"recipient_id":"psid","message_id":"m_9"}`}
	res, err := Send(context.Background(), p, "/1/messages", "tok", Text(ToUser("psid"), "hi"))
	if err != nil {
		t.Fatal(err)
	}
	if res.MessageID != "m_9" || res.RecipientID != "psid" {
		t.Fatalf("result = %+v", res)
	}
	if p.req.Idempotent || p.req.Method != http.MethodPost || p.req.Path != "/1/messages" || p.req.Token != "tok" {
		t.Fatalf("request = %+v", p.req)
	}

	if _, err := Send(context.Background(), p, "/1/messages", "tok", Action(ToUser("psid"), ActionMarkSeen)); err != nil {
		t.Fatal(err)
	}
	if !p.req.Idempotent {
		t.Fatal("sender action must be marked idempotent")
	}
}

func TestSendResponseWithoutMessageIDParses(t *testing.T) {
	p := &recordingPoster{out: `{"recipient_id":"psid"}`}
	res, err := Send(context.Background(), p, "/1/messages", "tok", React(ToUser("psid"), "m_1", "love"))
	if err != nil {
		t.Fatal(err)
	}
	if res.RecipientID != "psid" || res.MessageID != "" {
		t.Fatalf("result = %+v", res)
	}
}

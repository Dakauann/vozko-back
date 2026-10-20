package tiktok

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"vozko/domain/webhook"
)

const fixtureAccount = "_000abPL1cJIdVefgDhGijkl2mFn3AB0abY4"

const fixturePerson = "+ABc1D2/E0fGhijklMNOrD34vDW/zDiZUNRH5Jyahue6OcA7b8Zp=="

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return raw
}

func normalizeFixture(t *testing.T, name string) Event {
	t.Helper()
	env, err := DecodeEnvelope(fixture(t, name))
	if err != nil {
		t.Fatalf("%s: DecodeEnvelope: %v", name, err)
	}
	ev, err := Normalize(env)
	if err != nil {
		t.Fatalf("%s: Normalize: %v", name, err)
	}
	return ev
}

func TestReceivedTextMessage(t *testing.T) {
	ev := normalizeFixture(t, "receive_text.json")
	if ev.Kind != EventMessageReceived || ev.AccountOpenID != fixtureAccount {
		t.Fatalf("kind %q account %q", ev.Kind, ev.AccountOpenID)
	}
	m := ev.Message
	if m == nil || m.Type != MessageText || m.Text != "Oi, vocês entregam em Campinas?" {
		t.Fatalf("message = %+v", m)
	}
	if m.ConversationID != "S:1:0:abc+def/ghi=" || m.MessageID != "7381230000000000001" || m.PersonID != fixturePerson || m.PersonUsername != "ana.cliente" {
		t.Errorf("identity = %+v", m)
	}
	if m.IsFollower == nil || !*m.IsFollower || m.Source != SourceApp {
		t.Errorf("follower %v source %q", m.IsFollower, m.Source)
	}
	if !m.SentAt.Equal(time.UnixMilli(1701090879815).UTC()) || !ev.OccurredAt.Equal(m.SentAt) {
		t.Errorf("sent at %v occurred %v", m.SentAt, ev.OccurredAt)
	}
	if ev.IdempotencyKey() != "tt:msg:in:7381230000000000001" {
		t.Errorf("key = %q", ev.IdempotencyKey())
	}
}

func TestReceivedImageCarriesTheMediaID(t *testing.T) {
	ev := normalizeFixture(t, "receive_image.json")
	if ev.Message.Type != MessageImage || ev.Message.MediaID != "v0f025gc0000abc" || ev.Message.Text != "" {
		t.Fatalf("message = %+v", ev.Message)
	}
	if ev.Message.IsFollower == nil || *ev.Message.IsFollower {
		t.Errorf("follower = %v", ev.Message.IsFollower)
	}
}

func TestButtonReplyCarriesTheButtonID(t *testing.T) {
	ev := normalizeFixture(t, "receive_button_reply.json")
	if ev.Message.ButtonID != "size_m" || ev.Message.Text != "Quero o tamanho M" {
		t.Fatalf("message = %+v", ev.Message)
	}
}

func TestEchoFromTheTikTokAppIsASentMessage(t *testing.T) {
	ev := normalizeFixture(t, "send_from_app.json")
	if ev.Kind != EventMessageSent || ev.Message.Source != SourceApp || ev.Message.PersonUsername != "ana.cliente" || ev.Message.PersonID != fixturePerson {
		t.Fatalf("event = %+v message = %+v", ev, ev.Message)
	}
	if ev.IdempotencyKey() != "tt:msg:out:7381230000000000010" {
		t.Errorf("key = %q", ev.IdempotencyKey())
	}
}

func TestEchoOfATikTokAutoMessageNamesItsType(t *testing.T) {
	ev := normalizeFixture(t, "send_auto_message.json")
	if ev.Message.AutoMessageType != "WELCOME_MESSAGE" || ev.Message.Source != SourceOthers {
		t.Fatalf("message = %+v", ev.Message)
	}
}

func TestEUMessageHasNoContentAndNoConversation(t *testing.T) {
	ev := normalizeFixture(t, "receive_eu.json")
	if ev.Kind != EventMessageReceivedEU || ev.Message != nil {
		t.Fatalf("event = %+v", ev)
	}
	if !ev.OccurredAt.Equal(time.UnixMilli(1701090879815).UTC()) {
		t.Errorf("occurred at %v", ev.OccurredAt)
	}
	if !strings.HasPrefix(ev.IdempotencyKey(), "tt:eu:") {
		t.Errorf("key = %q", ev.IdempotencyKey())
	}
}

func TestAdReferral(t *testing.T) {
	ev := normalizeFixture(t, "referral_ad.json")
	r := ev.Referral
	if ev.Kind != EventReferral || r == nil || r.ConversationID != "S:1:0:abc+def/ghi=" || r.PersonID != fixturePerson {
		t.Fatalf("event = %+v", ev)
	}
	if r.Referral.Source != "ad" || r.Referral.AdID != "1800000000000000001" || r.Referral.AdName != "Promo outubro" || r.Referral.AdvertiserID != "7000000000000000001" {
		t.Errorf("referral = %+v", r.Referral)
	}
}

func TestMarkRead(t *testing.T) {
	ev := normalizeFixture(t, "mark_read.json")
	if ev.Kind != EventRead || ev.Read == nil || !ev.Read.ReadUpTo.Equal(time.UnixMilli(1701090890815).UTC()) || ev.Read.ConversationID != "S:1:0:abc+def/ghi=" {
		t.Fatalf("event = %+v read = %+v", ev, ev.Read)
	}
}

func TestCommentInsertKeepsExactIDs(t *testing.T) {
	ev := normalizeFixture(t, "comment_insert.json")
	c := ev.Comment
	if ev.Kind != EventComment || c == nil {
		t.Fatalf("event = %+v", ev)
	}
	if c.CommentID != "7247303576418566913" || c.VideoID != "7203946942097902849" || c.ParentCommentID != "" || c.Kind != KindComment {
		t.Errorf("ids = %+v", c)
	}
	if c.Action != CommentInserted || c.Text != "Qual o preço?" || c.PersonID == "" {
		t.Errorf("comment = %+v", c)
	}
	if !ev.OccurredAt.Equal(time.UnixMilli(1687394416109).UTC()) {
		t.Errorf("occurred %v", ev.OccurredAt)
	}
	if ev.IdempotencyKey() != "tt:cmt:7247303576418566913:insert:1687394416109" {
		t.Errorf("key = %q", ev.IdempotencyKey())
	}
}

func TestCommentReplyDeletion(t *testing.T) {
	ev := normalizeFixture(t, "comment_reply_delete.json")
	if ev.Comment.Kind != KindReply || ev.Comment.ParentCommentID != "7235861947622916866" || ev.Comment.Action != CommentDeleted {
		t.Fatalf("comment = %+v", ev.Comment)
	}
}

func TestAutoMessageEvents(t *testing.T) {
	audit := normalizeFixture(t, "auto_audit.json")
	if audit.Kind != EventAutoMessageAudit || audit.AutoMessage == nil || audit.AutoMessage.ID != "7390000000000000001" || audit.AutoMessage.AuditStatus != "APPROVED" {
		t.Fatalf("audit = %+v", audit.AutoMessage)
	}
	config := normalizeFixture(t, "auto_config.json")
	if config.Kind != EventAutoMessageConfig || config.AutoMessage.Action != "CREATE" || config.AutoMessage.Type != "WELCOME_MESSAGE" || config.AutoMessage.Text != "Olá! Obrigado pelo contato." {
		t.Fatalf("config = %+v", config.AutoMessage)
	}
	if audit.IdempotencyKey() == config.IdempotencyKey() {
		t.Error("an audit and a config change of the same message must not share a key")
	}
}

func TestUnknownEventIsRefused(t *testing.T) {
	env, err := DecodeEnvelope(fixture(t, "publish_complete.json"))
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}
	if _, err := Normalize(env); !errors.Is(err, ErrUnknownEvent) {
		t.Fatalf("err = %v, want ErrUnknownEvent", err)
	}
}

func TestDecodeEnvelopeRefusesMalformedInput(t *testing.T) {
	cases := map[string]string{
		"not json":         `{`,
		"no event":         `{"user_openid":"abc","content":"{}","create_time":1}`,
		"bad open id":      `{"event":"im_receive_msg","user_openid":"a b","content":"{}","create_time":1}`,
		"no content":       `{"event":"im_receive_msg","user_openid":"abc","create_time":1}`,
		"content object":   `{"event":"im_receive_msg","user_openid":"abc","content":{},"create_time":1}`,
		"fractional time":  `{"event":"im_receive_msg","user_openid":"abc","content":"{}","create_time":1.5}`,
		"trailing garbage": `{"event":"im_receive_msg","user_openid":"abc","content":"{}","create_time":1} x`,
	}
	for name, raw := range cases {
		if _, err := DecodeEnvelope([]byte(raw)); !errors.Is(err, ErrInvalidEnvelope) {
			t.Errorf("%s: err = %v, want ErrInvalidEnvelope", name, err)
		}
	}
}

func envelopeWith(event, content string) Envelope {
	return Envelope{Event: event, UserOpenID: fixtureAccount, CreateTime: 1701090882, Content: content}
}

func TestNormalizeRefusesMessagesItCannotAttribute(t *testing.T) {
	cases := map[string]string{
		"no conversation": `{"message_id":"1","unique_identifier":"p","timestamp":1,"type":"text","text":{"body":"x"}}`,
		"no message id":   `{"conversation_id":"c","unique_identifier":"p","timestamp":1,"type":"text","text":{"body":"x"}}`,
		"no person":       `{"conversation_id":"c","message_id":"1","timestamp":1,"type":"text","text":{"body":"x"}}`,
		"unsafe id":       `{"conversation_id":"c d","message_id":"1","unique_identifier":"p","timestamp":1,"type":"text"}`,
		"other account":   `{"conversation_id":"c","message_id":"1","unique_identifier":"p","timestamp":1,"type":"text","to_user":{"id":"someone-else","role":"business_account"}}`,
		"bad content":     `not json`,
	}
	for name, content := range cases {
		if _, err := Normalize(envelopeWith("im_receive_msg", content)); !errors.Is(err, ErrInvalidContent) {
			t.Errorf("%s: err = %v, want ErrInvalidContent", name, err)
		}
	}
}

func TestNormalizeRefusesAnEchoAttributedToAnotherAccount(t *testing.T) {
	content := `{"conversation_id":"c","message_id":"1","unique_identifier":"p","timestamp":1,"type":"text","from_user":{"id":"someone-else","role":"business_account"}}`
	if _, err := Normalize(envelopeWith("im_send_msg", content)); !errors.Is(err, ErrInvalidContent) {
		t.Fatalf("err = %v, want ErrInvalidContent", err)
	}
}

func TestUnknownMessageTypesAreKeptAsOther(t *testing.T) {
	content := `{"conversation_id":"c","message_id":"1","unique_identifier":"p","timestamp":1701090879815,"type":"hologram"}`
	ev, err := Normalize(envelopeWith("im_receive_msg", content))
	if err != nil || ev.Message.Type != MessageOther {
		t.Fatalf("ev = %+v err = %v", ev.Message, err)
	}
}

func TestMessageTimeFallsBackToTheEnvelopeTime(t *testing.T) {
	content := `{"conversation_id":"c","message_id":"1","unique_identifier":"p","type":"text","text":{"body":"x"}}`
	ev, err := Normalize(envelopeWith("im_receive_msg", content))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if !ev.OccurredAt.Equal(time.Unix(1701090882, 0).UTC()) {
		t.Fatalf("occurred %v", ev.OccurredAt)
	}
}

func TestCommentEventsNeedTheirIDsAndAnInsertNeedsItsAuthor(t *testing.T) {
	cases := map[string]string{
		"no comment id":         `{"video_id":1,"comment_type":"comment","comment_action":"insert","unique_identifier":"p","timestamp":1}`,
		"no video id":           `{"comment_id":1,"comment_type":"comment","comment_action":"insert","unique_identifier":"p","timestamp":1}`,
		"unknown action":        `{"comment_id":1,"video_id":1,"comment_type":"comment","comment_action":"pin","unique_identifier":"p","timestamp":1}`,
		"unknown type":          `{"comment_id":1,"video_id":1,"comment_type":"sticker","comment_action":"insert","unique_identifier":"p","timestamp":1}`,
		"reply without parent":  `{"comment_id":1,"video_id":1,"comment_type":"reply","comment_action":"insert","unique_identifier":"p","timestamp":1}`,
		"insert without person": `{"comment_id":1,"video_id":1,"comment_type":"comment","comment_action":"insert","timestamp":1}`,
		"fractional id":         `{"comment_id":1.5,"video_id":1,"comment_type":"comment","comment_action":"insert","unique_identifier":"p","timestamp":1}`,
	}
	for name, content := range cases {
		if _, err := Normalize(envelopeWith("comment.update", content)); !errors.Is(err, ErrInvalidContent) {
			t.Errorf("%s: err = %v, want ErrInvalidContent", name, err)
		}
	}
	hidden := `{"comment_id":1,"video_id":2,"comment_type":"comment","comment_action":"set_to_hidden","timestamp":1}`
	if _, err := Normalize(envelopeWith("comment.update", hidden)); err != nil {
		t.Errorf("a visibility change without the author is still a valid event: %v", err)
	}
}

func TestTopicForRoutesCommentsApartFromMessages(t *testing.T) {
	if topic, ok := TopicFor("comment.update"); !ok || topic != webhook.TopicTikTokComment {
		t.Errorf("comment.update -> %q %v", topic, ok)
	}
	for _, ev := range []string{"im_receive_msg", "im_send_msg", "im_receive_msg_eu", "im_referral_msg", "im_mark_read_msg", "im_auto_message_config_update", "im_auto_message_audit_update", "im_receive_high_intent_comment"} {
		if topic, ok := TopicFor(ev); !ok || topic != webhook.TopicTikTokMessage {
			t.Errorf("%s -> %q %v", ev, topic, ok)
		}
	}
	if _, ok := TopicFor("share.video.publish_complete"); ok {
		t.Error("an event we do not handle has no topic")
	}
}

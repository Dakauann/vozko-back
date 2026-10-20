package tiktok

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ttdomain "vozko/domain/tiktok"
)

func TestProfileRequestsTheFieldsItNeeds(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		writeOK(w, map[string]any{"username": "loja.oficial", "display_name": "Loja Oficial", "profile_image": "https://p16-sign.tiktokcdn.com/a.jpeg?x-expires=1", "has_advanced_access": true, "is_business_account": false})
	})
	c := newTestClient(t, srv.server.URL, nil)
	p, err := c.Profile(context.Background(), testCreds)
	if err != nil {
		t.Fatal(err)
	}
	if p.Username != "loja.oficial" || p.DisplayName != "Loja Oficial" || !p.HasAdvancedAccess || p.IsBusinessAccount || p.AvatarURL == "" {
		t.Errorf("profile = %+v", p)
	}
	r := srv.last()
	if r.Method != http.MethodGet || r.Path != "/business/get/" || r.Query["business_id"][0] != testCreds.OpenID || !containsAll(r.Query["fields"][0], `"username"`, `"has_advanced_access"`, `"is_business_account"`) {
		t.Errorf("request = %+v", r)
	}
}

func TestSendTextAddressesTheConversationAndQuotesWhenAsked(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		writeOK(w, map[string]any{"message": map[string]string{"message_id": "m-out-1"}})
	})
	c := newTestClient(t, srv.server.URL, nil)
	id, err := c.SendText(context.Background(), testCreds, "S:1:0:abc+def/ghi=", "Olá!", "m-in-1")
	if err != nil || id != "m-out-1" {
		t.Fatalf("id %q err %v", id, err)
	}
	body := decodeBody(t, srv.last())
	if body["business_id"] != testCreds.OpenID || body["recipient_type"] != "CONVERSATION" || body["recipient"] != "S:1:0:abc+def/ghi=" || body["message_type"] != "TEXT" {
		t.Errorf("body = %v", body)
	}
	if body["text"].(map[string]any)["body"] != "Olá!" || body["referenced_message_info"].(map[string]any)["referenced_message_id"] != "m-in-1" {
		t.Errorf("body = %v", body)
	}
}

func TestAnAcceptedSendWithoutAMessageIDIsAmbiguous(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		writeOK(w, map[string]any{"message": map[string]string{"message_id": ""}})
	})
	c := newTestClient(t, srv.server.URL, nil)
	if _, err := c.SendText(context.Background(), testCreds, "c", "x", ""); !IsAmbiguous(err) {
		t.Fatalf("err = %#v", err)
	}
}

func TestSendCardBuildsAQAButtonCard(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		writeOK(w, map[string]any{"message": map[string]string{"message_id": "m-card"}})
	})
	c := newTestClient(t, srv.server.URL, nil)
	card := ttdomain.Card{Title: "Qual tamanho?", Buttons: []ttdomain.CardButton{{ID: "size_p", Text: "P"}, {ID: "size_m", Text: "M"}}}
	if _, err := c.SendCard(context.Background(), testCreds, "c1", card); err != nil {
		t.Fatal(err)
	}
	body := decodeBody(t, srv.last())
	tmpl := body["template"].(map[string]any)
	buttons := tmpl["buttons"].([]any)
	first := buttons[0].(map[string]any)
	if body["message_type"] != "TEMPLATE" || tmpl["type"] != "QA_BUTTON_CARD" || tmpl["title"] != "Qual tamanho?" || len(buttons) != 2 || first["type"] != "REPLY" || first["title"] != "P" || first["id"] != "size_p" {
		t.Errorf("body = %v", body)
	}
}

func TestSenderActionsAreRetrySafeAndNeedNoMessageID(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		if attempt == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeOK(w, map[string]any{"message": map[string]string{"message_id": ""}})
	})
	c := newTestClient(t, srv.server.URL, nil)
	if err := c.SendAction(context.Background(), testCreds, "c1", ttdomain.SenderTyping); err != nil {
		t.Fatal(err)
	}
	if body := decodeBody(t, srv.last()); body["message_type"] != "SENDER_ACTION" || body["sender_action"] != "TYPING" {
		t.Errorf("body = %v", body)
	}
}

func TestUploadImageSendsAMultipartForm(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		writeOK(w, map[string]string{"media_id": "media-1"})
	})
	c := newTestClient(t, srv.server.URL, nil)
	id, err := c.UploadImage(context.Background(), testCreds, []byte("\x89PNGdata"), `foto"1.png`, ttdomain.MIMEPNG)
	if err != nil || id != "media-1" {
		t.Fatalf("id %q err %v", id, err)
	}
	r := srv.last()
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	mr := multipart.NewReader(strings.NewReader(string(r.Body)), params["boundary"])
	fields := map[string]string{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(part)
		fields[part.FormName()] = string(raw)
		if part.FormName() == "file" && strings.Contains(part.FileName(), `"`) {
			t.Errorf("file name not sanitized: %q", part.FileName())
		}
	}
	if fields["business_id"] != testCreds.OpenID || fields["media_type"] != "IMAGE" || fields["file"] != "\x89PNGdata" {
		t.Errorf("fields = %v", fields)
	}
}

func TestUploadImageRefusesWhatTikTokWouldRefuse(t *testing.T) {
	c := newTestClient(t, "http://unused", nil)
	if _, err := c.UploadImage(context.Background(), testCreds, []byte("x"), "a.gif", "image/gif"); !errors.Is(err, ttdomain.ErrMediaUnsupported) {
		t.Errorf("gif: %v", err)
	}
	if _, err := c.UploadImage(context.Background(), testCreds, make([]byte, ttdomain.MaxImageBytes+1), "a.jpg", ttdomain.MIMEJPEG); !errors.Is(err, ttdomain.ErrMediaTooLarge) {
		t.Errorf("large: %v", err)
	}
}

func TestDownloadMediaFetchesTheSignedURLWithTheUserHeader(t *testing.T) {
	var cdnHeader string
	cdn := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cdnHeader = r.Header.Get("x-user")
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpegbytes"))
	}))
	t.Cleanup(cdn.Close)
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		writeOK(w, map[string]string{"download_url": cdn.URL + "/obj/abc"})
	})
	c := newTestClient(t, srv.server.URL, nil)
	c.media = cdn.Client()
	got, err := c.DownloadMedia(context.Background(), testCreds, ttdomain.MediaRef{ConversationID: "c1", MessageID: "m1", MediaID: "md1", Kind: ttdomain.MediaKindImage})
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Data) != "jpegbytes" || got.MimeType != "image/jpeg" || cdnHeader != testCreds.AccessToken {
		t.Errorf("got %+v header %q", got, cdnHeader)
	}
	if body := decodeBody(t, srv.last()); body["media_id"] != "md1" || body["media_type"] != "IMAGE" || body["message_id"] != "m1" || body["conversation_id"] != "c1" {
		t.Errorf("body = %v", body)
	}
}

func TestDownloadMediaRefusesANonHTTPSURL(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		writeOK(w, map[string]string{"download_url": "http://insecure.example.com/x"})
	})
	c := newTestClient(t, srv.server.URL, nil)
	if _, err := c.DownloadMedia(context.Background(), testCreds, ttdomain.MediaRef{ConversationID: "c", MessageID: "m", MediaID: "x", Kind: ttdomain.MediaKindImage}); !errors.Is(err, errDownloadURLInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestImageAllowedReadsTheCapabilityAndFailsClosedWhenMissing(t *testing.T) {
	answer := `{"capability_infos":[{"capability_result":true,"capability_type":"IMAGE_SEND"}]}`
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"OK","request_id":"r","data":` + answer + `}`))
	})
	c := newTestClient(t, srv.server.URL, nil)
	ok, err := c.ImageAllowed(context.Background(), testCreds, "c1", ttdomain.ConversationUnknown)
	if err != nil || !ok {
		t.Fatalf("ok %v err %v", ok, err)
	}
	r := srv.last()
	if r.Query["conversation_type"][0] != "SINGLE" || r.Query["capability_types"][0] != `["IMAGE_SEND"]` {
		t.Errorf("query = %v", r.Query)
	}
	answer = `{"capability_infos":[]}`
	if ok, err := c.ImageAllowed(context.Background(), testCreds, "c1", ttdomain.ConversationSingle); ok || err == nil {
		t.Fatalf("missing capability: ok %v err %v", ok, err)
	}
}

func TestParticipantsKeepsOnlyPersonalAccounts(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		writeOK(w, map[string]any{"participants": []map[string]string{
			{"role": "BUSINESS_ACCOUNT", "id": testCreds.OpenID, "display_name": "Loja"},
			{"role": "PERSONAL_ACCOUNT", "id": "+ABc1D2/E0f==", "display_name": "Ana", "profile_image": "https://p16/a.jpg"},
		}, "messages": []any{}})
	})
	c := newTestClient(t, srv.server.URL, nil)
	people, err := c.Participants(context.Background(), testCreds, "c1")
	if err != nil || len(people) != 1 || people[0].PersonID != "+ABc1D2/E0f==" || people[0].DisplayName != "Ana" {
		t.Fatalf("people %+v err %v", people, err)
	}
}

func TestListVideosParsesIDsAndTimes(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"OK","request_id":"r","data":{"videos":[{"item_id":"7044071604573310209","media_type":"VIDEO","caption":"Promo","create_time":"1628461703","share_url":"https://www.tiktok.com/@loja/video/7044071604573310209","thumbnail_url":"https://p16/t.jpg","is_ad":false,"likes":243,"comments":10,"shares":2,"video_views":1000},{"item_id":"","caption":"broken"}],"cursor":1628461703000,"has_more":true}}`))
	})
	c := newTestClient(t, srv.server.URL, nil)
	page, err := c.ListVideos(context.Background(), testCreds, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Videos) != 1 || page.Videos[0].ItemID != "7044071604573310209" || page.Videos[0].PostedAt == nil || page.Videos[0].PostedAt.Unix() != 1628461703 || page.Videos[0].Comments != 10 || !page.HasMore || page.Cursor != 1628461703000 {
		t.Fatalf("page = %+v", page)
	}
	if got := srv.last().Query["max_count"][0]; got != "20" {
		t.Errorf("max_count = %s, want the documented maximum", got)
	}
}

func TestLookupCommentsFiltersToTheRequestedIDsAndFlattensReplies(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"OK","request_id":"r","data":{"comments":[
			{"comment_id":"7247303576418566913","video_id":"7203946942097902849","unique_identifier":"+XYz==","username":"ana","display_name":"Ana","text":"Qual o preço?","status":"PUBLIC","owner":false,"create_time":"1687394416",
			 "reply_list":[{"comment_id":7247303576418566999,"video_id":7203946942097902849,"text":"R$ 10","status":"PUBLIC","owner":true,"create_time":"1687394500"}]},
			{"comment_id":"1","video_id":"7203946942097902849","text":"not asked"}
		],"cursor":0,"has_more":false}}`))
	})
	c := newTestClient(t, srv.server.URL, nil)
	found, err := c.LookupComments(context.Background(), testCreds, "7203946942097902849", []string{"7247303576418566913", "7247303576418566999", "bad id"})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("found = %+v", found)
	}
	if found[0].Owner || found[0].Username != "ana" || found[0].Visibility != ttdomain.VisibilityPublic || found[0].CreatedAt == nil {
		t.Errorf("comment = %+v", found[0])
	}
	if found[1].CommentID != "7247303576418566999" || !found[1].Owner || found[1].ParentCommentID != "7247303576418566913" {
		t.Errorf("reply = %+v", found[1])
	}
	q := srv.last().Query
	if q["comment_ids"][0] != `["7247303576418566913","7247303576418566999"]` || q["status"][0] != "ALL" {
		t.Errorf("query = %v", q)
	}
}

func TestLookupCommentsCapsTheBatch(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		writeOK(w, map[string]any{"comments": []any{}, "cursor": 0, "has_more": false})
	})
	c := newTestClient(t, srv.server.URL, nil)
	ids := make([]string, 40)
	for i := range ids {
		ids[i] = itoa(1000 + i)
	}
	if _, err := c.LookupComments(context.Background(), testCreds, "1", ids); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(srv.last().Query["comment_ids"][0], ","); n != ttdomain.CommentLookupBatch-1 {
		t.Errorf("ids sent = %d", n+1)
	}
}

func TestReplyReturnsTheNewCommentID(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		writeOK(w, map[string]any{"comment_id": "7247303576418567000", "parent_comment_id": "7247303576418566913", "video_id": "7203946942097902849"})
	})
	c := newTestClient(t, srv.server.URL, nil)
	id, err := c.Reply(context.Background(), testCreds, "7203946942097902849", "7247303576418566913", "Oi!")
	if err != nil || id != "7247303576418567000" {
		t.Fatalf("id %q err %v", id, err)
	}
	if body := decodeBody(t, srv.last()); body["text"] != "Oi!" || body["comment_id"] != "7247303576418566913" || body["video_id"] != "7203946942097902849" {
		t.Errorf("body = %v", body)
	}
}

func TestModerationActions(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) { writeOK(w, map[string]any{}) })
	c := newTestClient(t, srv.server.URL, nil)
	if err := c.SetHidden(context.Background(), testCreds, "v1", "c1", true); err != nil {
		t.Fatal(err)
	}
	if body := decodeBody(t, srv.last()); body["action"] != "HIDE" || srv.last().Path != "/business/comment/hide/" {
		t.Errorf("hide body = %v", body)
	}
	if err := c.SetHidden(context.Background(), testCreds, "v1", "c1", false); err != nil {
		t.Fatal(err)
	}
	if body := decodeBody(t, srv.last()); body["action"] != "UNHIDE" {
		t.Errorf("unhide body = %v", body)
	}
	if err := c.SetLiked(context.Background(), testCreds, "c1", true); err != nil {
		t.Fatal(err)
	}
	if body := decodeBody(t, srv.last()); body["action"] != "LIKE" || srv.last().Path != "/business/comment/like/" {
		t.Errorf("like body = %v", body)
	}
}

func TestWebhookSubscriptionNeverSendsAnItemFilter(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		switch r.Path {
		case "/business/webhook/list/":
			writeOK(w, map[string]any{"app_id": "7043848340043202562", "event_type": "COMMENT", "callback_url": "https://api.example.com/webhooks/tiktok", "item_list": []string{"1"}})
		case "/business/webhook/update/":
			writeOK(w, map[string]any{"app_id": "7043848340043202562", "event_type": "COMMENT", "callback_url": "https://api.example.com/webhooks/tiktok"})
		}
	})
	c := newTestClient(t, srv.server.URL, nil)
	sub, err := c.Subscription(context.Background(), ttdomain.WebhookComment)
	if err != nil || sub == nil || sub.CallbackURL != "https://api.example.com/webhooks/tiktok" || len(sub.ItemFilter) != 1 {
		t.Fatalf("sub %+v err %v", sub, err)
	}
	if err := c.Subscribe(context.Background(), ttdomain.WebhookComment, "https://api.example.com/webhooks/tiktok"); err != nil {
		t.Fatal(err)
	}
	body := decodeBody(t, srv.last())
	if _, has := body["item_list"]; has {
		t.Fatal("item_list must never be sent")
	}
	if body["event_type"] != "COMMENT" || body["secret"] != "app-secret" || body["callback_url"] != "https://api.example.com/webhooks/tiktok" {
		t.Errorf("body = %v", body)
	}
}

func TestAnEmptySubscriptionIsNil(t *testing.T) {
	srv := newFakeTikTok(t, func(w http.ResponseWriter, r recordedRequest, attempt int) {
		writeOK(w, map[string]any{"app_id": "7043848340043202562", "event_type": "DIRECT_MESSAGE"})
	})
	c := newTestClient(t, srv.server.URL, nil)
	sub, err := c.Subscription(context.Background(), ttdomain.WebhookDirectMessage)
	if err != nil || sub != nil {
		t.Fatalf("sub %+v err %v", sub, err)
	}
}

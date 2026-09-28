package facebook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	fbdomain "vozko/domain/facebook"
	"vozko/infra/meta/metatest"
)

func commentServiceWith(t *testing.T, respond func(r *http.Request) string) (fbdomain.CommentService, *[]recordedCall) {
	t.Helper()
	calls := &[]recordedCall{}
	svc, err := NewCommentService(GraphConfig{HTTPClient: metatest.Server(t, func(w http.ResponseWriter, r *http.Request) {
		call := recordedCall{method: r.Method, path: r.URL.Path, query: map[string]string{}}
		for k := range r.URL.Query() {
			call.query[k] = r.URL.Query().Get(k)
		}
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			_ = json.Unmarshal(raw, &call.body)
		}
		*calls = append(*calls, call)
		_, _ = w.Write([]byte(respond(r)))
	})})
	if err != nil {
		t.Fatal(err)
	}
	return svc, calls
}

func TestListCommentsDecodesModerationFlags(t *testing.T) {
	svc, calls := commentServiceWith(t, func(*http.Request) string {
		return `{"data":[{"id":"1_c","message":"preço?","created_time":"2026-09-28T12:00:00+0000","from":{"id":"PSID","name":"Ana"},
			"parent":{"id":"1_p"},"like_count":2,"comment_count":1,"is_hidden":false,"can_hide":true,"can_remove":true,
			"can_reply_privately":true,"can_like":true,"user_likes":true,"attachment":{"type":"photo","url":"https://x"}}],
			"paging":{"cursors":{"after":"A"},"next":"https://next"}}`
	})
	out, err := svc.List(context.Background(), "tok", "PAGE_1", fbdomain.CommentsStream, 50, "")
	if err != nil {
		t.Fatal(err)
	}
	call := (*calls)[0]
	if call.path != "/v25.0/PAGE_1/comments" || call.query["filter"] != "stream" || call.query["order"] != "reverse_chronological" || !strings.Contains(call.query["fields"], "can_reply_privately") {
		t.Fatalf("call = %+v", call)
	}
	c := out.Items[0]
	if c.FBCommentID != "1_c" || c.FromID != "PSID" || c.ParentID != "1_p" || c.LikeCount != 2 || !c.CanReplyPrivately || !c.UserLikes || c.AttachmentType != "photo" || c.CreatedTime == nil {
		t.Fatalf("comment = %+v", c)
	}
	if !out.HasNext || out.NextCursor != "A" {
		t.Fatalf("page = %+v", out)
	}
}

func TestCommentWritesUseTheirEndpoints(t *testing.T) {
	svc, calls := commentServiceWith(t, func(r *http.Request) string {
		if strings.HasSuffix(r.URL.Path, "/comments") {
			return `{"id":"1_new"}`
		}
		return `{"success":true}`
	})
	ctx := context.Background()
	id, err := svc.Create(ctx, "tok", "PAGE_1", "Obrigado!")
	if err != nil || id != "1_new" {
		t.Fatalf("create %q %v", id, err)
	}
	if err := svc.Edit(ctx, "tok", "1_c", "editado"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetHidden(ctx, "tok", "1_c", true); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetLiked(ctx, "tok", "1_c", true); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetLiked(ctx, "tok", "1_c", false); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, "tok", "1_c"); err != nil {
		t.Fatal(err)
	}
	want := []string{"POST /v25.0/PAGE_1/comments", "POST /v25.0/1_c", "POST /v25.0/1_c", "POST /v25.0/1_c/likes", "DELETE /v25.0/1_c/likes", "DELETE /v25.0/1_c"}
	for i, w := range want {
		if got := (*calls)[i].method + " " + (*calls)[i].path; got != w {
			t.Errorf("call %d = %s, want %s", i, got, w)
		}
	}
	if (*calls)[0].body["message"] != "Obrigado!" || (*calls)[1].body["message"] != "editado" || (*calls)[2].body["is_hidden"] != true {
		t.Fatalf("bodies = %v %v %v", (*calls)[0].body, (*calls)[1].body, (*calls)[2].body)
	}
}

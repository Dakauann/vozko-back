package teamchat

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

var thread = Thread{ID: "t1", WorkspaceID: "ws", Kind: KindGroup}

func TestNewText(t *testing.T) {
	tooMany := make([]string, MaxMentions+1)
	for i := range tooMany {
		tooMany[i] = string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	cases := []struct {
		name  string
		draft Draft
		want  error
	}{
		{"plain text", Draft{Body: "  bom dia  ", ClientID: "c1"}, nil},
		{"empty", Draft{Body: " \n "}, ErrBodyRequired},
		{"too long", Draft{Body: strings.Repeat("é", MaxBodyRunes+1)}, ErrBodyTooLong},
		{"longest allowed", Draft{Body: strings.Repeat("é", MaxBodyRunes)}, nil},
		{"too many mentions", Draft{Body: "oi", Mentions: tooMany}, ErrTooManyMentions},
		{"client id too long", Draft{Body: "oi", ClientID: strings.Repeat("x", MaxClientIDLength+1)}, ErrClientIDTooLong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, err := NewText("m1", thread, "ana", tc.draft, created)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if err != nil {
				return
			}
			if msg.Kind != MessageText || msg.ThreadID != "t1" || msg.WorkspaceID != "ws" || msg.AuthorID != "ana" || msg.Seq != 0 {
				t.Fatalf("msg = %+v", msg)
			}
			if msg.Body != strings.TrimSpace(tc.draft.Body) {
				t.Fatalf("body = %q", msg.Body)
			}
		})
	}
}

func TestMentionsAreDistinctAndTrimmed(t *testing.T) {
	msg, err := NewText("m1", thread, "ana", Draft{Body: "oi", Mentions: []string{" bia", "bia", "", "caio"}}, created)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(msg.Mentions, []string{"bia", "caio"}) {
		t.Fatalf("mentions = %v", msg.Mentions)
	}
	if !msg.Mentioned("caio") || msg.Mentioned("ana") {
		t.Fatal("Mentioned must follow the mention list")
	}
}

func TestSystemMessagesCarryTheActorAndSubjects(t *testing.T) {
	msg := NewSystem("m2", thread, "ana", EventMembersAdded, []string{"bia", "bia", "caio"}, "", created)
	if msg.Kind != MessageSystem || msg.Event != EventMembersAdded || msg.AuthorID != "ana" {
		t.Fatalf("msg = %+v", msg)
	}
	if !reflect.DeepEqual(msg.SubjectIDs, []string{"bia", "caio"}) {
		t.Fatalf("subjects = %v", msg.SubjectIDs)
	}
}

func TestEditBy(t *testing.T) {
	original, _ := NewText("m1", thread, "ana", Draft{Body: "oi"}, created)
	later := created.Add(time.Minute)
	edited, err := original.EditBy("ana", " olá ", later)
	if err != nil || edited.Body != "olá" || edited.EditedAt == nil || !edited.EditedAt.Equal(later) {
		t.Fatalf("edited = %+v, err = %v", edited, err)
	}
	if _, err := original.EditBy("bia", "x", later); !errors.Is(err, ErrNotAuthor) {
		t.Fatalf("someone else: %v", err)
	}
	if _, err := original.EditBy("ana", "", later); !errors.Is(err, ErrBodyRequired) {
		t.Fatalf("empty: %v", err)
	}
	system := NewSystem("m2", thread, "ana", EventRenamed, nil, "Vendas", created)
	if _, err := system.EditBy("ana", "x", later); !errors.Is(err, ErrSystemMessage) {
		t.Fatalf("system: %v", err)
	}
	deleted, _ := original.DeleteBy("ana", later)
	if _, err := deleted.EditBy("ana", "x", later); !errors.Is(err, ErrMessageNotFound) {
		t.Fatalf("deleted: %v", err)
	}
}

func TestDeleteByLeavesATombstone(t *testing.T) {
	original, _ := NewText("m1", thread, "ana", Draft{Body: "segredo", Mentions: []string{"bia"}}, created)
	later := created.Add(time.Minute)
	deleted, err := original.DeleteBy("ana", later)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Body != "" || len(deleted.Mentions) != 0 || deleted.DeletedAt == nil || !deleted.DeletedAt.Equal(later) {
		t.Fatalf("deleted = %+v", deleted)
	}
	if _, err := original.DeleteBy("bia", later); !errors.Is(err, ErrNotAuthor) {
		t.Fatalf("someone else: %v", err)
	}
	if _, err := deleted.DeleteBy("ana", later); !errors.Is(err, ErrMessageNotFound) {
		t.Fatalf("twice: %v", err)
	}
}

func TestPageLimits(t *testing.T) {
	cases := []struct {
		requested, messages, threads int
	}{
		{0, DefaultMessagePage, DefaultThreadPage},
		{-3, DefaultMessagePage, DefaultThreadPage},
		{10, 10, 10},
		{500, MaxMessagePage, MaxThreadPage},
	}
	for _, tc := range cases {
		if got := MessagePageLimit(tc.requested); got != tc.messages {
			t.Errorf("MessagePageLimit(%d) = %d, want %d", tc.requested, got, tc.messages)
		}
		if got := ThreadPageLimit(tc.requested); got != tc.threads {
			t.Errorf("ThreadPageLimit(%d) = %d, want %d", tc.requested, got, tc.threads)
		}
	}
}

func TestMessageQueryTakesOneDirection(t *testing.T) {
	if err := (MessageQuery{BeforeSeq: 5, AfterSeq: 2}).Validate(); !errors.Is(err, ErrPageDirection) {
		t.Fatalf("both: %v", err)
	}
	if err := (MessageQuery{BeforeSeq: -1}).Validate(); !errors.Is(err, ErrPageDirection) {
		t.Fatalf("negative: %v", err)
	}
	if err := (MessageQuery{AfterSeq: 2}).Validate(); err != nil {
		t.Fatalf("after: %v", err)
	}
}

package facebook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	mm "vozko/domain/metamessaging"
)

type FeedKind string

const (
	FeedUnknown         FeedKind = "unknown"
	FeedCommentAdded    FeedKind = "comment_added"
	FeedCommentEdited   FeedKind = "comment_edited"
	FeedCommentRemoved  FeedKind = "comment_removed"
	FeedCommentHidden   FeedKind = "comment_hidden"
	FeedCommentUnhidden FeedKind = "comment_unhidden"
	FeedReaction        FeedKind = "reaction"
	FeedPostAdded       FeedKind = "post_added"
	FeedPostEdited      FeedKind = "post_edited"
	FeedPostRemoved     FeedKind = "post_removed"
	FeedPostHidden      FeedKind = "post_hidden"
	FeedPostUnhidden    FeedKind = "post_unhidden"
	FeedMentioned       FeedKind = "mentioned"
	FeedVideoStatus     FeedKind = "video_status"
)

type Actor struct {
	ID   string
	Name string
}

type FeedEvent struct {
	Kind         FeedKind
	Item         string
	Verb         string
	PostID       string
	CommentID    string
	ParentID     string
	PhotoID      string
	VideoID      string
	From         *Actor
	Message      string
	Photo        string
	ReactionType string
	CreatedTime  time.Time
	Published    *bool
	IsHidden     *bool
	VideoStatus  string
	Raw          json.RawMessage
}

func (e *FeedEvent) IsTopLevelComment() bool {
	return e.ParentID == "" || e.ParentID == e.PostID
}

func (e *FeedEvent) IsFromPage(fbPageID string) bool {
	return e.From != nil && e.From.ID == fbPageID
}

func (e *FeedEvent) ReactionTarget() string {
	if e.CommentID != "" {
		return e.CommentID
	}
	return e.PostID
}

type verbFamily int

const (
	verbOther verbFamily = iota
	verbAdd
	verbEdit
	verbRemove
	verbHide
	verbUnhide
)

func familyOf(verb string) verbFamily {
	switch verb {
	case "add":
		return verbAdd
	case "edit", "edited", "update":
		return verbEdit
	case "remove", "delete":
		return verbRemove
	case "hide":
		return verbHide
	case "unhide":
		return verbUnhide
	}
	return verbOther
}

var commentKinds = map[verbFamily]FeedKind{
	verbAdd: FeedCommentAdded, verbEdit: FeedCommentEdited, verbRemove: FeedCommentRemoved,
	verbHide: FeedCommentHidden, verbUnhide: FeedCommentUnhidden,
}

var postKinds = map[verbFamily]FeedKind{
	verbAdd: FeedPostAdded, verbEdit: FeedPostEdited, verbRemove: FeedPostRemoved,
	verbHide: FeedPostHidden, verbUnhide: FeedPostUnhidden,
}

var postItems = map[string]struct{}{
	"status": {}, "post": {}, "photo": {}, "video": {}, "share": {}, "link": {}, "album": {},
}

type rawActor struct {
	ID   mm.GraphID `json:"id"`
	Name string     `json:"name"`
}

type rawFeedValue struct {
	Item         string          `json:"item"`
	Verb         string          `json:"verb"`
	PostID       string          `json:"post_id"`
	CommentID    string          `json:"comment_id"`
	ParentID     string          `json:"parent_id"`
	PhotoID      mm.GraphID      `json:"photo_id"`
	VideoID      mm.GraphID      `json:"video_id"`
	From         *rawActor       `json:"from"`
	SenderID     mm.GraphID      `json:"sender_id"`
	SenderName   string          `json:"sender_name"`
	Message      string          `json:"message"`
	Photo        string          `json:"photo"`
	ReactionType string          `json:"reaction_type"`
	CreatedTime  json.Number     `json:"created_time"`
	Published    json.RawMessage `json:"published"`
	IsHidden     *bool           `json:"is_hidden"`
}

type rawVideoValue struct {
	ID     mm.GraphID `json:"id"`
	Status struct {
		VideoStatus string `json:"video_status"`
	} `json:"status"`
}

func NormalizeFeedChange(field string, value json.RawMessage) (*FeedEvent, error) {
	switch field {
	case "feed", "mention":
		return normalizeFeedValue(field, value)
	case "videos":
		var v rawVideoValue
		if err := decodeStrict(value, &v); err != nil {
			return nil, err
		}
		return &FeedEvent{Kind: FeedVideoStatus, VideoID: v.ID.String(), VideoStatus: v.Status.VideoStatus, Raw: value}, nil
	}
	return &FeedEvent{Kind: FeedUnknown, Raw: value}, nil
}

func normalizeFeedValue(field string, value json.RawMessage) (*FeedEvent, error) {
	var v rawFeedValue
	if err := decodeStrict(value, &v); err != nil {
		return nil, err
	}
	published, err := flexibleBool(v.Published)
	if err != nil {
		return nil, err
	}
	created, _ := v.CreatedTime.Int64()
	ev := &FeedEvent{
		Kind:         FeedUnknown,
		Item:         v.Item,
		Verb:         v.Verb,
		PostID:       v.PostID,
		CommentID:    v.CommentID,
		ParentID:     v.ParentID,
		PhotoID:      v.PhotoID.String(),
		VideoID:      v.VideoID.String(),
		From:         actorOf(v),
		Message:      v.Message,
		Photo:        v.Photo,
		ReactionType: strings.ToLower(v.ReactionType),
		CreatedTime:  mm.UnixTime(created),
		Published:    published,
		IsHidden:     v.IsHidden,
		Raw:          value,
	}
	switch {
	case field == "mention":
		ev.Kind = FeedMentioned
	case v.Item == "comment":
		ev.Kind = kindOr(commentKinds, v.Verb)
	case v.Item == "reaction":
		ev.Kind = FeedReaction
	default:
		if _, ok := postItems[v.Item]; ok {
			ev.Kind = kindOr(postKinds, v.Verb)
		}
	}
	return ev, nil
}

func kindOr(kinds map[verbFamily]FeedKind, verb string) FeedKind {
	if kind, ok := kinds[familyOf(verb)]; ok {
		return kind
	}
	return FeedUnknown
}

func actorOf(v rawFeedValue) *Actor {
	if v.From != nil && v.From.ID.String() != "" {
		return &Actor{ID: v.From.ID.String(), Name: v.From.Name}
	}
	if id := v.SenderID.String(); id != "" {
		return &Actor{ID: id, Name: v.SenderName}
	}
	return nil
}

func flexibleBool(raw json.RawMessage) (*bool, error) {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if text == "" || text == "null" {
		return nil, nil
	}
	b, err := strconv.ParseBool(text)
	if err != nil {
		return nil, fmt.Errorf("facebook feed: published is %q: %w", text, err)
	}
	return &b, nil
}

func decodeStrict(raw json.RawMessage, into any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("%w: %v", mm.ErrInvalidWebhookPayload, err)
	}
	return nil
}

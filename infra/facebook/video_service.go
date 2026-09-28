package facebook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	fbdomain "vozko/domain/facebook"
	"vozko/infra/meta"
)

const (
	GraphVideoHost = "graph-video.facebook.com"
	reelFields     = "id,description,updated_time,permalink_url,length,status"
)

type publishResult struct {
	Success bool   `json:"success"`
	PostID  string `json:"post_id"`
}

func (s *postService) listReels(ctx context.Context, fbPageID, pageToken string, limit int, after string) (*fbdomain.Paged[*fbdomain.RemotePost], error) {
	q := url.Values{}
	q.Set("fields", reelFields)
	q.Set("limit", strconv.Itoa(limit))
	if after != "" {
		q.Set("after", after)
	}
	var out struct {
		Data []struct {
			ID           string `json:"id"`
			Description  string `json:"description"`
			UpdatedTime  string `json:"updated_time"`
			PermalinkURL string `json:"permalink_url"`
		} `json:"data"`
		Paging struct {
			Cursors struct {
				After string `json:"after"`
			} `json:"cursors"`
			Next string `json:"next"`
		} `json:"paging"`
	}
	if err := s.client.Do(ctx, meta.Request{Method: http.MethodGet, Path: "/" + fbPageID + "/video_reels", Token: pageToken, Query: q}, &out); err != nil {
		return nil, err
	}
	page := &fbdomain.Paged[*fbdomain.RemotePost]{Items: make([]*fbdomain.RemotePost, 0, len(out.Data))}
	for _, r := range out.Data {
		updated := graphTime(r.UpdatedTime)
		page.Items = append(page.Items, &fbdomain.RemotePost{
			FBPostID: fbPageID + "_" + r.ID, FromID: fbPageID, Message: r.Description, PermalinkURL: r.PermalinkURL,
			CreatedTime: updated, UpdatedTime: updated, IsPublished: true, StatusType: "added_reel",
			Attachments: []fbdomain.RemoteAttachment{{Type: "reel", MediaType: "video"}},
		})
	}
	if out.Paging.Next != "" {
		page.NextCursor, page.HasNext = out.Paging.Cursors.After, true
	}
	return page, nil
}

func (s *postService) ListStories(ctx context.Context, fbPageID, pageToken string) ([]*fbdomain.RemoteStory, error) {
	var out struct {
		Data []struct {
			PostID       string          `json:"post_id"`
			Status       string          `json:"status"`
			CreationTime json.RawMessage `json:"creation_time"`
			MediaType    string          `json:"media_type"`
			MediaID      string          `json:"media_id"`
			URL          string          `json:"url"`
		} `json:"data"`
	}
	if err := s.client.Do(ctx, meta.Request{Method: http.MethodGet, Path: "/" + fbPageID + "/stories", Token: pageToken}, &out); err != nil {
		return nil, err
	}
	stories := make([]*fbdomain.RemoteStory, 0, len(out.Data))
	for _, st := range out.Data {
		stories = append(stories, &fbdomain.RemoteStory{
			PostID: st.PostID, Status: st.Status, MediaType: st.MediaType, MediaID: st.MediaID, URL: st.URL,
			CreatedTime: unixField(st.CreationTime),
		})
	}
	return stories, nil
}

func (s *postService) CreateVideo(ctx context.Context, fbPageID, pageToken string, in fbdomain.VideoInput) (string, error) {
	body := map[string]any{"file_url": in.FileURL}
	if in.Title != "" {
		body["title"] = in.Title
	}
	if in.Description != "" {
		body["description"] = in.Description
	}
	if in.ScheduledAt != nil {
		body["published"] = false
		body["scheduled_publish_time"] = in.ScheduledAt.Unix()
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := s.video.Do(ctx, meta.Request{Method: http.MethodPost, Path: "/" + fbPageID + "/videos", Token: pageToken, Body: body}, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("facebook: video created without an id")
	}
	return out.ID, nil
}

func (s *postService) StartVideoUpload(ctx context.Context, fbPageID, pageToken string, target fbdomain.VideoTarget) (*fbdomain.VideoSession, error) {
	var out struct {
		VideoID   string `json:"video_id"`
		UploadURL string `json:"upload_url"`
	}
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodPost, Path: "/" + fbPageID + "/" + string(target), Token: pageToken,
		Body: map[string]string{"upload_phase": "start"}, Idempotent: true,
	}, &out); err != nil {
		return nil, err
	}
	if out.VideoID == "" || out.UploadURL == "" {
		return nil, fmt.Errorf("facebook: %s upload session came back incomplete", target)
	}
	return &fbdomain.VideoSession{VideoID: out.VideoID, UploadURL: out.UploadURL}, nil
}

func (s *postService) TransferVideo(ctx context.Context, pageToken string, session fbdomain.VideoSession, fileURL string) error {
	req, err := http.NewRequest(http.MethodPost, session.UploadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "OAuth "+pageToken)
	req.Header.Set("file_url", fileURL)
	var out struct {
		Success bool `json:"success"`
	}
	if err := s.client.DoRaw(ctx, req, &out); err != nil {
		return err
	}
	if !out.Success {
		return fmt.Errorf("facebook: the video transfer for %s was not acknowledged", session.VideoID)
	}
	return nil
}

func (s *postService) FinishReel(ctx context.Context, fbPageID, pageToken, videoID string, in fbdomain.ReelFinish) (string, error) {
	body := map[string]any{"video_id": videoID, "upload_phase": "finish", "video_state": "PUBLISHED"}
	if in.Description != "" {
		body["description"] = in.Description
	}
	if in.ScheduledAt != nil {
		body["video_state"] = "SCHEDULED"
		body["scheduled_publish_time"] = in.ScheduledAt.Unix()
	}
	return s.finish(ctx, "/"+fbPageID+"/video_reels", pageToken, body)
}

func (s *postService) FinishVideoStory(ctx context.Context, fbPageID, pageToken, videoID string) (string, error) {
	return s.finish(ctx, "/"+fbPageID+"/video_stories", pageToken, map[string]any{"video_id": videoID, "upload_phase": "finish"})
}

func (s *postService) CreatePhotoStory(ctx context.Context, fbPageID, pageToken, photoID string) (string, error) {
	return s.finish(ctx, "/"+fbPageID+"/photo_stories", pageToken, map[string]any{"photo_id": photoID})
}

func (s *postService) finish(ctx context.Context, path, pageToken string, body map[string]any) (string, error) {
	var out publishResult
	if err := s.client.Do(ctx, meta.Request{Method: http.MethodPost, Path: path, Token: pageToken, Body: body}, &out); err != nil {
		return "", err
	}
	if !out.Success {
		return "", fmt.Errorf("facebook: %s was not acknowledged", path)
	}
	return out.PostID, nil
}

func (s *postService) VideoStatus(ctx context.Context, pageToken, videoID string) (*fbdomain.VideoStatus, error) {
	q := url.Values{}
	q.Set("fields", "status,post_id")
	var out struct {
		Status struct {
			VideoStatus string `json:"video_status"`
		} `json:"status"`
		PostID string `json:"post_id"`
	}
	if err := s.client.Do(ctx, meta.Request{Method: http.MethodGet, Path: "/" + videoID, Token: pageToken, Query: q}, &out); err != nil {
		return nil, err
	}
	return &fbdomain.VideoStatus{State: out.Status.VideoStatus, PostID: out.PostID}, nil
}

func unixField(raw json.RawMessage) *time.Time {
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return nil
		}
		n = json.Number(s)
	}
	seconds, err := n.Int64()
	if err != nil || seconds <= 0 {
		return nil
	}
	at := time.Unix(seconds, 0).UTC()
	return &at
}

package media_repository

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/media"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestLibraryMediaIsReadOnlyInsideItsOwnWorkspace(t *testing.T) {
	repo := NewMediaRepository(repotest.IsolatedDB(t, "media_ws", &schema.Media{}))
	workspace, other := uuid.NewString(), uuid.NewString()
	photo := &media.Media{WorkspaceID: workspace, URL: "https://cdn/p.jpg", Type: media.MediaTypeProductImage}
	if err := repo.CreateMedia(photo); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.GetMediaForWorkspace(workspace, photo.ID)
	if err != nil {
		t.Fatalf("GetMediaForWorkspace: %v", err)
	}
	if got.ID != photo.ID || got.WorkspaceID != workspace || got.Type != media.MediaTypeProductImage || got.URL != photo.URL {
		t.Fatalf("read back %+v", got)
	}
	if _, err := conversation.SendableLibraryMedia(repo, workspace, photo.ID); err != nil {
		t.Fatalf("a library photo of the workspace must be sendable: %v", err)
	}

	for name, c := range map[string]struct{ workspace, id string }{
		"another workspace":   {other, photo.ID},
		"no workspace":        {"", photo.ID},
		"malformed workspace": {"ws-1", photo.ID},
		"unknown id":          {workspace, uuid.NewString()},
		"malformed id":        {workspace, "not-a-uuid"},
	} {
		if _, err := repo.GetMediaForWorkspace(c.workspace, c.id); !errors.Is(err, media.ErrMediaNotFound) {
			t.Errorf("%s: err = %v, want ErrMediaNotFound", name, err)
		}
	}
	if _, err := conversation.SendableLibraryMedia(repo, other, photo.ID); !errors.Is(err, conversation.ErrMediaNotSendable) {
		t.Fatalf("another workspace's file was sendable: %v", err)
	}
}

func TestLibraryMediaIsFoundByAddressOnlyInsideItsOwnWorkspace(t *testing.T) {
	repo := NewMediaRepository(repotest.IsolatedDB(t, "media_ws_url", &schema.Media{}))
	workspace, other := uuid.NewString(), uuid.NewString()
	photo := &media.Media{WorkspaceID: workspace, URL: "https://cdn/by-url.jpg", Type: media.MediaTypeProductImage}
	if err := repo.CreateMedia(photo); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.GetMediaForWorkspaceByURL(workspace, photo.URL)
	if err != nil || got.ID != photo.ID || got.WorkspaceID != workspace {
		t.Fatalf("read back %+v, err %v", got, err)
	}
	for name, c := range map[string]struct{ workspace, url string }{
		"another workspace":   {other, photo.URL},
		"malformed workspace": {"ws-1", photo.URL},
		"unknown address":     {workspace, "https://cdn/other.jpg"},
		"no address":          {workspace, ""},
	} {
		if _, err := repo.GetMediaForWorkspaceByURL(c.workspace, c.url); !errors.Is(err, media.ErrMediaNotFound) {
			t.Errorf("%s: err = %v, want ErrMediaNotFound", name, err)
		}
	}
}

package carriermessaging_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/media"
	"vozko/domain/rcs"
	"vozko/domain/shared"
)

type fakeLibrary map[string]*media.Media

func (f fakeLibrary) GetMediaForWorkspace(workspaceID, id string) (*media.Media, error) {
	m, ok := f[id]
	if !ok || m.WorkspaceID != workspaceID {
		return nil, media.ErrMediaNotFound
	}
	return m, nil
}

type looseLibrary struct{ item *media.Media }

func (l looseLibrary) GetMediaForWorkspace(string, string) (*media.Media, error) { return l.item, nil }

type fakeRemote struct {
	contentType string
	size        int64
	err         error
}

func (f fakeRemote) Describe(context.Context, string) (string, int64, error) {
	return f.contentType, f.size, f.err
}

func TestResolveMediaDescribesALibraryFile(t *testing.T) {
	h := newHarness(t)
	h.svc.Library = fakeLibrary{"m1": {ID: "m1", WorkspaceID: "ws-1", URL: "https://cdn.example.com/a.jpg", Description: "Oferta", Type: media.MediaTypeProductImage}}
	h.svc.RemoteMedia = fakeRemote{contentType: "image/jpeg", size: 2048}

	got, err := h.svc.ResolveMedia(context.Background(), "ws-1", "m1")
	if err != nil {
		t.Fatalf("ResolveMedia = %v", err)
	}
	if got.Kind != rcs.MediaImage || got.MIMEType != "image/jpeg" || got.SizeBytes != 2048 || got.URL != "https://cdn.example.com/a.jpg" || got.Name != "Oferta" {
		t.Fatalf("media = %+v", got)
	}
}

func TestResolveMediaRefuses(t *testing.T) {
	cases := []struct {
		name   string
		item   *media.Media
		remote fakeRemote
		ws     string
		want   error
	}{
		{"another workspace", &media.Media{ID: "m1", WorkspaceID: "ws-2", URL: "https://cdn.example.com/a.jpg", Type: media.MediaTypeProductImage}, fakeRemote{contentType: "image/jpeg", size: 1}, "ws-1", media.ErrMediaNotFound},
		{"private file", &media.Media{ID: "m1", WorkspaceID: "ws-1", URL: "https://cdn.example.com/a.csv", Type: media.MediaTypeLeadImport}, fakeRemote{contentType: "text/csv", size: 1}, "ws-1", media.ErrMediaNotFound},
		{"type RCS does not carry", &media.Media{ID: "m1", WorkspaceID: "ws-1", URL: "https://cdn.example.com/a.zip", Type: media.MediaTypeDocument}, fakeRemote{contentType: "application/zip", size: 1}, "ws-1", rcs.ErrMediaInvalid},
		{"too large", &media.Media{ID: "m1", WorkspaceID: "ws-1", URL: "https://cdn.example.com/a.mp4", Type: media.MediaTypeProductVideo}, fakeRemote{contentType: "video/mp4", size: rcs.MaxMediaBytes + 1}, "ws-1", rcs.ErrMediaInvalid},
		{"unreachable", &media.Media{ID: "m1", WorkspaceID: "ws-1", URL: "https://cdn.example.com/a.mp4", Type: media.MediaTypeProductVideo}, fakeRemote{err: media.ErrRemoteFileMissing}, "ws-1", media.ErrRemoteFileMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.svc.Library = fakeLibrary{"m1": tc.item}
			h.svc.RemoteMedia = tc.remote
			if _, err := h.svc.ResolveMedia(context.Background(), tc.ws, "m1"); !errors.Is(err, tc.want) {
				t.Fatalf("ResolveMedia = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestResolveMediaGoesThroughTheSharedSendableCheck(t *testing.T) {
	cases := map[string]struct {
		library MediaLibrary
		id      string
	}{
		"reader let another workspace through": {looseLibrary{item: &media.Media{ID: "m1", WorkspaceID: "ws-2", URL: "https://cdn.example.com/a.jpg", Type: media.MediaTypeProductImage}}, "m1"},
		"note image id":                        {fakeLibrary{}, "note-image-1"},
		"editor asset":                         {fakeLibrary{"m1": {ID: "m1", WorkspaceID: "ws-1", URL: "https://cdn.example.com/a.cube", Type: media.MediaTypeLut}}, "m1"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.svc.Library = c.library
			h.svc.RemoteMedia = fakeRemote{contentType: "image/jpeg", size: 1}
			_, err := h.svc.ResolveMedia(context.Background(), "ws-1", c.id)
			if !errors.Is(err, conversation.ErrMediaNotSendable) || !errors.Is(err, media.ErrMediaNotFound) {
				t.Fatalf("ResolveMedia = %v, want the shared refusal that still reads as a missing file", err)
			}
		})
	}
}

const carrierCDN = "https://cdn.vozko.test/"

type carrierStorage struct{}

func (carrierStorage) OwnsURL(rawURL string) bool { return strings.HasPrefix(rawURL, carrierCDN) }

type carrierLibraryByURL map[string]*media.Media

func (l carrierLibraryByURL) GetMediaForWorkspaceByURL(workspaceID, url string) (*media.Media, error) {
	row, ok := l[url]
	if !ok || row.WorkspaceID != workspaceID {
		return nil, media.ErrMediaNotFound
	}
	return row, nil
}

type carrierUploadsByURL map[string]*conversation.ConversationMedia

func (u carrierUploadsByURL) GetInEntryByURL(entryID string, entryType shared.EntryType, url string) (*conversation.ConversationMedia, error) {
	row, ok := u[url]
	if !ok || row.EntryID != entryID || row.EntryType != entryType {
		return nil, conversation.ErrMediaNotFound
	}
	return row, nil
}

func carrierMediaURLs(library carrierLibraryByURL, uploads carrierUploadsByURL) *conversation.MediaURLResolver {
	return &conversation.MediaURLResolver{Storage: carrierStorage{}, Library: library, Uploads: uploads}
}

func imageAt(url string) *rcs.Media {
	return &rcs.Media{Kind: rcs.MediaImage, URL: url, MIMEType: "image/jpeg"}
}

func TestRCSContentMediaByURLFollowsTheSendableRule(t *testing.T) {
	library := carrierCDN + "ws-1/oferta.jpg"
	upload := carrierCDN + "conv-1/foto.jpg"
	note := carrierCDN + "conv-1/nota.jpg"
	foreign := carrierCDN + "ws-2/segredo.jpg"
	resolver := carrierMediaURLs(
		carrierLibraryByURL{
			library: {ID: "lib-1", WorkspaceID: "ws-1", URL: library, Type: media.MediaTypeProductImage},
			foreign: {ID: "lib-2", WorkspaceID: "ws-2", URL: foreign, Type: media.MediaTypeProductImage},
		},
		carrierUploadsByURL{
			upload: {ID: "up-1", EntryID: "conv-1", EntryType: shared.EntryTypeRCS, Type: conversation.MediaTypeImage, URL: upload, Purpose: conversation.MediaPurposeMessage},
			note:   {ID: "up-2", EntryID: "conv-1", EntryType: shared.EntryTypeRCS, Type: conversation.MediaTypeImage, URL: note, Purpose: conversation.MediaPurposeNote},
		},
	)
	contents := func(url string) map[string]rcs.Content {
		return map[string]rcs.Content{
			"media": {Text: "Oi", Media: imageAt(url)},
			"card":  {Cards: []rcs.Card{{Title: "Oferta", Media: imageAt(url)}}},
		}
	}

	for name, url := range map[string]string{"library item": library, "upload of this conversation": upload, "external file": "https://media.example.com/a.jpg"} {
		for shape, content := range contents(url) {
			t.Run(name+" as "+shape, func(t *testing.T) {
				h := newHarness(t)
				h.svc.MediaURLs = resolver
				if _, err := h.svc.Deliver(context.Background(), marketing(content)); err != nil {
					t.Fatalf("Deliver = %v", err)
				}
				if len(h.provider.sends) != 1 {
					t.Fatalf("sends = %d, want the message delivered", len(h.provider.sends))
				}
			})
		}
	}
	for name, url := range map[string]string{"note image": note, "another workspace's file": foreign, "file with no row": carrierCDN + "x.jpg"} {
		for shape, content := range contents(url) {
			t.Run(name+" as "+shape, func(t *testing.T) {
				h := newHarness(t)
				h.svc.MediaURLs = resolver
				_, err := h.svc.Deliver(context.Background(), marketing(content))
				if code := conversation.SendFailureCode(err, ""); code != conversation.MediaNotSendableCode {
					t.Fatalf("err = %v code %q, want %s", err, code, conversation.MediaNotSendableCode)
				}
				if len(h.provider.sends) != 0 || len(h.ledger.debits) != 0 || len(h.messages.all()) != 0 {
					t.Fatalf("sends %d debits %d messages %d, a refused file is never sent or charged", len(h.provider.sends), len(h.ledger.debits), len(h.messages.all()))
				}
			})
		}
	}
}

func TestTheCarrierServiceNeedsTheMediaURLResolver(t *testing.T) {
	h := newHarness(t)
	deps := h.svc.Deps
	deps.MediaURLs = nil
	if _, err := NewService(deps); !errors.Is(err, ErrDependencyMissing) {
		t.Fatalf("NewService = %v, want ErrDependencyMissing", err)
	}
}

package conversation

import (
	"errors"
	"testing"

	"vozko/domain/media"
	"vozko/domain/shared"
)

const ourCDN = "https://cdn.vozko.test/"

type cdnHosts struct{}

func (cdnHosts) OwnsURL(rawURL string) bool {
	return len(rawURL) >= len(ourCDN) && rawURL[:len(ourCDN)] == ourCDN
}

type libraryByURL map[string]*media.Media

func (r libraryByURL) GetMediaForWorkspaceByURL(workspaceID, url string) (*media.Media, error) {
	row, ok := r[url]
	if !ok || row.WorkspaceID != workspaceID {
		return nil, media.ErrMediaNotFound
	}
	return row, nil
}

type looseLibraryByURL struct{ row *media.Media }

func (l looseLibraryByURL) GetMediaForWorkspaceByURL(string, string) (*media.Media, error) {
	return l.row, nil
}

type uploadsByURL map[string]*ConversationMedia

func (r uploadsByURL) GetInEntryByURL(entryID string, entryType shared.EntryType, url string) (*ConversationMedia, error) {
	row, ok := r[url]
	if !ok || row.EntryID != entryID || row.EntryType != entryType {
		return nil, ErrMediaNotFound
	}
	return row, nil
}

type looseUploadsByURL struct{ row *ConversationMedia }

func (l looseUploadsByURL) GetInEntryByURL(string, shared.EntryType, string) (*ConversationMedia, error) {
	return l.row, nil
}

type failingURLRows struct{}

func (failingURLRows) GetMediaForWorkspaceByURL(string, string) (*media.Media, error) {
	return nil, errors.New("db down")
}

func (failingURLRows) GetInEntryByURL(string, shared.EntryType, string) (*ConversationMedia, error) {
	return nil, errors.New("db down")
}

var scopeOfEntry1 = MediaSendScope{WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp}

func libraryVideo(url, workspaceID string) *media.Media {
	return &media.Media{ID: "lib-1", WorkspaceID: workspaceID, URL: url, Type: media.MediaTypeProductVideo}
}

func uploadAt(url, entryID string, purpose MediaPurpose) *ConversationMedia {
	return &ConversationMedia{ID: "up-1", EntryID: entryID, EntryType: shared.EntryTypeWhatsApp, Type: MediaTypeImage, URL: url, Purpose: purpose}
}

func TestAnExternalURLKeepsItsOwnGuards(t *testing.T) {
	resolver := &MediaURLResolver{Storage: cdnHosts{}, Library: libraryByURL{}, Uploads: uploadsByURL{}}

	if err := resolver.SendableURL(scopeOfEntry1, " https://images.example.com/a.png "); err != nil {
		t.Fatalf("external URL refused: %v", err)
	}
}

func TestOurURLOfALibraryItemOfTheWorkspaceIsSendable(t *testing.T) {
	url := ourCDN + "ws-1/promo.mp4"
	resolver := &MediaURLResolver{Storage: cdnHosts{}, Library: libraryByURL{url: libraryVideo(url, "ws-1")}, Uploads: uploadsByURL{}}

	if err := resolver.SendableURL(scopeOfEntry1, url); err != nil {
		t.Fatalf("library URL refused: %v", err)
	}
}

func TestOurURLOfAnUploadOfThisConversationIsSendable(t *testing.T) {
	url := ourCDN + "conversations/entry-1/photo.jpg"
	resolver := &MediaURLResolver{Storage: cdnHosts{}, Library: libraryByURL{}, Uploads: uploadsByURL{url: uploadAt(url, "entry-1", MediaPurposeMessage)}}

	if err := resolver.SendableURL(scopeOfEntry1, url); err != nil {
		t.Fatalf("upload URL refused: %v", err)
	}
}

func TestOurURLThatNoRuleAllowsIsRefused(t *testing.T) {
	url := ourCDN + "files/x.jpg"
	cases := map[string]*MediaURLResolver{
		"note image of this conversation": {Storage: cdnHosts{}, Library: libraryByURL{}, Uploads: uploadsByURL{url: uploadAt(url, "entry-1", MediaPurposeNote)}},
		"upload of another conversation":  {Storage: cdnHosts{}, Library: libraryByURL{}, Uploads: uploadsByURL{url: uploadAt(url, "entry-2", MediaPurposeMessage)}},
		"file of another workspace":       {Storage: cdnHosts{}, Library: libraryByURL{url: libraryVideo(url, "ws-2")}, Uploads: uploadsByURL{}},
		"reader let another workspace in": {Storage: cdnHosts{}, Library: looseLibraryByURL{row: libraryVideo(url, "ws-2")}, Uploads: looseUploadsByURL{}},
		"reader let another entry in":     {Storage: cdnHosts{}, Library: looseLibraryByURL{}, Uploads: looseUploadsByURL{row: uploadAt(url, "entry-2", MediaPurposeMessage)}},
		"row of another address":          {Storage: cdnHosts{}, Library: looseLibraryByURL{row: libraryVideo(ourCDN+"other.mp4", "ws-1")}, Uploads: looseUploadsByURL{row: uploadAt(ourCDN+"other.jpg", "entry-1", MediaPurposeMessage)}},
		"private library kind":            {Storage: cdnHosts{}, Library: libraryByURL{url: {ID: "lib-1", WorkspaceID: "ws-1", URL: url, Type: media.MediaTypeLeadImport}}, Uploads: uploadsByURL{}},
		"file with no row":                {Storage: cdnHosts{}, Library: libraryByURL{}, Uploads: uploadsByURL{}},
		"lookups fail":                    {Storage: cdnHosts{}, Library: failingURLRows{}, Uploads: failingURLRows{}},
		"no row readers":                  {Storage: cdnHosts{}},
		"no storage":                      {Library: libraryByURL{url: libraryVideo(url, "ws-1")}},
	}
	for name, resolver := range cases {
		t.Run(name, func(t *testing.T) {
			assertMediaRefused(t, resolver.SendableURL(scopeOfEntry1, url))
		})
	}
}

func TestAnyURLIsRefusedWithoutAResolverOrAnAddress(t *testing.T) {
	var none *MediaURLResolver
	assertMediaRefused(t, none.SendableURL(scopeOfEntry1, "https://images.example.com/a.png"))

	resolver := &MediaURLResolver{Storage: cdnHosts{}, Library: libraryByURL{}, Uploads: uploadsByURL{}}
	assertMediaRefused(t, resolver.SendableURL(scopeOfEntry1, "  "))
}

func TestOurURLNeedsTheScopeItIsCheckedAgainst(t *testing.T) {
	url := ourCDN + "files/x.jpg"
	resolver := &MediaURLResolver{
		Storage: cdnHosts{},
		Library: looseLibraryByURL{row: libraryVideo(url, "")},
		Uploads: looseUploadsByURL{row: uploadAt(url, "", MediaPurposeMessage)},
	}

	assertMediaRefused(t, resolver.SendableURL(MediaSendScope{EntryType: shared.EntryTypeWhatsApp}, url))
}

func TestOwnsTellsWhetherAnAddressIsOurs(t *testing.T) {
	resolver := &MediaURLResolver{Storage: cdnHosts{}}
	if !resolver.Owns(ourCDN + "a.png") {
		t.Fatal("our CDN address must be ours")
	}
	if resolver.Owns("https://images.example.com/a.png") {
		t.Fatal("an external address must not be ours")
	}
	var none *MediaURLResolver
	if none.Owns("https://images.example.com/a.png") {
		t.Fatal("without storage nothing is known to be ours")
	}
}

package instagram

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	igdomain "vozko/domain/instagram"
)

// defaultMediaPageSize keeps a posts page small enough to stay well inside the
// per-account read budget, which scales with the account's audience activity
// (4800 x impressions per 24h) and is therefore tiny for a new tenant.
const defaultMediaPageSize = 24

// maxContainerPolls bounds how long we wait for Instagram to finish processing a
// publish container.
const (
	maxContainerPolls    = 20
	containerPollBackoff = 1500 * time.Millisecond
)

// accountResolver loads an account and enforces workspace ownership. Shared by
// every posts/comments usecase so the tenant check exists in exactly one place.
type accountResolver struct {
	accounts igdomain.AccountRepository
}

func (r accountResolver) resolve(ctx context.Context, workspaceID, accountID string) (*igdomain.Account, error) {
	account, err := r.accounts.FindByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	// A mismatch reads as not-found so an id from another tenant does not reveal
	// that it exists.
	if account.WorkspaceID != workspaceID {
		return nil, igdomain.ErrAccountNotFound
	}
	if account.AccessToken == "" {
		return nil, igdomain.ErrAccessTokenRequired
	}
	return account, nil
}

// ListMediaInput is one page of posts.
type ListMediaInput struct {
	WorkspaceID string
	AccountID   string
	Limit       int
	After       string
}

// ListMediaUseCase lists an account's posts.
//
// This is an on-demand proxy rather than a mirror: media_url and thumbnail_url
// are short-lived signed CDN links, so a stored copy would rot. Only durable
// fields are mirrored (for comment linkage and counters); the ephemeral URLs are
// served through the media proxy.
type ListMediaUseCase struct {
	accountResolver
	media     igdomain.MediaService
	mediaRepo igdomain.MediaRepository
}

func NewListMediaUseCase(
	accounts igdomain.AccountRepository,
	mediaSvc igdomain.MediaService,
	mediaRepo igdomain.MediaRepository,
) *ListMediaUseCase {
	return &ListMediaUseCase{
		accountResolver: accountResolver{accounts: accounts},
		media:           mediaSvc,
		mediaRepo:       mediaRepo,
	}
}

func (uc *ListMediaUseCase) Execute(ctx context.Context, in ListMediaInput) (*igdomain.Page[*igdomain.RemoteMedia], error) {
	account, err := uc.resolve(ctx, in.WorkspaceID, in.AccountID)
	if err != nil {
		return nil, err
	}
	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = defaultMediaPageSize
	}

	page, err := uc.media.ListMedia(ctx, account.IGUserID, account.AccessToken, limit, in.After)
	if err != nil {
		return nil, err
	}

	// Mirror the durable projection so comments and counters have something to
	// join against. Best effort: a mirror failure must not fail the read.
	uc.mirror(ctx, account, page.Items)
	return page, nil
}

func (uc *ListMediaUseCase) mirror(ctx context.Context, account *igdomain.Account, items []*igdomain.RemoteMedia) {
	if uc.mediaRepo == nil || len(items) == 0 {
		return
	}
	records := make([]*igdomain.Media, 0, len(items))
	for _, m := range items {
		if m == nil {
			continue
		}
		records = append(records, remoteToMedia(account, m))
	}
	if err := uc.mediaRepo.UpsertMany(ctx, records); err != nil {
		log.Printf("[instagram] media mirror failed account=%s: %v", account.IGUserID, err)
	}
}

// GetMediaUseCase reads one post with carousel children expanded.
type GetMediaUseCase struct {
	accountResolver
	media     igdomain.MediaService
	mediaRepo igdomain.MediaRepository
}

func NewGetMediaUseCase(
	accounts igdomain.AccountRepository,
	mediaSvc igdomain.MediaService,
	mediaRepo igdomain.MediaRepository,
) *GetMediaUseCase {
	return &GetMediaUseCase{
		accountResolver: accountResolver{accounts: accounts},
		media:           mediaSvc,
		mediaRepo:       mediaRepo,
	}
}

func (uc *GetMediaUseCase) Execute(ctx context.Context, workspaceID, accountID, igMediaID string) (*igdomain.RemoteMedia, error) {
	account, err := uc.resolve(ctx, workspaceID, accountID)
	if err != nil {
		return nil, err
	}
	remote, err := uc.media.GetMedia(ctx, account.AccessToken, igMediaID, true)
	if err != nil {
		return nil, err
	}
	if uc.mediaRepo != nil {
		if err := uc.mediaRepo.Upsert(ctx, remoteToMedia(account, remote)); err != nil {
			log.Printf("[instagram] media mirror failed media=%s: %v", igMediaID, err)
		}
	}
	return remote, nil
}

// ProxyMediaUseCase streams a post asset.
//
// The proxy exists because media_url expires: the browser is handed our URL,
// which resolves the current CDN link on demand. Nothing is stored, which also
// keeps us clear of Meta's guidance against caching media content.
type ProxyMediaUseCase struct {
	accountResolver
	media igdomain.MediaService
}

func NewProxyMediaUseCase(accounts igdomain.AccountRepository, mediaSvc igdomain.MediaService) *ProxyMediaUseCase {
	return &ProxyMediaUseCase{accountResolver: accountResolver{accounts: accounts}, media: mediaSvc}
}

// Execute returns the asset bytes and its content type. thumb selects the video
// thumbnail; note IMAGE media has no thumbnail_url, so it falls back to media_url.
func (uc *ProxyMediaUseCase) Execute(ctx context.Context, workspaceID, accountID, igMediaID string, thumb bool) ([]byte, string, error) {
	account, err := uc.resolve(ctx, workspaceID, accountID)
	if err != nil {
		return nil, "", err
	}
	remote, err := uc.media.GetMedia(ctx, account.AccessToken, igMediaID, false)
	if err != nil {
		return nil, "", err
	}

	url := remote.MediaURL
	if thumb && remote.ThumbnailURL != "" {
		url = remote.ThumbnailURL
	}
	if url == "" {
		// media_url is OMITTED (not null) for copyrighted content.
		return nil, "", fmt.Errorf("instagram: media %s has no downloadable asset", igMediaID)
	}
	return uc.media.FetchMediaBytes(ctx, url)
}

// ErrNoAvatar is returned when the account has no profile picture.
//
// This is a normal state, not a failure: Instagram OMITS profile_picture_url from
// the response entirely for an account that has never set a photo, the field is
// absent rather than empty, and the caller should fall back to a placeholder.
var ErrNoAvatar = errors.New("instagram: account has no profile picture")

// ProxyAvatarUseCase streams the account's profile picture.
//
// It re-reads the URL from Graph on every request rather than trusting the copy
// stored at connect time, for the same reason media is proxied: profile_picture_url
// is a signed CDN link with an expiry, so a stored value becomes a broken image,
// silently, and only some time after everything looked fine.
//
// The extra Graph call is bounded by the HTTP cache header on the response, and an
// avatar changes far less often than that window.
type ProxyAvatarUseCase struct {
	accountResolver
	oauth igdomain.OAuthService
	media igdomain.MediaService
}

func NewProxyAvatarUseCase(
	accounts igdomain.AccountRepository,
	oauth igdomain.OAuthService,
	mediaSvc igdomain.MediaService,
) *ProxyAvatarUseCase {
	return &ProxyAvatarUseCase{
		accountResolver: accountResolver{accounts: accounts},
		oauth:           oauth,
		media:           mediaSvc,
	}
}

// Execute returns the avatar bytes and its content type.
func (uc *ProxyAvatarUseCase) Execute(ctx context.Context, workspaceID, accountID string) ([]byte, string, error) {
	account, err := uc.resolve(ctx, workspaceID, accountID)
	if err != nil {
		return nil, "", err
	}

	profile, err := uc.oauth.GetProfile(ctx, account.AccessToken)
	if err != nil {
		return nil, "", err
	}
	if profile.ProfilePictureURL == "" {
		return nil, "", ErrNoAvatar
	}
	return uc.media.FetchMediaBytes(ctx, profile.ProfilePictureURL)
}

// CreateMediaInput publishes a post.
type CreateMediaInput struct {
	WorkspaceID string
	AccountID   string
	ImageURL    string
	VideoURL    string
	Caption     string
	// MediaType is REELS or STORIES; empty publishes a feed image.
	MediaType string
}

// CreateMediaUseCase publishes a post.
//
// Publishing is two-step and asynchronous: a container is created, Instagram
// processes it, and only then can it be published. We poll the container instead
// of publishing optimistically, because publishing an unfinished container fails.
type CreateMediaUseCase struct {
	accountResolver
	media     igdomain.MediaService
	mediaRepo igdomain.MediaRepository
}

func NewCreateMediaUseCase(
	accounts igdomain.AccountRepository,
	mediaSvc igdomain.MediaService,
	mediaRepo igdomain.MediaRepository,
) *CreateMediaUseCase {
	return &CreateMediaUseCase{
		accountResolver: accountResolver{accounts: accounts},
		media:           mediaSvc,
		mediaRepo:       mediaRepo,
	}
}

func (uc *CreateMediaUseCase) Execute(ctx context.Context, in CreateMediaInput) (*igdomain.RemoteMedia, error) {
	account, err := uc.resolve(ctx, in.WorkspaceID, in.AccountID)
	if err != nil {
		return nil, err
	}
	if !account.CanPublishContent() {
		return nil, fmt.Errorf("instagram account %s cannot publish (missing %s)",
			account.Username, igdomain.ScopeContentPublish)
	}
	if strings.TrimSpace(in.ImageURL) == "" && strings.TrimSpace(in.VideoURL) == "" {
		return nil, fmt.Errorf("instagram: publishing requires an image or video URL")
	}

	containerID, err := uc.media.CreateContainer(ctx, account.IGUserID, account.AccessToken, igdomain.CreateMediaInput{
		ImageURL:  in.ImageURL,
		VideoURL:  in.VideoURL,
		Caption:   in.Caption,
		MediaType: in.MediaType,
	})
	if err != nil {
		return nil, err
	}

	if err := uc.awaitContainer(ctx, account, containerID); err != nil {
		return nil, err
	}

	igMediaID, err := uc.media.PublishContainer(ctx, account.IGUserID, account.AccessToken, containerID)
	if err != nil {
		return nil, err
	}

	remote, err := uc.media.GetMedia(ctx, account.AccessToken, igMediaID, true)
	if err != nil {
		// The post is published; failing the request now would be misleading.
		log.Printf("[instagram] published %s but could not read it back: %v", igMediaID, err)
		return &igdomain.RemoteMedia{IGMediaID: igMediaID}, nil
	}
	if uc.mediaRepo != nil {
		if err := uc.mediaRepo.Upsert(ctx, remoteToMedia(account, remote)); err != nil {
			log.Printf("[instagram] media mirror failed media=%s: %v", igMediaID, err)
		}
	}
	return remote, nil
}

// awaitContainer polls until the container is ready, failed, or we give up.
func (uc *CreateMediaUseCase) awaitContainer(ctx context.Context, account *igdomain.Account, containerID string) error {
	for attempt := 0; attempt < maxContainerPolls; attempt++ {
		status, err := uc.media.GetContainerStatus(ctx, account.AccessToken, containerID)
		if err != nil {
			return err
		}
		switch {
		case status.Ready():
			return nil
		case status.Failed():
			return fmt.Errorf("instagram: media container %s failed: %s (%s)",
				containerID, status.StatusCode, status.Status)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(containerPollBackoff):
		}
	}
	return fmt.Errorf("instagram: media container %s was still processing after %d polls",
		containerID, maxContainerPolls)
}

// SetCommentEnabledUseCase toggles comments on a post.
//
// This is the ONLY supported update on a published post, Instagram has no
// endpoint to edit a caption, so caption changes are rejected in the domain
// rather than attempted.
type SetCommentEnabledUseCase struct {
	accountResolver
	media     igdomain.MediaService
	mediaRepo igdomain.MediaRepository
}

func NewSetCommentEnabledUseCase(
	accounts igdomain.AccountRepository,
	mediaSvc igdomain.MediaService,
	mediaRepo igdomain.MediaRepository,
) *SetCommentEnabledUseCase {
	return &SetCommentEnabledUseCase{
		accountResolver: accountResolver{accounts: accounts},
		media:           mediaSvc,
		mediaRepo:       mediaRepo,
	}
}

func (uc *SetCommentEnabledUseCase) Execute(ctx context.Context, workspaceID, accountID, igMediaID string, enabled bool) error {
	account, err := uc.resolve(ctx, workspaceID, accountID)
	if err != nil {
		return err
	}
	if err := uc.media.SetCommentEnabled(ctx, account.AccessToken, igMediaID, enabled); err != nil {
		return err
	}
	if uc.mediaRepo != nil {
		if err := uc.mediaRepo.SetCommentEnabled(ctx, account.ID, igMediaID, enabled); err != nil {
			log.Printf("[instagram] comment-enabled mirror failed media=%s: %v", igMediaID, err)
		}
	}
	return nil
}

// remoteToMedia projects a Graph post onto the durable row. Note the absence of
// MediaURL/ThumbnailURL: those expire and are never persisted.
func remoteToMedia(account *igdomain.Account, m *igdomain.RemoteMedia) *igdomain.Media {
	return &igdomain.Media{
		WorkspaceID:      account.WorkspaceID,
		IGAccountID:      account.ID,
		IGMediaID:        m.IGMediaID,
		MediaType:        m.MediaType,
		MediaProductType: m.MediaProductType,
		Caption:          m.Caption,
		Permalink:        m.Permalink,
		Shortcode:        m.Shortcode,
		Timestamp:        m.Timestamp,
		LikeCount:        m.LikeCount,
		CommentsCount:    m.CommentsCount,
		IsCommentEnabled: m.IsCommentEnabled,
	}
}

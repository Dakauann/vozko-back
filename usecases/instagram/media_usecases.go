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

const defaultMediaPageSize = 24

const (
	maxContainerPolls    = 20
	containerPollBackoff = 1500 * time.Millisecond
)

type accountResolver struct {
	accounts igdomain.AccountRepository
}

func (r accountResolver) resolve(ctx context.Context, workspaceID, accountID string) (*igdomain.Account, error) {
	account, err := r.accounts.FindByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.WorkspaceID != workspaceID {
		return nil, igdomain.ErrAccountNotFound
	}
	if account.AccessToken == "" {
		return nil, igdomain.ErrAccessTokenRequired
	}
	return account, nil
}

type ListMediaInput struct {
	WorkspaceID string
	AccountID   string
	Limit       int
	After       string
}

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

type ProxyMediaUseCase struct {
	accountResolver
	media igdomain.MediaService
}

func NewProxyMediaUseCase(accounts igdomain.AccountRepository, mediaSvc igdomain.MediaService) *ProxyMediaUseCase {
	return &ProxyMediaUseCase{accountResolver: accountResolver{accounts: accounts}, media: mediaSvc}
}

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
		return nil, "", fmt.Errorf("instagram: media %s has no downloadable asset", igMediaID)
	}
	return uc.media.FetchMediaBytes(ctx, url)
}

var ErrNoAvatar = errors.New("instagram: account has no profile picture")

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

func (uc *ProxyAvatarUseCase) Execute(ctx context.Context, workspaceID, accountID string) ([]byte, string, error) {
	account, err := uc.resolve(ctx, workspaceID, accountID)
	if err != nil {
		return nil, "", err
	}

	avatarURL := account.ProfilePictureURL
	profile, err := uc.oauth.GetProfile(ctx, account.AccessToken)
	if err == nil && profile.ProfilePictureURL != "" {
		avatarURL = profile.ProfilePictureURL
	} else if avatarURL == "" {
		if err != nil {
			log.Printf("[instagram] avatar profile lookup failed account=%s: %v", accountID, err)
			return nil, "", err
		}
		return nil, "", ErrNoAvatar
	}
	if err != nil {
		log.Printf("[instagram] avatar profile lookup failed account=%s; trying stored URL: %v", accountID, err)
	}

	data, contentType, err := uc.media.FetchMediaBytes(ctx, avatarURL)
	if err == nil {
		return data, contentType, nil
	}
	log.Printf("[instagram] avatar CDN fetch failed account=%s: %v", accountID, err)
	if account.ProfilePictureURL != "" && account.ProfilePictureURL != avatarURL {
		return uc.media.FetchMediaBytes(ctx, account.ProfilePictureURL)
	}
	return nil, "", err
}

type CreateMediaInput struct {
	WorkspaceID string
	AccountID   string
	ImageURL    string
	VideoURL    string
	Caption     string
	MediaType   string
}

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

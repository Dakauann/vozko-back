package instagram

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/cache"
	igdomain "vozko/domain/instagram"
	"vozko/infra/meta"
)

const nonceTTL = 15 * time.Minute

type StartConnectInput struct {
	WorkspaceID string
	UserID      string
	ReturnPath  string
	Popup       bool
}

type StartConnectOutput struct {
	AuthorizeURL string
}

type CompleteConnectInput struct {
	Code        string
	State       string
	Error       string
	ErrorReason string
}

type CompleteConnectOutput struct {
	Account     *igdomain.Account
	ReturnPath  string
	Popup       bool
	Reconnected bool
}

type ConnectAccountUseCase struct {
	oauth        igdomain.OAuthService
	subscription igdomain.SubscriptionService
	messaging    igdomain.MessagingService
	accounts     igdomain.AccountRepository
	sharedState  cache.SharedState

	appSecret         string
	defaultReturnPath string
}

func NewConnectAccountUseCase(
	oauth igdomain.OAuthService,
	subscription igdomain.SubscriptionService,
	messaging igdomain.MessagingService,
	accounts igdomain.AccountRepository,
	sharedState cache.SharedState,
	appSecret string,
	defaultReturnPath string,
) *ConnectAccountUseCase {
	if defaultReturnPath == "" {
		defaultReturnPath = "/dashboard/instagram-accounts"
	}
	return &ConnectAccountUseCase{
		oauth:             oauth,
		subscription:      subscription,
		messaging:         messaging,
		accounts:          accounts,
		sharedState:       sharedState,
		appSecret:         appSecret,
		defaultReturnPath: defaultReturnPath,
	}
}

func (uc *ConnectAccountUseCase) Start(ctx context.Context, in StartConnectInput) (*StartConnectOutput, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return nil, igdomain.ErrWorkspaceIDRequired
	}

	nonce, err := NewNonce()
	if err != nil {
		return nil, fmt.Errorf("instagram: mint nonce: %w", err)
	}

	state, err := EncodeState(OAuthState{
		WorkspaceID: in.WorkspaceID,
		UserID:      in.UserID,
		Nonce:       nonce,
		ExpiresAt:   time.Now().UTC().Add(stateTTL),
		ReturnPath:  SafeReturnPath(in.ReturnPath, uc.defaultReturnPath),
		Popup:       in.Popup,
	}, uc.appSecret)
	if err != nil {
		return nil, err
	}

	if uc.sharedState != nil {
		ok, err := uc.sharedState.SetNX(nonceKey(nonce), in.WorkspaceID, nonceTTL)
		if err != nil {
			return nil, fmt.Errorf("instagram: persist oauth nonce: %w", err)
		}
		if !ok {
			return nil, fmt.Errorf("instagram: oauth nonce collision")
		}
	}

	return &StartConnectOutput{AuthorizeURL: uc.oauth.BuildAuthorizeURL(state)}, nil
}

func (uc *ConnectAccountUseCase) Complete(ctx context.Context, in CompleteConnectInput) (*CompleteConnectOutput, error) {
	log.Printf("[instagram] callback received (code=%t state=%t error=%q reason=%q)",
		in.Code != "", in.State != "", in.Error, in.ErrorReason)

	state, err := DecodeState(in.State, uc.appSecret)
	if err != nil {
		log.Printf("[instagram] state rejected: %v", err)
		return nil, err
	}
	log.Printf("[instagram] state ok (workspace=%s user=%s popup=%t returnPath=%s)",
		state.WorkspaceID, state.UserID, state.Popup, state.ReturnPath)

	if uc.sharedState != nil {
		issued, err := uc.sharedState.Exists(nonceKey(state.Nonce))
		if err != nil {
			return nil, fmt.Errorf("instagram: verify oauth nonce: %w", err)
		}
		if !issued {
			log.Printf("[instagram] nonce %s not found (expired, or the callback was replayed)", state.Nonce)
			return nil, ErrReplayedState
		}
		claimed, err := uc.sharedState.SetNX(nonceUsedKey(state.Nonce), "1", nonceTTL)
		if err != nil {
			return nil, fmt.Errorf("instagram: consume oauth nonce: %w", err)
		}
		if !claimed {
			log.Printf("[instagram] nonce %s already consumed; refusing a replayed callback", state.Nonce)
			return nil, ErrReplayedState
		}
		_ = uc.sharedState.Del(nonceKey(state.Nonce))
	}

	returnPath := SafeReturnPath(state.ReturnPath, uc.defaultReturnPath)

	if in.Error != "" || in.ErrorReason != "" {
		log.Printf("[instagram] user declined authorization: error=%q reason=%q", in.Error, in.ErrorReason)
		return nil, fmt.Errorf("instagram: authorization declined (%s/%s)", in.Error, in.ErrorReason)
	}
	if strings.TrimSpace(in.Code) == "" {
		return nil, fmt.Errorf("instagram: authorization code is required")
	}

	shortLived, err := uc.oauth.ExchangeCode(ctx, in.Code)
	if err != nil {
		return nil, fmt.Errorf("instagram: exchange code: %w", err)
	}
	log.Printf("[instagram] code exchanged (user_id=%s, permissions=%v)",
		shortLived.UserID, shortLived.Permissions)

	longLived, err := uc.oauth.ExchangeForLongLived(ctx, shortLived.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("instagram: exchange for long-lived token: %w", err)
	}
	log.Printf("[instagram] long-lived token obtained (expires_in=%s permissions=%v)",
		longLived.ExpiresIn, longLived.Permissions)

	profile, err := uc.oauth.GetProfile(ctx, longLived.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("instagram: read profile: %w", err)
	}
	log.Printf("[instagram] profile read (ig_user_id=%s username=%s type=%s)",
		profile.IGUserID, profile.Username, profile.AccountType)

	granted := longLived.Permissions
	if len(granted) == 0 {
		granted = shortLived.Permissions
	}

	now := time.Now().UTC()
	expiresAt := now.Add(longLived.ExpiresIn)

	account := &igdomain.Account{
		WorkspaceID:       state.WorkspaceID,
		IGUserID:          profile.IGUserID,
		Username:          profile.Username,
		Name:              profile.Name,
		AccountType:       profile.AccountType,
		ProfilePictureURL: profile.ProfilePictureURL,
		FollowersCount:    profile.FollowersCount,
		FollowsCount:      profile.FollowsCount,
		MediaCount:        profile.MediaCount,
		AccessToken:       longLived.AccessToken,
		TokenExpiresAt:    &expiresAt,
		TokenRefreshedAt:  &now,
		GrantedScopes:     granted,
		Status:            igdomain.StatusConnected,
	}
	account.Normalize()
	if err := account.Validate(); err != nil {
		return nil, err
	}

	if len(granted) == 0 {
		log.Printf("[instagram] account %s: Instagram did not report granted permissions; assuming the requested scopes",
			profile.IGUserID)
		account.GrantedScopes = igdomain.RequiredScopes()
	} else if !account.HasScope(igdomain.ScopeManageMessages) {
		return nil, fmt.Errorf("%w: granted %v", igdomain.ErrMissingMessagingScope, granted)
	}

	log.Printf("[instagram] persisting account ig_user_id=%s username=%s workspace=%s scopes=%v",
		account.IGUserID, account.Username, account.WorkspaceID, account.GrantedScopes)

	reconnected, err := uc.persist(ctx, account)
	if err != nil {
		log.Printf("[instagram] persist failed for ig_user_id=%s: %v", account.IGUserID, err)
		return nil, err
	}
	log.Printf("[instagram] account persisted id=%s ig_user_id=%s reconnected=%t",
		account.ID, account.IGUserID, reconnected)

	log.Printf("[instagram] connect complete for @%s (id=%s)", account.Username, account.ID)

	uc.subscribeWebhooks(ctx, account)
	uc.probeMessagingHealth(ctx, account)

	return &CompleteConnectOutput{
		Account:     account,
		ReturnPath:  returnPath,
		Popup:       state.Popup,
		Reconnected: reconnected,
	}, nil
}

func (uc *ConnectAccountUseCase) persist(ctx context.Context, account *igdomain.Account) (bool, error) {
	existing, err := uc.accounts.FindByIGUserIDUnscoped(ctx, account.IGUserID)
	switch {
	case err == nil:
		log.Printf("[instagram] ig_user_id=%s already exists (id=%s workspace=%s), restoring and updating",
			account.IGUserID, existing.ID, existing.WorkspaceID)
		if existing.WorkspaceID != account.WorkspaceID {
			return false, fmt.Errorf("%w: already connected to another workspace",
				igdomain.ErrAccountAlreadyLinked)
		}
		account.ID = existing.ID
		account.AgentID = existing.AgentID
		account.WorkflowID = existing.WorkflowID
		account.PipelineID = existing.PipelineID
		account.EnableAgentResponses = existing.EnableAgentResponses
		account.EnableWorkflow = existing.EnableWorkflow
		account.EnableAnalysis = existing.EnableAnalysis
		account.EnableAutoStaging = existing.EnableAutoStaging
		account.EnableAutoMemory = existing.EnableAutoMemory
		account.DepartmentID = existing.DepartmentID

		if err := uc.accounts.Restore(ctx, existing.ID); err != nil {
			return false, err
		}
		if err := uc.accounts.Update(ctx, account); err != nil {
			return false, err
		}
		if err := uc.accounts.UpdateToken(ctx, account.ID, account.AccessToken,
			*account.TokenExpiresAt, *account.TokenRefreshedAt); err != nil {
			return false, err
		}
		return true, nil

	case errors.Is(err, igdomain.ErrAccountNotFound):
		log.Printf("[instagram] ig_user_id=%s is new, creating", account.IGUserID)
		if err := uc.accounts.Create(ctx, account); err != nil {
			return false, err
		}
		return false, nil

	default:
		return false, err
	}
}

func (uc *ConnectAccountUseCase) subscribeWebhooks(ctx context.Context, account *igdomain.Account) {
	if uc.subscription == nil {
		return
	}
	fields := igdomain.SubscribedFields()
	if err := uc.subscription.Subscribe(ctx, account.IGUserID, account.AccessToken, fields); err != nil {
		log.Printf("[instagram] webhook subscription FAILED account=%s fields=%v: %v", account.IGUserID, fields, err)
		return
	}
	log.Printf("[instagram] webhook subscribed account=%s fields=%v", account.IGUserID, fields)
	now := time.Now().UTC()
	account.WebhookSubscribedAt = &now
	if err := uc.accounts.SetWebhookSubscribedAt(ctx, account.ID, now); err != nil {
		log.Printf("[instagram] persist webhook subscription failed account=%s: %v", account.IGUserID, err)
	}
}

func (uc *ConnectAccountUseCase) probeMessagingHealth(ctx context.Context, account *igdomain.Account) {
	if uc.messaging == nil {
		return
	}
	now := time.Now().UTC()
	healthy := true

	if err := uc.messaging.GetConversations(ctx, account.IGUserID, account.AccessToken, 1); err != nil {
		if apiErr, ok := meta.AsError(err); ok && !apiErr.Retryable() {
			healthy = false
			log.Printf("[instagram] messaging health probe failed account=%s code=%d: %v",
				account.IGUserID, apiErr.Code, err)
		} else {
			log.Printf("[instagram] messaging health probe inconclusive account=%s: %v",
				account.IGUserID, err)
			return
		}
	}

	log.Printf("[instagram] messaging health for account=%s: healthy=%t", account.IGUserID, healthy)
	account.MessagingHealthy = healthy
	account.MessagingCheckedAt = &now
	if err := uc.accounts.UpdateMessagingHealth(ctx, account.ID, healthy, now); err != nil {
		log.Printf("[instagram] persist messaging health failed account=%s: %v", account.IGUserID, err)
	}
}

func nonceKey(nonce string) string     { return "ig:oauth:nonce:" + nonce }
func nonceUsedKey(nonce string) string { return "ig:oauth:used:" + nonce }

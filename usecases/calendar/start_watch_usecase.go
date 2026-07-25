package calendar_usecase

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/calendar"
)

type startWatchUseCase struct {
	repo   calendar.Repository
	google calendar.GoogleOAuthService
}

func NewStartWatchUseCase(repo calendar.Repository, google calendar.GoogleOAuthService) calendar.StartWatchUseCase {
	return &startWatchUseCase{repo: repo, google: google}
}

func (uc *startWatchUseCase) Execute(workspaceID string) (*calendar.CalendarWatchChannel, error) {
	conn, err := uc.repo.GetConnection(workspaceID)
	if err != nil || conn == nil {
		return nil, calendar.ErrGoogleNotConnected
	}

	accessToken, err := ensureValidToken(uc.google, uc.repo, conn)
	if err != nil {
		return nil, fmt.Errorf("google auth: %w", err)
	}

	existing, err := uc.repo.GetWatchChannelByWorkspace(workspaceID)
	if err != nil {
		return nil, fmt.Errorf("check existing watch: %w", err)
	}
	if existing != nil {
		_ = uc.google.StopChannel(accessToken, existing.ChannelID, existing.ResourceID)
		if err := uc.repo.DeleteWatchChannel(existing.ID); err != nil {
			log.Printf("[calendar-watch] failed to delete old channel %s: %v", existing.ID, err)
		}
	}

	channelID := uuid.New().String()
	token := generateSecureToken(32)
	webhookURL := getWebhookURL()

	resp, err := uc.google.WatchEvents(accessToken, channelID, token, webhookURL)
	if err != nil {
		return nil, fmt.Errorf("google watch: %w", err)
	}

	ch := &calendar.CalendarWatchChannel{
		WorkspaceID: workspaceID,
		ChannelID:   resp.ChannelID,
		ResourceID:  resp.ResourceID,
		Token:       token,
		Expiration:  resp.Expiration,
	}
	if err := uc.repo.SaveWatchChannel(ch); err != nil {

		_ = uc.google.StopChannel(accessToken, resp.ChannelID, resp.ResourceID)
		return nil, fmt.Errorf("save watch channel: %w", err)
	}

	if conn.SyncToken == "" {
		uc.seedSyncToken(accessToken, conn)
	}

	log.Printf("[calendar-watch] started watch for workspace %s, channelID=%s, expires=%s",
		workspaceID, ch.ChannelID, ch.Expiration)
	return ch, nil
}

func (uc *startWatchUseCase) seedSyncToken(accessToken string, conn *calendar.GoogleCalendarConnection) {

	result, err := uc.google.ListEventsIncremental(accessToken, "")
	if err != nil || result == nil || result.SyncExpired {

		log.Printf("[calendar-watch] could not seed sync token: %v", err)
		return
	}
	if result.NextSyncToken != "" {
		if err := uc.repo.UpdateConnectionSyncToken(conn.ID, result.NextSyncToken); err != nil {
			log.Printf("[calendar-watch] failed to save initial sync token: %v", err)
		}
	}
}

func generateSecureToken(length int) string {
	b := make([]byte, length)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func getWebhookURL() string {
	apiBase := os.Getenv("API_BASE_URL")
	if apiBase == "" {
		apiBase = "http://localhost:8080"
	}
	return strings.TrimRight(apiBase, "/") + "/webhooks/google-calendar"
}

package whatsapp_campaign_usecase

import (
	"crypto/rand"
	"fmt"
	"math/big"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	wc "vozko/domain/whatsapp_campaign"
)

type clearHistoryUseCase struct {
	campaignRepo wc.Repository
	messageRepo  conversation.MessageRepository
}

func NewClearHistoryUseCase(
	campaignRepo wc.Repository,
	messageRepo conversation.MessageRepository,
) wc.ClearHistoryUseCase {
	return &clearHistoryUseCase{
		campaignRepo: campaignRepo,
		messageRepo:  messageRepo,
	}
}

func (uc *clearHistoryUseCase) PrepareClearHistory(campaignID string) (*wc.PrepareClearHistoryOutput, error) {
	if campaignID == "" {
		return nil, wc.ErrCampaignNotFound
	}

	existing, err := uc.campaignRepo.FindByID(campaignID)
	if err != nil {
		return nil, err
	}

	if existing.Status == wc.CampaignStatusRunning {
		return nil, wc.ErrCampaignClearNotAllowed
	}

	messageCount, err := uc.messageRepo.CountByCampaignID(campaignID, shared.EntryTypeWhatsApp)
	if err != nil {
		return nil, fmt.Errorf("failed to count messages: %w", err)
	}

	clearCode, err := generateClearCode()
	if err != nil {
		return nil, fmt.Errorf("failed to generate clear code: %w", err)
	}

	if err := uc.campaignRepo.UpdateClearCode(campaignID, clearCode); err != nil {
		return nil, err
	}

	return &wc.PrepareClearHistoryOutput{
		CampaignID:   campaignID,
		ClearCode:    clearCode,
		MessageCount: messageCount,
		Message:      fmt.Sprintf("Use this code to confirm clearing %d conversation message(s) for this campaign. This action cannot be undone.", messageCount),
	}, nil
}

func (uc *clearHistoryUseCase) ConfirmClearHistory(input wc.ClearHistoryInput) (*wc.ClearHistoryOutput, error) {
	if input.CampaignID == "" {
		return nil, wc.ErrCampaignNotFound
	}

	if input.ClearCode == "" {
		return nil, wc.ErrCampaignClearCodeInvalid
	}

	existing, err := uc.campaignRepo.FindByID(input.CampaignID)
	if err != nil {
		return nil, err
	}

	if existing.Status == wc.CampaignStatusRunning {
		return nil, wc.ErrCampaignClearNotAllowed
	}

	if existing.ClearCode == "" || existing.ClearCode != input.ClearCode {
		return nil, wc.ErrCampaignClearCodeInvalid
	}

	deletedCount, err := uc.messageRepo.DeleteByCampaignID(input.CampaignID, shared.EntryTypeWhatsApp)
	if err != nil {
		return nil, fmt.Errorf("failed to delete messages: %w", err)
	}

	if err := uc.campaignRepo.UpdateClearCode(input.CampaignID, ""); err != nil {
		return nil, err
	}

	updated, err := uc.campaignRepo.FindByID(input.CampaignID)
	if err != nil {
		return nil, err
	}

	return &wc.ClearHistoryOutput{
		Campaign:     updated,
		DeletedCount: deletedCount,
	}, nil
}

func generateClearCode() (string, error) {
	const digits = "0123456789"
	code := make([]byte, 6)
	for i := range code {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(digits))))
		if err != nil {
			return "", err
		}
		code[i] = digits[n.Int64()]
	}
	return string(code), nil
}

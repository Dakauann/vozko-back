package whatsapp_campaign_usecase

import (
	"crypto/rand"
	"fmt"
	"math/big"

	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type resetCampaignUseCase struct {
	campaignRepo wc.Repository
	entryRepo    wce.Repository
}

func NewResetCampaignUseCase(
	campaignRepo wc.Repository,
	entryRepo wce.Repository,
) wc.ResetCampaignUseCase {
	return &resetCampaignUseCase{
		campaignRepo: campaignRepo,
		entryRepo:    entryRepo,
	}
}

func (uc *resetCampaignUseCase) PrepareReset(campaignID string) (*wc.PrepareResetOutput, error) {
	if campaignID == "" {
		return nil, wc.ErrCampaignNotFound
	}

	existing, err := uc.campaignRepo.FindByID(campaignID)
	if err != nil {
		return nil, err
	}

	if existing.Status == wc.CampaignStatusRunning {
		return nil, wc.ErrCampaignResetNotAllowed
	}

	resetCode, err := generateResetCode()
	if err != nil {
		return nil, fmt.Errorf("failed to generate reset code: %w", err)
	}

	if err := uc.campaignRepo.UpdateResetCode(campaignID, resetCode); err != nil {
		return nil, err
	}

	return &wc.PrepareResetOutput{
		CampaignID: campaignID,
		ResetCode:  resetCode,
		Message:    "Use this code to confirm the campaign reset. This will reset all entries to PENDING status.",
	}, nil
}

func (uc *resetCampaignUseCase) ConfirmReset(input wc.ResetCampaignInput) (*wc.ResetCampaignOutput, error) {
	if input.CampaignID == "" {
		return nil, wc.ErrCampaignNotFound
	}

	if input.ResetCode == "" {
		return nil, wc.ErrCampaignResetCodeInvalid
	}

	existing, err := uc.campaignRepo.FindByID(input.CampaignID)
	if err != nil {
		return nil, err
	}

	if existing.Status == wc.CampaignStatusRunning {
		return nil, wc.ErrCampaignResetNotAllowed
	}

	if existing.ResetCode == "" || existing.ResetCode != input.ResetCode {
		return nil, wc.ErrCampaignResetCodeInvalid
	}

	resetCount, err := uc.entryRepo.ResetAllStatuses(input.CampaignID)
	if err != nil {
		return nil, fmt.Errorf("failed to reset entries: %w", err)
	}

	if err := uc.campaignRepo.UpdateResetCode(input.CampaignID, ""); err != nil {
		return nil, err
	}

	_, err = uc.campaignRepo.UpdateStatus(input.CampaignID, wc.CampaignStatusStopped)
	if err != nil {
		return nil, err
	}

	newResetCode, _ := generateResetCode()

	updated, err := uc.campaignRepo.FindByID(input.CampaignID)
	if err != nil {
		return nil, err
	}

	counts, err := uc.entryRepo.CountByStatus(input.CampaignID)
	if err != nil {
		return nil, err
	}
	updated.Metrics = wc.NewCampaignMetrics(counts)

	return &wc.ResetCampaignOutput{
		Campaign:     updated,
		ResetCount:   resetCount,
		NewResetCode: newResetCode,
	}, nil
}

func generateResetCode() (string, error) {
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

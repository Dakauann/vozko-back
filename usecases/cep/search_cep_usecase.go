package cep_usecase

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"vozko/domain/cep"
)

type searchCEPUseCase struct {
	repo   cep.CEPRepository
	client *http.Client
}

func NewSearchCEPUseCase(repo cep.CEPRepository, client *http.Client) cep.CEPSearchUseCase {
	if client == nil {
		client = http.DefaultClient
	}
	return &searchCEPUseCase{
		repo:   repo,
		client: client,
	}
}

func (uc *searchCEPUseCase) Execute(cepCode string) (*cep.CEPInfo, error) {
	cleanedCEP := uc.cleanCEP(cepCode)

	if !uc.isValidCEP(cleanedCEP) {
		return nil, fmt.Errorf("invalid CEP format")
	}

	dbCEP, err := uc.repo.GetByCode(cleanedCEP)
	if err != nil {
		return nil, fmt.Errorf("failed to get CEP from database: %w", err)
	}

	if dbCEP != nil {
		return dbCEP, nil
	}

	url := fmt.Sprintf("https://viacep.com.br/ws/%s/json/", cleanedCEP)

	resp, err := uc.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch CEP data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CEP not found")
	}

	var cepInfo cep.CEPInfo
	if err := json.NewDecoder(resp.Body).Decode(&cepInfo); err != nil {
		return nil, fmt.Errorf("failed to parse CEP data: %w", err)
	}

	if cepInfo.Erro {
		return nil, fmt.Errorf("CEP not found")
	}

	cepInfo.Cep = cleanedCEP

	err = uc.repo.Create(&cepInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to save CEP to database: %w", err)
	}

	return &cepInfo, nil
}

func (uc *searchCEPUseCase) cleanCEP(cepCode string) string {
	cepCode = strings.ReplaceAll(cepCode, ".", "")
	cepCode = strings.ReplaceAll(cepCode, "-", "")
	cepCode = strings.ReplaceAll(cepCode, " ", "")
	return cepCode
}

func (uc *searchCEPUseCase) isValidCEP(cep string) bool {
	validCEP := regexp.MustCompile(`^\d{8}$`)
	return validCEP.MatchString(cep)
}

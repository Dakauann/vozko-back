package agent_usecase

import (
	"errors"
	"strings"

	"vozko/domain/agent"
	businessphone "vozko/domain/whatsapp/business_phone"
)

func validateBusinessPhoneOwnership(repo businessphone.Repository, workspaceID, businessPhoneID string) error {
	businessPhoneID = strings.TrimSpace(businessPhoneID)
	if businessPhoneID == "" || repo == nil {
		return nil
	}

	phone, err := repo.FindByID(businessPhoneID)
	if err != nil {
		if errors.Is(err, businessphone.ErrPhoneNumberNotFound) {
			return agent.ErrAgentBusinessPhoneNotFound
		}
		return err
	}
	if phone == nil {
		return agent.ErrAgentBusinessPhoneNotFound
	}
	if !phone.BelongsToWorkspace(workspaceID) {
		return agent.ErrAgentBusinessPhoneNoAccess
	}
	return nil
}

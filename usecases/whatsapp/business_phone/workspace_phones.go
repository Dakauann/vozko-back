package businessphone_usecase

import (
	"fmt"
	"strings"

	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
)

type PhoneGrants interface {
	GetPhoneIDsForWorkspace(workspaceID string) ([]string, error)
}

type workspacePhones struct {
	list   businessphone.ListUseCase
	grants PhoneGrants
}

func NewWorkspacePhonesUseCase(list businessphone.ListUseCase, grants PhoneGrants) businessphone.WorkspacePhonesUseCase {
	return &workspacePhones{list: list, grants: grants}
}

func (uc *workspacePhones) List(workspaceID string, input businessphone.ListInput) (*shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber], error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, businessphone.ErrWorkspaceRequired
	}
	input.OwnerWorkspaceID = workspaceID
	input.AccessPhoneIDs = nil
	if uc.grants != nil {
		granted, err := uc.grants.GetPhoneIDsForWorkspace(workspaceID)
		if err != nil {
			return nil, fmt.Errorf("phone grants of %s: %w", workspaceID, err)
		}
		input.AccessPhoneIDs = granted
	}
	return uc.list.Execute(input)
}

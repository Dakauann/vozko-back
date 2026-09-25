package businessphone

import (
	"errors"

	"vozko/domain/shared"
)

var ErrWorkspaceRequired = errors.New("whatsapp business phone: workspace is required")

type WorkspacePhonesUseCase interface {
	List(workspaceID string, input ListInput) (*shared.PaginatedResult[*WhatsAppBusinessPhoneNumber], error)
}

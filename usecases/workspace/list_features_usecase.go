package workspace_usecase

import "vozko/domain/workspace"

type listFeaturesUseCase struct{}

func NewListFeaturesUseCase() workspace.ListFeaturesUseCase {
	return listFeaturesUseCase{}
}

func (listFeaturesUseCase) Execute() []workspace.Feature {
	return append([]workspace.Feature(nil), workspace.Features...)
}

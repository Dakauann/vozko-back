package dealautomation_usecase

import (
	"strings"
	"time"

	"vozko/domain/dealautomation"
	"vozko/domain/shared"
	"vozko/domain/stage"
	"vozko/domain/workspace"
)

type AccessChecker interface {
	Execute(userID, workspaceID string, resource workspace.Resource, action workspace.Action) error
}

type DealFunnels interface {
	PipelineStages(workspaceID, pipelineID string) ([]*stage.Stage, error)
}

type UseCase struct {
	repo    dealautomation.Repository
	access  AccessChecker
	funnels DealFunnels
	now     func() time.Time
}

func New(repo dealautomation.Repository, access AccessChecker, funnels DealFunnels) *UseCase {
	return &UseCase{repo: repo, access: access, funnels: funnels, now: time.Now}
}

func (uc *UseCase) Get(by shared.Person, workspaceID string, channel dealautomation.Channel, containerID string) (*dealautomation.Setting, error) {
	if err := uc.authorize(by, workspaceID, channel, workspace.ActionRead); err != nil {
		return nil, err
	}
	found, err := uc.repo.Find(workspaceID, channel, containerID)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return &dealautomation.Setting{WorkspaceID: workspaceID, Channel: channel, ContainerID: containerID}, nil
	}
	return found, nil
}

func (uc *UseCase) Set(by shared.Person, workspaceID string, channel dealautomation.Channel, containerID, pipelineID string) (*dealautomation.Setting, error) {
	if err := uc.authorize(by, workspaceID, channel, workspace.ActionUpdate); err != nil {
		return nil, err
	}
	containerID = strings.TrimSpace(containerID)
	pipelineID = strings.TrimSpace(pipelineID)
	if pipelineID == "" {
		if err := uc.repo.Delete(workspaceID, channel, containerID); err != nil {
			return nil, err
		}
		return &dealautomation.Setting{WorkspaceID: workspaceID, Channel: channel, ContainerID: containerID}, nil
	}
	if _, err := uc.funnels.PipelineStages(workspaceID, pipelineID); err != nil {
		return nil, err
	}
	setting := dealautomation.Setting{
		WorkspaceID: workspaceID,
		Channel:     channel,
		ContainerID: containerID,
		PipelineID:  pipelineID,
		UpdatedBy:   by.UserID,
		UpdatedAt:   uc.now().UTC(),
	}
	if err := uc.repo.Save(setting); err != nil {
		return nil, err
	}
	return &setting, nil
}

func (uc *UseCase) PipelineFor(workspaceID string, channel dealautomation.Channel, containerID string) (string, error) {
	if _, supported := channel.Resource(); !supported {
		return "", nil
	}
	found, err := uc.repo.Find(workspaceID, channel, containerID)
	if err != nil || found == nil {
		return "", err
	}
	return found.PipelineID, nil
}

func (uc *UseCase) authorize(by shared.Person, workspaceID string, channel dealautomation.Channel, action workspace.Action) error {
	resource, supported := channel.Resource()
	if !supported {
		return dealautomation.ErrUnsupportedChannel
	}
	if by.SystemAdmin {
		return nil
	}
	if strings.TrimSpace(by.UserID) == "" {
		return workspace.ErrUnauthorized
	}
	return uc.access.Execute(by.UserID, workspaceID, resource, action)
}

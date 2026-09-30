package callrouting_usecase

import (
	"context"
	"errors"

	"vozko/domain/callrouting"
	dept_domain "vozko/domain/workspace/workspace_department"
)

type StaticMembers struct{}

func (StaticMembers) UserIDs(_ context.Context, queue callrouting.Queue) ([]string, error) {
	return queue.MemberUserIDs, nil
}

type departmentLister interface {
	ListMembers(departmentID string) ([]dept_domain.DepartmentMember, error)
}

type QueueMembers struct {
	departments departmentLister
}

var _ callrouting.QueueMembers = (*QueueMembers)(nil)

func NewQueueMembers(departments departmentLister) *QueueMembers {
	return &QueueMembers{departments: departments}
}

func (m *QueueMembers) UserIDs(ctx context.Context, queue callrouting.Queue) ([]string, error) {
	if queue.DepartmentID == "" {
		return StaticMembers{}.UserIDs(ctx, queue)
	}
	if m.departments == nil {
		return nil, errors.New("department members are not available")
	}
	members, err := m.departments.ListMembers(queue.DepartmentID)
	if err != nil {
		return nil, err
	}
	userIDs := make([]string, 0, len(members))
	for _, member := range members {
		if member.UserID != "" {
			userIDs = append(userIDs, member.UserID)
		}
	}
	return userIDs, nil
}

package callrouting_usecase

import (
	"context"

	"vozko/domain/callrouting"
	"vozko/domain/workspace"
	dept_domain "vozko/domain/workspace/workspace_department"
)

type departmentFinder interface {
	GetDepartmentByID(id string) (*dept_domain.Department, error)
}

type workspaceMemberFinder interface {
	GetMember(workspaceID, userID string) (*workspace.Member, error)
}

type QueueCatalogDeps struct {
	Queues      callrouting.QueueRepository
	Departments departmentFinder
	Members     workspaceMemberFinder
	Music       callrouting.HoldMusicLibrary
}

type QueueCatalog struct {
	deps QueueCatalogDeps
}

func NewQueueCatalog(deps QueueCatalogDeps) *QueueCatalog {
	return &QueueCatalog{deps: deps}
}

func (c *QueueCatalog) List(ctx context.Context, workspaceID string) ([]*callrouting.Queue, error) {
	return c.deps.Queues.ListByWorkspace(ctx, workspaceID)
}

func (c *QueueCatalog) Get(ctx context.Context, workspaceID, id string) (*callrouting.Queue, error) {
	return c.deps.Queues.FindInWorkspace(ctx, workspaceID, id)
}

func (c *QueueCatalog) Create(ctx context.Context, queue callrouting.Queue) (*callrouting.Queue, error) {
	queue.ID = ""
	if err := c.admit(ctx, &queue); err != nil {
		return nil, err
	}
	if err := c.deps.Queues.Create(ctx, &queue); err != nil {
		return nil, err
	}
	return &queue, nil
}

func (c *QueueCatalog) Update(ctx context.Context, queue callrouting.Queue) (*callrouting.Queue, error) {
	existing, err := c.deps.Queues.FindInWorkspace(ctx, queue.WorkspaceID, queue.ID)
	if err != nil {
		return nil, err
	}
	queue.CreatedAt = existing.CreatedAt
	if err := c.admit(ctx, &queue); err != nil {
		return nil, err
	}
	if err := c.deps.Queues.Update(ctx, &queue); err != nil {
		return nil, err
	}
	return &queue, nil
}

func (c *QueueCatalog) Delete(ctx context.Context, workspaceID, id string) error {
	return c.deps.Queues.Delete(ctx, workspaceID, id)
}

func (c *QueueCatalog) admit(ctx context.Context, queue *callrouting.Queue) error {
	queue.ApplyDefaults()
	if err := queue.Validate(); err != nil {
		return err
	}
	if err := c.ensureDepartment(queue); err != nil {
		return err
	}
	if err := c.ensureMembers(queue); err != nil {
		return err
	}
	_, err := c.deps.Music.PCM(ctx, queue.WorkspaceID, queue.HoldMusic)
	return err
}

func (c *QueueCatalog) ensureDepartment(queue *callrouting.Queue) error {
	if queue.DepartmentID == "" {
		return nil
	}
	department, err := c.deps.Departments.GetDepartmentByID(queue.DepartmentID)
	if err != nil || department == nil || department.WorkspaceID != queue.WorkspaceID {
		return callrouting.ErrQueueDepartment
	}
	return nil
}

func (c *QueueCatalog) ensureMembers(queue *callrouting.Queue) error {
	for _, userID := range queue.MemberUserIDs {
		member, err := c.deps.Members.GetMember(queue.WorkspaceID, userID)
		if err != nil {
			return err
		}
		if member == nil {
			return callrouting.ErrQueueMemberOutside
		}
	}
	return nil
}

package gormmcp

import (
	"context"
	"errors"

	"gorm.io/gorm"

	domainmcp "vozko/domain/agent/mcp"
	"vozko/infra/database/schema"
)

type CollectionRepo struct{ db *gorm.DB }

func NewCollectionRepo(db *gorm.DB) *CollectionRepo { return &CollectionRepo{db: db} }

func (r *CollectionRepo) Create(ctx context.Context, c *domainmcp.MCPCollection) error {
	if c == nil || c.WorkspaceID == "" {
		return domainmcp.ErrWorkspaceRequired
	}
	row := schema.MCPCollection{
		ID:          c.ID,
		WorkspaceID: c.WorkspaceID,
		Name:        c.Name,
		Description: c.Description,
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		c.ID = row.ID
		if len(c.Members) == 0 {
			return nil
		}
		members := make([]schema.MCPCollectionMember, 0, len(c.Members))
		for _, m := range c.Members {
			members = append(members, schema.MCPCollectionMember{
				CollectionID: row.ID,
				Kind:         string(m.Kind),
				RefID:        m.RefID,
			})
		}
		return tx.Create(&members).Error
	})
}

func (r *CollectionRepo) Update(ctx context.Context, c *domainmcp.MCPCollection) error {
	if c == nil || c.WorkspaceID == "" {
		return domainmcp.ErrWorkspaceRequired
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&schema.MCPCollection{}).
			Where("id = ? AND workspace_id = ?", c.ID, c.WorkspaceID).
			Updates(map[string]any{
				"name":        c.Name,
				"description": c.Description,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domainmcp.ErrCollectionNotFound
		}
		if err := tx.Where("collection_id = ?", c.ID).
			Delete(&schema.MCPCollectionMember{}).Error; err != nil {
			return err
		}
		if len(c.Members) == 0 {
			return nil
		}
		members := make([]schema.MCPCollectionMember, 0, len(c.Members))
		for _, m := range c.Members {
			members = append(members, schema.MCPCollectionMember{
				CollectionID: c.ID,
				Kind:         string(m.Kind),
				RefID:        m.RefID,
			})
		}
		return tx.Create(&members).Error
	})
}

func (r *CollectionRepo) Get(ctx context.Context, ws, id string) (*domainmcp.MCPCollection, error) {
	var row schema.MCPCollection
	err := r.db.WithContext(ctx).
		Where("id = ? AND workspace_id = ?", id, ws).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainmcp.ErrCollectionNotFound
	}
	if err != nil {
		return nil, err
	}
	members, err := r.loadMembers(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	out := rowToCollection(&row)
	out.Members = members[id]
	return out, nil
}

func (r *CollectionRepo) ListByWorkspace(ctx context.Context, ws string) ([]*domainmcp.MCPCollection, error) {
	var rows []schema.MCPCollection
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ?", ws).
		Order("name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []*domainmcp.MCPCollection{}, nil
	}
	ids := make([]string, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	members, err := r.loadMembers(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]*domainmcp.MCPCollection, len(rows))
	for i := range rows {
		c := rowToCollection(&rows[i])
		c.Members = members[c.ID]
		out[i] = c
	}
	return out, nil
}

func (r *CollectionRepo) ListByIDs(ctx context.Context, ws string, ids []string) ([]*domainmcp.MCPCollection, error) {
	if len(ids) == 0 {
		return []*domainmcp.MCPCollection{}, nil
	}
	var rows []schema.MCPCollection
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND id IN ?", ws, ids).
		Order("name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []*domainmcp.MCPCollection{}, nil
	}
	fetched := make([]string, len(rows))
	for i := range rows {
		fetched[i] = rows[i].ID
	}
	members, err := r.loadMembers(ctx, fetched)
	if err != nil {
		return nil, err
	}
	out := make([]*domainmcp.MCPCollection, len(rows))
	for i := range rows {
		c := rowToCollection(&rows[i])
		c.Members = members[c.ID]
		out[i] = c
	}
	return out, nil
}

func (r *CollectionRepo) Delete(ctx context.Context, ws, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ? AND workspace_id = ?", id, ws).
			Delete(&schema.MCPCollection{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domainmcp.ErrCollectionNotFound
		}
		if err := tx.Where("collection_id = ?", id).
			Delete(&schema.MCPCollectionMember{}).Error; err != nil {
			return err
		}
		return tx.Where("collection_id = ?", id).
			Delete(&schema.AgentMCPCollection{}).Error
	})
}

func (r *CollectionRepo) loadMembers(ctx context.Context, ids []string) (map[string][]domainmcp.CollectionMember, error) {
	out := make(map[string][]domainmcp.CollectionMember, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []schema.MCPCollectionMember
	if err := r.db.WithContext(ctx).
		Where("collection_id IN ?", ids).
		Order("kind ASC, ref_id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, m := range rows {
		out[m.CollectionID] = append(out[m.CollectionID], domainmcp.CollectionMember{
			Kind:  domainmcp.CollectionMemberKind(m.Kind),
			RefID: m.RefID,
		})
	}
	return out, nil
}

func rowToCollection(r *schema.MCPCollection) *domainmcp.MCPCollection {
	return &domainmcp.MCPCollection{
		ID:          r.ID,
		WorkspaceID: r.WorkspaceID,
		Name:        r.Name,
		Description: r.Description,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
		Members:     []domainmcp.CollectionMember{},
	}
}

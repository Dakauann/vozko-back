package unofficial_whatsapp_group_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	uwg "vozko/domain/unofficial_whatsapp_group"
	"vozko/infra/database/schema"
)

type joinRepository struct{ db *gorm.DB }

func NewJoinRepository(db *gorm.DB) uwg.JoinRepository { return &joinRepository{db: db} }

func (r *joinRepository) CreateMany(ctx context.Context, tasks []uwg.JoinTask) error {
	if len(tasks) == 0 {
		return nil
	}
	rows := make([]schema.UnofficialWhatsAppGroupJoin, 0, len(tasks))
	for _, t := range tasks {
		rows = append(rows, joinToRow(t))
	}
	if err := r.db.WithContext(ctx).Create(&rows).Error; err != nil {
		return err
	}
	for i := range rows {
		tasks[i].ID = rows[i].ID
	}
	return nil
}

func (r *joinRepository) ClaimNext(ctx context.Context, now time.Time, owner string, leaseUntil time.Time) (*uwg.JoinTask, error) {
	var rows []schema.UnofficialWhatsAppGroupJoin
	err := r.db.WithContext(ctx).Raw(`
		UPDATE unofficial_whatsapp_group_joins
		   SET status = ?, lease_owner = ?, lease_until = ?, updated_at = ?
		 WHERE id IN (
		       SELECT id FROM unofficial_whatsapp_group_joins
		        WHERE ((status = ? AND due_at <= ?) OR (status = ? AND lease_until < ?))
		        ORDER BY due_at ASC
		        LIMIT 1
		        FOR UPDATE SKIP LOCKED)
		RETURNING *`,
		string(uwg.JoinRunning), owner, leaseUntil, now,
		string(uwg.JoinPending), now, string(uwg.JoinRunning), now).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	task := joinToDomain(&rows[0])
	return &task, nil
}

var ErrJoinLeaseLost = errors.New("the join task lease was taken by another worker")

func (r *joinRepository) Save(ctx context.Context, t *uwg.JoinTask) error {
	res := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppGroupJoin{}).
		Where("id = ? AND lease_owner = ?", t.ID, t.LeaseOwner).
		Updates(map[string]any{
			"status":      string(t.Status),
			"attempts":    t.Attempts,
			"error":       truncate(t.Error, 500),
			"due_at":      t.DueAt,
			"group_jid":   t.GroupJID,
			"lease_owner": "",
			"lease_until": nil,
			"updated_at":  time.Now().UTC(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrJoinLeaseLost
	}
	t.LeaseOwner, t.LeaseUntil = "", nil
	return nil
}

func (r *joinRepository) FailRemaining(ctx context.Context, planID, reason string) error {
	return r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppGroupJoin{}).
		Where("plan_id = ? AND status = ?", planID, string(uwg.JoinPending)).
		Updates(map[string]any{"status": string(uwg.JoinFailed), "error": truncate(reason, 500), "updated_at": time.Now().UTC()}).Error
}

func (r *joinRepository) CancelPlan(ctx context.Context, workspaceID, planID string, scope uwg.Scope) (int64, error) {
	q := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppGroupJoin{}).
		Where("workspace_id = ? AND plan_id = ? AND status = ?", workspaceID, planID, string(uwg.JoinPending))
	if scope.Restrict {
		if len(scope.InstanceIDs) == 0 {
			return 0, nil
		}
		q = q.Where("instance_id IN ?", scope.InstanceIDs)
	}
	res := q.Updates(map[string]any{"status": string(uwg.JoinCancelled), "updated_at": time.Now().UTC()})
	return res.RowsAffected, res.Error
}

func (r *joinRepository) Recent(ctx context.Context, workspaceID string, scope uwg.Scope, since time.Time) ([]uwg.JoinTask, error) {
	q := r.db.WithContext(ctx).Where("workspace_id = ? AND created_at >= ?", workspaceID, since)
	if scope.Restrict {
		if len(scope.InstanceIDs) == 0 {
			return []uwg.JoinTask{}, nil
		}
		q = q.Where("instance_id IN ?", scope.InstanceIDs)
	}
	var rows []schema.UnofficialWhatsAppGroupJoin
	if err := q.Order("created_at DESC, sequence ASC").Limit(200).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]uwg.JoinTask, 0, len(rows))
	for i := range rows {
		out = append(out, joinToDomain(&rows[i]))
	}
	return out, nil
}

func (r *joinRepository) LastLink(ctx context.Context, workspaceID, groupJID string) (string, error) {
	var links []string
	err := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppGroupJoin{}).
		Where("workspace_id = ? AND group_jid = ? AND status = ?", workspaceID, groupJID, string(uwg.JoinDone)).
		Order("created_at DESC").Limit(1).Pluck("invite_link", &links).Error
	if err != nil || len(links) == 0 {
		return "", err
	}
	return links[0], nil
}

func joinToRow(t uwg.JoinTask) schema.UnofficialWhatsAppGroupJoin {
	return schema.UnofficialWhatsAppGroupJoin{
		ID: t.ID, PlanID: t.PlanID, WorkspaceID: t.WorkspaceID, InstanceID: t.InstanceID,
		InviteLink: t.InviteLink, GroupJID: t.GroupJID, Sequence: t.Sequence, Follow: t.Follow, Category: t.Category,
		Status: string(t.Status), Attempts: t.Attempts, Error: truncate(t.Error, 500), DueAt: t.DueAt,
		LeaseOwner: t.LeaseOwner, LeaseUntil: t.LeaseUntil, CreatedByID: optional(t.CreatedByID),
	}
}

func joinToDomain(r *schema.UnofficialWhatsAppGroupJoin) uwg.JoinTask {
	return uwg.JoinTask{
		ID: r.ID, PlanID: r.PlanID, WorkspaceID: r.WorkspaceID, InstanceID: r.InstanceID,
		InviteLink: r.InviteLink, GroupJID: r.GroupJID, Sequence: r.Sequence, Follow: r.Follow, Category: r.Category,
		Status: uwg.JoinStatus(r.Status), Attempts: r.Attempts, Error: r.Error, DueAt: r.DueAt,
		CreatedByID: value(r.CreatedByID), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		LeaseOwner: r.LeaseOwner, LeaseUntil: r.LeaseUntil,
	}
}

package lead

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"vozko/domain/lead"
	"vozko/infra/database/schema"
)

const emailBulkLockSQL = "SELECT pg_advisory_xact_lock(hashtext(?))"

func normalizedEmail(raw string) (string, bool) {
	address := strings.ToLower(strings.TrimSpace(raw))
	if address == "" || lead.ValidateEmail(address) != nil {
		return "", false
	}
	return address, true
}

func (r *repository) IDsByEmails(ctx context.Context, workspaceID string, addresses []string) (map[string]string, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	found, err := r.leadsByEmail(r.db.WithContext(ctx), workspaceID, uniqueEmails(addresses))
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(found))
	for address, l := range found {
		out[address] = l.ID
	}
	return out, nil
}

func uniqueEmails(addresses []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(addresses))
	for _, raw := range addresses {
		if address, ok := normalizedEmail(raw); ok && !seen[address] {
			seen[address] = true
			out = append(out, address)
		}
	}
	return out
}

func (r *repository) leadsByEmail(db *gorm.DB, workspaceID string, addresses []string) (map[string]*lead.Lead, error) {
	out := make(map[string]*lead.Lead, len(addresses))
	for start := 0; start < len(addresses); start += lookupBatchSize {
		chunk := addresses[start:min(start+lookupBatchSize, len(addresses))]
		var rows []schema.Lead
		if err := db.Where("workspace_id = ? AND lower(email) IN ?", workspaceID, chunk).Order("created_at ASC, id ASC").Find(&rows).Error; err != nil {
			return nil, err
		}
		leads, err := toDomainAll(rows)
		if err != nil {
			return nil, err
		}
		for _, l := range leads {
			address := strings.ToLower(strings.TrimSpace(l.Email))
			if _, taken := out[address]; !taken {
				out[address] = l
			}
		}
	}
	return out, nil
}

func (r *repository) FindOrCreateManyByEmail(ctx context.Context, workspaceID string, inputs []lead.BulkEmailInput) (map[string]*lead.Lead, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	byEmail := make(map[string]lead.BulkEmailInput, len(inputs))
	order := make([]string, 0, len(inputs))
	for _, in := range inputs {
		if !in.Source.Valid() {
			return nil, lead.ErrLeadSourceInvalid
		}
		address, ok := normalizedEmail(in.Email)
		if !ok {
			continue
		}
		if _, dup := byEmail[address]; !dup {
			byEmail[address] = in
			order = append(order, address)
		}
	}
	out := make(map[string]*lead.Lead, len(order))
	if len(order) == 0 {
		return out, nil
	}
	var merges []incomingMerge
	created := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(emailBulkLockSQL, "lead-email:"+workspaceID).Error; err != nil {
			return err
		}
		existing, err := r.leadsByEmail(tx, workspaceID, order)
		if err != nil {
			return err
		}
		var rows []schema.Lead
		for _, address := range order {
			in := byEmail[address]
			update := lead.LeadUpdate{Source: in.Source, Name: in.Name}
			if l, ok := existing[address]; ok {
				out[address] = l
				merges = append(merges, incomingMerge{lead: l, apply: mergeOf(update)})
				continue
			}
			l := &lead.Lead{ID: r.leadID(), WorkspaceID: workspaceID, Email: address, Source: in.Source, Version: 1}
			l.MergeIncoming(update)
			row, err := toSchema(l)
			if err != nil {
				return err
			}
			rows = append(rows, *row)
			out[address] = l
		}
		if len(rows) == 0 {
			return nil
		}
		if err := tx.CreateInBatches(&rows, lookupBatchSize).Error; err != nil {
			return err
		}
		stored, err := toDomainAll(rows)
		if err != nil {
			return err
		}
		for _, l := range stored {
			out[strings.ToLower(l.Email)] = l
		}
		created = len(rows)
		return nil
	})
	if err != nil {
		return nil, err
	}
	wrote, err := r.mergeIncoming(workspaceID, merges)
	if err != nil {
		return nil, err
	}
	if wrote || created > 0 {
		r.agg.bump(workspaceID)
	}
	return out, nil
}

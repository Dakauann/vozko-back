package audience_repository

import (
	"encoding/json"

	"gorm.io/datatypes"

	ca "vozko/domain/audience"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

func toDomain(row *schema.AudienceAnalysis) *ca.Analysis {
	if row == nil {
		return nil
	}
	a := &ca.Analysis{
		ID:                 row.ID,
		WorkspaceID:        row.WorkspaceID,
		SubjectKind:        ca.SubjectKind(row.SubjectKind),
		Revision:           row.Revision,
		Transcript:         row.Transcript,
		Source:             ca.Source(row.Source),
		AccountID:          row.AccountID,
		ContainerID:        row.ContainerID,
		SubjectID:          row.SubjectID,
		ParentSubjectID:    row.ParentSubjectID,
		AuthorExternalID:   row.AuthorExternalID,
		AuthorHandle:       row.AuthorHandle,
		Status:             ca.Status(row.Status),
		Attempts:           row.Attempts,
		FailureReason:      row.FailureReason,
		Sentiment:          shared.Sentiment(row.Sentiment),
		Stance:             ca.Stance(row.Stance),
		Intent:             ca.Intent(row.Intent),
		TopicKey:           row.TopicKey,
		IsSpam:             row.IsSpam,
		Language:           row.Language,
		Toxicity:           shared.QualityLevel(row.Toxicity),
		PersonalAttack:     shared.QualityLevel(row.PersonalAttack),
		LegalRisk:          shared.QualityLevel(row.LegalRisk),
		Severity:           row.Severity,
		Interest:           ca.Interest(row.Interest),
		ProductInterest:    row.ProductInterest,
		ProductInterestKey: row.ProductInterestKey,
		Disposition:        ca.Disposition(row.Disposition),
		Qualification:      ca.Qualification(row.Qualification),
		NextAction:         ca.NextAction(row.NextAction),
		Summary:            row.Summary,
		AttendanceQuality:  row.AttendanceQuality,
		MessageCount:       row.MessageCount,
		RequiresAction:     row.RequiresAction,
		Excerpt:            row.Excerpt,
		Truncated:          row.Truncated,
		Model:              row.Model,
		AnalyzedAt:         row.AnalyzedAt,
		OccurredAt:         row.OccurredAt,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
		DeletedAt:          row.DeletedAt,
	}
	if row.BatchID != nil {
		a.BatchID = *row.BatchID
	}
	return a
}

func toDomainSlice(rows []schema.AudienceAnalysis) []*ca.Analysis {
	out := make([]*ca.Analysis, 0, len(rows))
	for i := range rows {
		out = append(out, toDomain(&rows[i]))
	}
	return out
}

func fromDomain(a *ca.Analysis) *schema.AudienceAnalysis {
	row := &schema.AudienceAnalysis{
		ID:                 a.ID,
		WorkspaceID:        a.WorkspaceID,
		SubjectKind:        string(a.Kind()),
		Revision:           a.Revision,
		Transcript:         a.Transcript,
		Source:             string(a.Source),
		AccountID:          a.AccountID,
		ContainerID:        a.ContainerID,
		SubjectID:          a.SubjectID,
		ParentSubjectID:    a.ParentSubjectID,
		AuthorExternalID:   a.AuthorExternalID,
		AuthorHandle:       a.AuthorHandle,
		Status:             string(a.Status),
		Attempts:           a.Attempts,
		FailureReason:      a.FailureReason,
		Sentiment:          string(a.Sentiment),
		Stance:             string(a.Stance),
		Intent:             string(a.Intent),
		TopicKey:           a.TopicKey,
		IsSpam:             a.IsSpam,
		Language:           a.Language,
		Toxicity:           string(a.Toxicity),
		PersonalAttack:     string(a.PersonalAttack),
		LegalRisk:          string(a.LegalRisk),
		Severity:           a.Severity,
		Interest:           string(a.Interest),
		ProductInterest:    a.ProductInterest,
		ProductInterestKey: a.ProductInterestKey,
		Disposition:        string(a.Disposition),
		Qualification:      string(a.Qualification),
		NextAction:         string(a.NextAction),
		Summary:            a.Summary,
		AttendanceQuality:  a.AttendanceQuality,
		MessageCount:       a.MessageCount,
		RequiresAction:     a.RequiresAction,
		Excerpt:            a.Excerpt,
		Truncated:          a.Truncated,
		Model:              a.Model,
		AnalyzedAt:         a.AnalyzedAt,
		OccurredAt:         a.OccurredAt,
		CreatedAt:          a.CreatedAt,
		UpdatedAt:          a.UpdatedAt,
		DeletedAt:          a.DeletedAt,
	}
	if a.BatchID != "" {
		id := a.BatchID
		row.BatchID = &id
	}
	return row
}

// ---- settings ----

func settingsToDomain(row *schema.AudienceSettings) (*ca.Settings, error) {
	s := &ca.Settings{
		WorkspaceID:  row.WorkspaceID,
		Source:       ca.Source(row.Source),
		AccountID:    row.AccountID,
		Enabled:      row.Enabled,
		Model:        row.Model,
		Vertical:     ca.Vertical(row.Vertical),
		ActionPolicy: ca.ActionPolicy{SeverityThreshold: row.SeverityThreshold},
		ReplyPolicy: ca.ReplyPolicy{
			Mode:            ca.ReplyMode(row.ReplyMode),
			MaxAutoSeverity: row.ReplyMaxAutoSeverity,
		},
		DailyCap:     row.DailyCap,
		Instructions: row.Instructions,
		UpdatedAt:    row.UpdatedAt,
	}
	if len(row.Topics) > 0 {
		if err := json.Unmarshal(row.Topics, &s.Topics); err != nil {
			return nil, err
		}
	}
	s.Normalize()
	return s, nil
}

func settingsFromDomain(s *ca.Settings) (*schema.AudienceSettings, error) {
	topics, err := json.Marshal(s.Topics)
	if err != nil {
		return nil, err
	}
	return &schema.AudienceSettings{
		Source:               string(s.Source),
		AccountID:            s.AccountID,
		WorkspaceID:          s.WorkspaceID,
		Enabled:              s.Enabled,
		Model:                s.Model,
		Vertical:             string(s.Vertical),
		Topics:               datatypes.JSON(topics),
		SeverityThreshold:    s.ActionPolicy.SeverityThreshold,
		ReplyMode:            string(s.ReplyPolicy.Mode),
		ReplyMaxAutoSeverity: s.ReplyPolicy.MaxAutoSeverity,
		DailyCap:             s.DailyCap,
		Instructions:         s.Instructions,
		UpdatedAt:            s.UpdatedAt,
	}, nil
}

// ---- authors ----

func authorToDomain(row *schema.AudienceAuthor) (*ca.AuthorStats, error) {
	a := &ca.AuthorStats{
		ID:               row.ID,
		WorkspaceID:      row.WorkspaceID,
		Source:           ca.Source(row.Source),
		AccountID:        row.AccountID,
		AuthorExternalID: row.AuthorExternalID,
		AuthorHandle:     row.AuthorHandle,
		FirstSeenAt:      row.FirstSeenAt,
		LastSeenAt:       row.LastSeenAt,
		DerivedStance:    ca.Stance(row.DerivedStance),
		IsFlagged:        row.IsFlagged,
		ModerationState:  ca.ModerationState(row.ModerationState),
		Role: ca.AuthorRoleInference{
			Role:            ca.AuthorRole(row.Role),
			Confidence:      shared.QualityLevel(row.RoleConfidence),
			BasedOnComments: row.RoleComments,
			Rationale:       row.RoleRationale,
		},
		UpdatedAt: row.UpdatedAt,
	}
	if len(row.Counters) > 0 {
		if err := json.Unmarshal(row.Counters, &a.Counters); err != nil {
			return nil, err
		}
	}
	if len(row.TopTopics) > 0 {
		if err := json.Unmarshal(row.TopTopics, &a.TopTopics); err != nil {
			return nil, err
		}
	}
	if a.TopTopics == nil {
		a.TopTopics = []ca.TopicCount{}
	}
	// Re-derive rather than trust the columns.
	//
	// The standing, the flag and the reputation are pure functions of the
	// counters, so this returns the same values the writer stored — except on a
	// row written before one of them existed, where the column is a default and
	// the counters are still right. Deriving on read means such a row is
	// correct immediately instead of waiting for its next rollup. The columns
	// remain what the query FILTERS and SORTS on; this is what it returns.
	a.Derive()
	return a, nil
}

func authorFromDomain(a *ca.AuthorStats) (*schema.AudienceAuthor, error) {
	counters, err := json.Marshal(a.Counters)
	if err != nil {
		return nil, err
	}
	topics := a.TopTopics
	if topics == nil {
		topics = []ca.TopicCount{}
	}
	top, err := json.Marshal(topics)
	if err != nil {
		return nil, err
	}
	return &schema.AudienceAuthor{
		ID:               a.ID,
		WorkspaceID:      a.WorkspaceID,
		Source:           string(a.Source),
		AccountID:        a.AccountID,
		AuthorExternalID: a.AuthorExternalID,
		AuthorHandle:     a.AuthorHandle,
		FirstSeenAt:      a.FirstSeenAt,
		LastSeenAt:       a.LastSeenAt,
		Counters:         datatypes.JSON(counters),
		// The ranking columns are denormalised out of the JSON so the flagged
		// table sorts on an index instead of parsing jsonb per row.
		TotalComments:   a.Total,
		MaxSeverity:     a.SeverityMax,
		HighSevCount:    a.SeverityHighCount,
		StanceHostile:   a.StanceHostile,
		StanceSupporter: a.StanceSupporter,
		Reputation:      a.Reputation,
		TopTopics:       datatypes.JSON(top),
		DerivedStance:   string(a.DerivedStance),
		IsFlagged:       a.IsFlagged,
		ModerationState: string(a.ModerationState),
		Role:            string(a.Role.Role),
		RoleConfidence:  string(a.Role.Confidence),
		RoleComments:    a.Role.BasedOnComments,
		RoleRationale:   a.Role.Rationale,
		UpdatedAt:       a.UpdatedAt,
	}, nil
}

// ---- rollups ----

func rollupToDomain(row *schema.AudienceRollup) (*ca.Rollup, error) {
	r := &ca.Rollup{
		WorkspaceID:     row.WorkspaceID,
		Source:          ca.Source(row.Source),
		AccountID:       row.AccountID,
		Scope:           ca.RollupScope(row.Scope),
		ScopeID:         row.ScopeID,
		BucketDate:      ca.BucketDate(row.BucketDate),
		AcceptanceScore: row.AcceptanceScore,
		ComputedAt:      row.ComputedAt,
	}
	if len(row.Counters) > 0 {
		if err := json.Unmarshal(row.Counters, &r.Counters); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func rollupFromDomain(r *ca.Rollup) (*schema.AudienceRollup, error) {
	counters, err := json.Marshal(r.Counters)
	if err != nil {
		return nil, err
	}
	return &schema.AudienceRollup{
		WorkspaceID:     r.WorkspaceID,
		Source:          string(r.Source),
		AccountID:       r.AccountID,
		Scope:           string(r.Scope),
		ScopeID:         r.ScopeID,
		BucketDate:      ca.BucketDate(r.BucketDate),
		Counters:        datatypes.JSON(counters),
		AcceptanceScore: r.AcceptanceScore,
		ComputedAt:      r.ComputedAt,
	}, nil
}

// ---- batches ----

func batchFromDomain(b *ca.Batch) *schema.AudienceBatch {
	return &schema.AudienceBatch{
		ID:               b.ID,
		WorkspaceID:      b.WorkspaceID,
		Source:           string(b.Source),
		AccountID:        b.AccountID,
		ContainerID:      b.ContainerID,
		Kind:             string(ca.NormalizeBatchKind(b.Kind)),
		Model:            b.Model,
		ItemCount:        b.ItemCount,
		PromptTokens:     b.PromptTokens,
		CompletionTokens: b.CompletionTokens,
		PriceMicros:      b.PriceMicros,
		Outcome:          string(b.Outcome),
		RequestID:        b.RequestID,
		CreatedAt:        b.CreatedAt,
	}
}

// ---- backfills ----

func backfillToDomain(row *schema.AudienceBackfill) *ca.Backfill {
	return &ca.Backfill{
		ID:                row.ID,
		WorkspaceID:       row.WorkspaceID,
		Source:            ca.Source(row.Source),
		AccountID:         row.AccountID,
		ContainerID:       row.ContainerID,
		Status:            ca.BackfillStatus(row.Status),
		Cursor:            row.Cursor,
		EstimatedComments: row.EstimatedComments,
		Fetched:           row.Fetched,
		Enqueued:          row.Enqueued,
		Error:             row.Error,
		RequestedByUserID: row.RequestedByUserID,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
		FinishedAt:        row.FinishedAt,
	}
}

func backfillFromDomain(b *ca.Backfill) *schema.AudienceBackfill {
	return &schema.AudienceBackfill{
		ID:                b.ID,
		WorkspaceID:       b.WorkspaceID,
		Source:            string(b.Source),
		AccountID:         b.AccountID,
		ContainerID:       b.ContainerID,
		Status:            string(b.Status),
		Cursor:            b.Cursor,
		EstimatedComments: b.EstimatedComments,
		Fetched:           b.Fetched,
		Enqueued:          b.Enqueued,
		Error:             b.Error,
		RequestedByUserID: b.RequestedByUserID,
		CreatedAt:         b.CreatedAt,
		UpdatedAt:         b.UpdatedAt,
		FinishedAt:        b.FinishedAt,
	}
}

// ---- container overrides ----

func overrideToDomain(row *schema.AudienceContainerSettings) (*ca.ContainerOverride, error) {
	o := &ca.ContainerOverride{
		WorkspaceID:       row.WorkspaceID,
		Source:            ca.Source(row.Source),
		AccountID:         row.AccountID,
		ContainerID:       row.ContainerID,
		Enabled:           row.Enabled,
		Model:             row.Model,
		SeverityThreshold: row.SeverityThreshold,
		Instructions:      row.Instructions,
		UpdatedAt:         row.UpdatedAt,
	}
	if len(row.Topics) > 0 {
		var topics ca.TopicSet
		if err := json.Unmarshal(row.Topics, &topics); err != nil {
			return nil, err
		}
		o.Topics = &topics
	}
	return o, nil
}

func overrideFromDomain(o *ca.ContainerOverride) (*schema.AudienceContainerSettings, error) {
	row := &schema.AudienceContainerSettings{
		Source:            string(o.Source),
		AccountID:         o.AccountID,
		ContainerID:       o.ContainerID,
		WorkspaceID:       o.WorkspaceID,
		Enabled:           o.Enabled,
		Model:             o.Model,
		SeverityThreshold: o.SeverityThreshold,
		Instructions:      o.Instructions,
		UpdatedAt:         o.UpdatedAt,
	}
	if o.Topics != nil {
		topics, err := json.Marshal(*o.Topics)
		if err != nil {
			return nil, err
		}
		row.Topics = datatypes.JSON(topics)
	}
	return row, nil
}

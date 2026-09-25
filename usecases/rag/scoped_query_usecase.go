package rag_usecase

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/rag"
)

type scopedQueryUseCase struct {
	access rag.KnowledgeBaseAccessUseCase
	query  rag.QueryKnowledgeBaseUseCase
}

func NewScopedQueryUseCase(access rag.KnowledgeBaseAccessUseCase, query rag.QueryKnowledgeBaseUseCase) rag.ScopedQueryUseCase {
	return &scopedQueryUseCase{access: access, query: query}
}

func (uc *scopedQueryUseCase) Execute(ctx context.Context, viewer rag.Viewer, input rag.QueryInput) (*rag.QueryOutput, error) {
	input.Query = strings.TrimSpace(input.Query)
	if input.Query == "" {
		return nil, rag.ErrQueryRequired
	}
	ids := distinct(input.KnowledgeBaseIDs)
	if len(ids) == 0 {
		return nil, rag.ErrKnowledgeBaseIDRequired
	}
	if len(ids) > rag.MaxQueriedKnowledgeBases {
		return nil, rag.ErrQueryTooManyBases
	}
	for _, id := range ids {
		if _, err := uc.access.Owned(ctx, viewer, id); err != nil {
			return nil, fmt.Errorf("knowledge base %s: %w", id, err)
		}
	}
	input.KnowledgeBaseIDs = ids
	input.TopK = rag.QueryResultLimit(input.TopK)
	if input.MinScore <= 0 {
		input.MinScore = rag.DefaultQueryMinScore
	}
	return uc.query.Execute(ctx, input)
}

func distinct(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

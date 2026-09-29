package conversation_usecase

import (
	"context"
	"fmt"

	"vozko/domain/livedecision"
	"vozko/domain/shared"
	livedecisions_usecase "vozko/usecases/livedecisions"
)

type LiveSubjects struct {
	resolvers map[shared.EntryType]AnalysisSubjectResolver
}

func NewLiveSubjects() *LiveSubjects {
	return &LiveSubjects{resolvers: map[shared.EntryType]AnalysisSubjectResolver{}}
}

func (s *LiveSubjects) SetAnalysisSubjectResolver(entryType shared.EntryType, resolver AnalysisSubjectResolver) {
	s.resolvers[entryType] = resolver
}

func (s *LiveSubjects) Subject(ctx context.Context, ref livedecision.EntryRef) (livedecisions_usecase.Trigger, bool, error) {
	resolver, ok := s.resolvers[shared.EntryType(ref.EntryType)]
	if !ok || resolver == nil {
		return livedecisions_usecase.Trigger{}, false, fmt.Errorf("no analysis resolver for %q", ref.EntryType)
	}
	subject, err := resolver(ctx, ref.EntryID)
	if err != nil || subject == nil {
		return livedecisions_usecase.Trigger{}, false, err
	}
	return livedecisions_usecase.Trigger{
		WorkspaceID: subject.WorkspaceID,
		EntryID:     ref.EntryID,
		EntryType:   shared.EntryType(ref.EntryType),
		Features: livedecision.Features{
			Staging:  subject.EnableAutoStaging,
			Analysis: subject.EnableAnalysis,
		},
	}, true, nil
}

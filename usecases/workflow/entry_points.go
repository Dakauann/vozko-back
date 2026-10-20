package workflow_usecase

import (
	"errors"

	"vozko/domain/shared"
	"vozko/domain/workflow"
)

var ErrEntryPointsUnavailable = errors.New("workflow: entry points cannot be read")

type EntryPointAttacher interface {
	Attach(workspaceID string, workflows []*workflow.Workflow) error
}

type EntryPoints struct {
	reader workflow.EntryPointReader
}

func NewEntryPoints(reader workflow.EntryPointReader) *EntryPoints {
	return &EntryPoints{reader: reader}
}

func (e *EntryPoints) Attach(workspaceID string, workflows []*workflow.Workflow) error {
	if len(workflows) == 0 {
		return nil
	}
	if e == nil || e.reader == nil {
		return ErrEntryPointsUnavailable
	}
	var published []string
	for _, w := range workflows {
		if w != nil && w.PublishedVersion != nil {
			published = append(published, w.ID)
		}
	}
	triggers := map[string][]workflow.Node{}
	if len(published) > 0 {
		read, err := e.reader.PublishedTriggers(published)
		if err != nil {
			return err
		}
		triggers = read
	}
	sources, err := e.reader.EntryPointSources(workflow.EntryPointQueryOf(workspaceID, workflows, triggers))
	if err != nil {
		return err
	}
	for _, w := range workflows {
		if w != nil {
			w.EntryPoints = workflow.EntryPointsOf(w, workflow.RunningTriggers(w, triggers), sources.FactsOf(w.ID))
		}
	}
	return nil
}

type listWithEntryPoints struct {
	list        workflow.ListWorkflowsUseCase
	entryPoints EntryPointAttacher
}

func NewListWorkflowsWithEntryPoints(list workflow.ListWorkflowsUseCase, entryPoints EntryPointAttacher) workflow.ListWorkflowsUseCase {
	return &listWithEntryPoints{list: list, entryPoints: entryPoints}
}

func (uc *listWithEntryPoints) Execute(input workflow.ListWorkflowsInput) (*shared.PaginatedResult[*workflow.Workflow], error) {
	page, err := uc.list.Execute(input)
	if err != nil || page == nil {
		return page, err
	}
	if uc.entryPoints == nil {
		return nil, ErrEntryPointsUnavailable
	}
	if err := uc.entryPoints.Attach(input.WorkspaceID, page.Items); err != nil {
		return nil, err
	}
	return page, nil
}

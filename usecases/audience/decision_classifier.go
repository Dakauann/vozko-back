package audience_usecase

import (
	"context"
	"log"
	"strings"

	"golang.org/x/sync/errgroup"

	ca "vozko/domain/audience"
	"vozko/domain/decision"
	ld "vozko/domain/livedecision"
)

const parallelDecisions = 8

type LiveWorkspaces interface {
	Acts(ctx context.Context, workspaceID string) bool
}

type decisionClassifier struct {
	llm          ca.Classifier
	model        decision.Model
	live         LiveWorkspaces
	summaryModel string
}

func NewDecisionClassifier(llm ca.Classifier, model decision.Model, live LiveWorkspaces, summaryModel string) ca.Classifier {
	if model == nil || live == nil {
		return llm
	}
	return &decisionClassifier{llm: llm, model: model, live: live, summaryModel: strings.TrimSpace(summaryModel)}
}

func (c *decisionClassifier) Classify(ctx context.Context, req ca.ClassifyRequest) (*ca.ClassifyResult, error) {
	if req.SummaryOnly || !c.live.Acts(ctx, req.WorkspaceID) {
		return c.llm.Classify(ctx, req)
	}
	labels, model, err := c.labels(ctx, req)
	if err != nil {
		log.Printf("[audience-decisions] workspace %s falls back to the full llm classification: %v", req.WorkspaceID, err)
		return c.llm.Classify(ctx, req)
	}
	if req.SubjectKind != ca.SubjectKindConversation {
		return &ca.ClassifyResult{Results: labels, FinishReason: "stop", Model: model}, nil
	}
	return c.withSummaries(ctx, req, labels)
}

func (c *decisionClassifier) withSummaries(ctx context.Context, req ca.ClassifyRequest, labels []ca.BatchResult) (*ca.ClassifyResult, error) {
	summary := req
	summary.SummaryOnly = true
	if c.summaryModel != "" {
		summary.Model = c.summaryModel
	}
	res, err := c.llm.Classify(ctx, summary)
	if err != nil || res == nil || res.FinishReason == "length" {
		return res, err
	}
	byRef := make(map[int]ca.BatchResult, len(labels))
	for _, label := range labels {
		byRef[label.Ref] = label
	}
	merged := make([]ca.BatchResult, 0, len(res.Results))
	for _, text := range res.Results {
		if label, ok := byRef[text.Ref]; ok {
			merged = append(merged, label.WithText(text))
		}
	}
	res.Results = merged
	return res, nil
}

func (c *decisionClassifier) labels(ctx context.Context, req ca.ClassifyRequest) ([]ca.BatchResult, string, error) {
	results := make([]ca.BatchResult, len(req.Batch.Items))
	models := make([]string, len(req.Batch.Items))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(parallelDecisions)
	for i, item := range req.Batch.Items {
		group.Go(func() error {
			result, model, err := c.label(groupCtx, req, item)
			results[i], models[i] = result, model
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return nil, "", err
	}
	var model string
	if len(models) > 0 {
		model = models[0]
	}
	return results, model, nil
}

func (c *decisionClassifier) label(ctx context.Context, req ca.ClassifyRequest, item ca.PlannedItem) (ca.BatchResult, string, error) {
	request := decision.Request{WorkspaceID: req.WorkspaceID, ReferenceID: item.ID}
	if req.SubjectKind == ca.SubjectKindConversation {
		request.Purpose = ld.PurposeAnalysis
		request.State = conversationState(req, item)
		request.Questions = ca.ConversationDecisionQuestions()
	} else {
		request.Purpose = ld.PurposeComment
		request.State = map[string]string{"publicacao": req.Context.Caption, "comentario": item.Text}
		request.Questions = ca.CommentDecisionQuestions(req.Topics)
	}
	result, err := c.model.Decide(ctx, request)
	if err != nil {
		return ca.BatchResult{}, "", err
	}
	classification, err := classificationOf(req, result)
	if err != nil {
		return ca.BatchResult{}, "", err
	}
	return ca.NewBatchResult(item.Ref, req.SubjectKind, classification), result.Model, nil
}

func classificationOf(req ca.ClassifyRequest, result decision.Result) (ca.Classification, error) {
	if req.SubjectKind == ca.SubjectKindConversation {
		reading, err := ca.ConversationReadingFrom(result)
		return reading.Classification, err
	}
	return ca.CommentClassificationFrom(result, req.Topics)
}

func conversationState(req ca.ClassifyRequest, item ca.PlannedItem) map[string]string {
	state := map[string]string{"conversa": item.Text}
	if goal := strings.TrimSpace(req.Context.Caption); goal != "" {
		state["objetivo_da_campanha"] = truncateRunes(goal, 1200)
	}
	if instructions := strings.TrimSpace(req.Instructions); instructions != "" {
		state["contexto_do_operador"] = truncateRunes(instructions, ca.MaxInstructionsRunes)
	}
	return state
}

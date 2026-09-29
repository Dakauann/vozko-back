package audience_usecase

import (
	"context"
	"testing"

	ca "vozko/domain/audience"
	"vozko/domain/decision"
	ld "vozko/domain/livedecision"
	"vozko/domain/shared"
)

type fakeRoleInferrer struct{ calls []ca.RoleInferRequest }

func (f *fakeRoleInferrer) InferRole(_ context.Context, req ca.RoleInferRequest) (*ca.RoleInferResult, error) {
	f.calls = append(f.calls, req)
	return &ca.RoleInferResult{Role: ca.RoleUnknown, Confidence: "none", Rationale: "sem pistas", Model: "llm"}, nil
}

func roleRequest() ca.RoleInferRequest {
	comments := make([]string, ca.MinCommentsForRole)
	for i := range comments {
		comments[i] = "como jornalista, cobri essa obra " + itoa(i)
	}
	return ca.RoleInferRequest{WorkspaceID: "ws-1", Model: "m", Instructions: "Prefeitura", Comments: comments}
}

func roleAnswer(role ca.AuthorRole, certainty float64) func(decision.Request) map[string]decision.Answer {
	return func(decision.Request) map[string]decision.Answer {
		return map[string]decision.Answer{ca.FieldRole: {Kind: decision.KindChoice, Choice: string(role), Confidence: certainty}}
	}
}

func TestALiveWorkspaceInfersTheAuthorRoleWithDecisions(t *testing.T) {
	llm := &fakeRoleInferrer{}
	model := &answeringModel{answers: roleAnswer(ca.RoleJournalist, 0.9)}
	res, err := NewDecisionRoleInferrer(llm, model, liveSwitch(true)).InferRole(context.Background(), roleRequest())
	if err != nil {
		t.Fatal(err)
	}
	if len(llm.calls) != 0 {
		t.Fatal("the llm must not be called when the decision is valid")
	}
	if res.Role != ca.RoleJournalist || res.Confidence != string(shared.QualityLevelHigh) || res.Rationale != "" || res.Model != "typesafe/jev-1.13" {
		t.Fatalf("result = %+v", res)
	}
	request := model.requests[0]
	state := request.State.(map[string]any)
	if request.Purpose != ld.PurposeAuthorRole || state["contexto_da_conta"] != "Prefeitura" || len(state["comentarios"].([]string)) != ca.MinCommentsForRole {
		t.Fatalf("request = %+v", request)
	}
}

func TestTheRoleInferrerFallsBackToTheLLMOnAnyDecisionFailure(t *testing.T) {
	llm := &fakeRoleInferrer{}
	failing := &answeringModel{answers: roleAnswer(ca.RoleJournalist, 0.9), failOn: 1}
	res, err := NewDecisionRoleInferrer(llm, failing, liveSwitch(true)).InferRole(context.Background(), roleRequest())
	if err != nil || len(llm.calls) != 1 || res.Model != "llm" {
		t.Fatalf("res %+v err %v calls %d", res, err, len(llm.calls))
	}

	foreign := &answeringModel{answers: roleAnswer("astronaut", 0.9)}
	if _, err := NewDecisionRoleInferrer(llm, foreign, liveSwitch(true)).InferRole(context.Background(), roleRequest()); err != nil || len(llm.calls) != 2 {
		t.Fatalf("an invalid role must fall back too: err %v calls %d", err, len(llm.calls))
	}
}

func TestAWorkspaceNotActingLiveKeepsTheLLMRoleInferrer(t *testing.T) {
	llm := &fakeRoleInferrer{}
	model := &answeringModel{answers: roleAnswer(ca.RoleJournalist, 0.9)}
	if _, err := NewDecisionRoleInferrer(llm, model, liveSwitch(false)).InferRole(context.Background(), roleRequest()); err != nil {
		t.Fatal(err)
	}
	if len(model.requests) != 0 || len(llm.calls) != 1 {
		t.Fatalf("decisions %d llm %d", len(model.requests), len(llm.calls))
	}
	if NewDecisionRoleInferrer(llm, nil, liveSwitch(true)) != ca.RoleInferrer(llm) {
		t.Fatal("no decision model means no decorator")
	}
}

func TestTooFewCommentsNeverReachEitherModel(t *testing.T) {
	llm := &fakeRoleInferrer{}
	model := &answeringModel{answers: roleAnswer(ca.RoleJournalist, 0.9)}
	req := roleRequest()
	req.Comments = req.Comments[:2]
	if _, err := NewDecisionRoleInferrer(llm, model, liveSwitch(true)).InferRole(context.Background(), req); err == nil {
		t.Fatal("too few comments must be refused")
	}
	if len(model.requests) != 0 || len(llm.calls) != 0 {
		t.Fatal("no model may be called")
	}
}

package node_executors

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vozko/domain/ai"
	"vozko/domain/label"
	"vozko/domain/opportunity"
	"vozko/domain/workflow"
	"vozko/domain/workspace"
	lead_usecase "vozko/usecases/lead"
	opportunity_usecase "vozko/usecases/opportunity"
)

const unrelatedOutput = "zz_unrelated"

type labelRouted struct {
	output   string
	fallback string
	run      func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult
}

type entryLabelsStub struct {
	label.Repository
	labels []*label.EntryLabel
}

func (s *entryLabelsStub) GetEntryLabels(string, string, string) ([]*label.EntryLabel, error) {
	return s.labels, nil
}

type toolCallingAI struct {
	ai.Service
	output *ai.GenerateOutput
}

func (s *toolCallingAI) Generate(context.Context, ai.GenerateInput) (*ai.GenerateOutput, error) {
	return s.output, nil
}

func withSource(edges []workflow.Edge, source string) []workflow.Edge {
	out := make([]workflow.Edge, len(edges))
	for i, e := range edges {
		e.Source = source
		out[i] = e
	}
	return out
}

func executed(t *testing.T, executor workflow.NodeExecutor, ctx *workflow.NodeContext) *workflow.NodeResult {
	t.Helper()
	result, err := executor.Execute(ctx)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	return result
}

func routedCtx(nodeType workflow.NodeType, config map[string]interface{}, edges []workflow.Edge) *workflow.NodeContext {
	return stageNodeCtx(nodeType, config, edges)
}

func slowServer(t *testing.T, status int, delay time.Duration) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func httpRouted(url string, timeoutSeconds float64) func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
	return func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
		state := workflow.NewRunState()
		ctx := httpNodeCtx("n1", map[string]interface{}{"url": url, "method": "GET", "timeout_seconds": timeoutSeconds}, &state)
		ctx.Graph.Edges = edges
		return executed(t, NewHTTPRequestExecutor(testEgress(), nil), ctx)
	}
}

func emailRouted(sendErr error) func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
	return func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
		ctx := newSendEmailNodeCtx(validSendEmailNodeConfig())
		ctx.Graph.Edges = withSource(edges, ctx.Node.ID)
		return executed(t, NewSendEmailExecutor(&sendEmailNodeSenderMock{err: sendErr}), ctx)
	}
}

func agentRouted(output *ai.GenerateOutput, config map[string]interface{}) func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
	return func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
		state := workflow.NewRunState()
		ctx := agentNodeContext(&state, config)
		ctx.Graph.Edges = withSource(edges, ctx.Node.ID)
		return executed(t, NewAIAgentExecutor(&toolCallingAI{output: output}, nil, &entryTranscript{}, nil, nil, nil), ctx)
	}
}

func labelRoutedNodes(t *testing.T) map[string]labelRouted {
	failing := errors.New("falhou")
	won := opportunity.EntryDeals{wonDeal}
	vip := []*label.EntryLabel{{LabelID: "vip", LabelName: "VIP"}}
	agentTools := map[string]interface{}{"custom_tools": []interface{}{map[string]interface{}{"name": "agendar", "description": "Agenda uma visita"}}}
	return map[string]labelRouted{
		"assign label succeeds": {output: "sucesso", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewAssignLabelExecutor(&labelerStub{change: label.LabelChange{LabelID: "vip"}}), routedCtx(workflow.NodeTypeActionAssignLabel, map[string]interface{}{"label_id": "vip"}, edges))
		}},
		"assign label fails": {output: "erro", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewAssignLabelExecutor(&labelerStub{err: failing}), routedCtx(workflow.NodeTypeActionAssignLabel, map[string]interface{}{"label_id": "vip"}, edges))
		}},
		"assign member succeeds": {output: "sucesso", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewAssignMemberExecutor(&assignMemberWorkspaceMock{member: &workspace.Member{UserID: "u-1"}}, &assignMemberHandOffMock{}), assignMemberCtx(map[string]interface{}{"member_id": "m-1"}, edges))
		}},
		"assign member fails": {output: "erro", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewAssignMemberExecutor(nil, nil), assignMemberCtx(map[string]interface{}{"member_id": "m-1"}, edges))
		}},
		"finish conversation succeeds": {output: "sucesso", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewFinishConversationExecutor(&finishStatusMock{}), finishConvCtx("e-1", "whatsapp", map[string]interface{}{}, edges))
		}},
		"finish conversation fails": {output: "erro", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewFinishConversationExecutor(&finishStatusMock{err: failing}), finishConvCtx("e-1", "whatsapp", map[string]interface{}{}, edges))
		}},
		"move stage succeeds": {output: "sucesso", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewMoveStageExecutor(stagePipelines(), &stageMoverStub{}, nil), routedCtx(workflow.NodeTypeActionMoveStage, map[string]interface{}{"stage_id": "st-won"}, edges))
		}},
		"move stage fails": {output: "erro", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewMoveStageExecutor(nil, nil, nil), routedCtx(workflow.NodeTypeActionMoveStage, map[string]interface{}{"stage_id": "st-won"}, edges))
		}},
		"check stage matches": {output: "true", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewCheckStageExecutor(stagePipelines()), routedCtx(workflow.NodeTypeConditionCheckStage, map[string]interface{}{"stage_id": "st-new"}, edges))
		}},
		"check stage differs": {output: "false", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewCheckStageExecutor(stagePipelines()), routedCtx(workflow.NodeTypeConditionCheckStage, map[string]interface{}{"stage_id": "st-won"}, edges))
		}},
		"text match finds a case": {output: "sim", fallback: "default", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewTextMatchExecutor(), routedCtx(workflow.NodeTypeConditionTextMatch, map[string]interface{}{"text": "Sim", "cases": []interface{}{map[string]interface{}{"value": "sim"}}}, edges))
		}},
		"text match finds nothing": {output: "default", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewTextMatchExecutor(), routedCtx(workflow.NodeTypeConditionTextMatch, map[string]interface{}{"text": "talvez", "cases": []interface{}{map[string]interface{}{"value": "sim"}}}, edges))
		}},
		"transfer department succeeds": {output: "sucesso", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, newTransfer(&rouletteHandOffStub{owner: "user-9"}), routedCtx(workflow.NodeTypeActionTransferDepartment, map[string]interface{}{"department_id": "sales"}, edges))
		}},
		"transfer department fails": {output: "erro", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, newTransfer(&rouletteHandOffStub{err: failing}), routedCtx(workflow.NodeTypeActionTransferDepartment, map[string]interface{}{"department_id": "sales"}, edges))
		}},
		"update lead succeeds": {output: "sucesso", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			stub := &leadProfilesStub{result: lead_usecase.ProfileResult{LeadID: "l-1"}}
			return executed(t, NewUpdateLeadExecutor(stub), routedCtx(workflow.NodeTypeActionUpdateLead, map[string]interface{}{"district": "Centro"}, edges))
		}},
		"update lead fails": {output: "erro", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			stub := &leadProfilesStub{err: failing}
			return executed(t, NewUpdateLeadExecutor(stub), routedCtx(workflow.NodeTypeActionUpdateLead, map[string]interface{}{"district": "Centro"}, edges))
		}},
		"manage deal succeeds": {output: "sucesso", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewManageOpportunityExecutor(&dealDeskStub{}), routedCtx(workflow.NodeTypeActionManageOpportunity, map[string]interface{}{"pipeline_id": "pl-deals", "action": "create_new", "title": "X"}, edges))
		}},
		"manage deal fails": {output: "erro", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			desk := &dealDeskStub{manageFn: func(opportunity_usecase.EntryCommand) (*opportunity_usecase.EntryResult, error) { return nil, failing }}
			return executed(t, NewManageOpportunityExecutor(desk), routedCtx(workflow.NodeTypeActionManageOpportunity, map[string]interface{}{"pipeline_id": "pl-deals", "action": "create_new", "title": "X"}, edges))
		}},
		"check deal matches": {output: "true", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewCheckOpportunityExecutor(&dealDeskStub{deals: won}), routedCtx(workflow.NodeTypeConditionCheckOpportunity, map[string]interface{}{"pipeline_id": "pl-deals", "check": "exists"}, edges))
		}},
		"check deal differs": {output: "false", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewCheckOpportunityExecutor(&dealDeskStub{}), routedCtx(workflow.NodeTypeConditionCheckOpportunity, map[string]interface{}{"pipeline_id": "pl-deals", "check": "exists"}, edges))
		}},
		"check label matches": {output: "true", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewCheckLabelExecutor(&entryLabelsStub{labels: vip}), routedCtx(workflow.NodeTypeConditionCheckLabel, map[string]interface{}{"label_id": "vip"}, edges))
		}},
		"check label differs": {output: "false", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewCheckLabelExecutor(&entryLabelsStub{}), routedCtx(workflow.NodeTypeConditionCheckLabel, map[string]interface{}{"label_id": "vip"}, edges))
		}},
		"consent is recorded": {output: "sucesso", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewConsentExecutor(consentNodeDeps(&consentCommandsStub{})), routedCtx(workflow.NodeTypeActionConsent, map[string]interface{}{workflow.ConsentModeKey: workflow.ConsentModeWithdraw}, edges))
		}},
		"condition is true": {output: "true", fallback: "yes", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			ctx := routedCtx(workflow.NodeTypeConditionBranch, map[string]interface{}{"variable": "{{resposta}}", "operator": "eq", "value": "sim"}, edges)
			ctx.State.Set("resposta", "sim")
			return executed(t, NewConditionBranchExecutor(), ctx)
		}},
		"condition is false": {output: "false", fallback: "no", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			ctx := routedCtx(workflow.NodeTypeConditionBranch, map[string]interface{}{"variable": "{{resposta}}", "operator": "eq", "value": "nao"}, edges)
			ctx.State.Set("resposta", "sim")
			return executed(t, NewConditionBranchExecutor(), ctx)
		}},
		"channel branch": {output: "instagram", fallback: workflow.ChannelBranchDefault, run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			ctx := routedCtx(workflow.NodeTypeConditionChannel, map[string]interface{}{}, edges)
			ctx.Run.EntryType = "instagram"
			return executed(t, NewChannelBranchExecutor(), ctx)
		}},
		"loop has items": {output: "body", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			ctx := routedCtx(workflow.NodeTypeActionLoop, map[string]interface{}{"list_variable": "itens"}, edges)
			ctx.State.Set("itens", []interface{}{"a"})
			return executed(t, NewLoopExecutor(), ctx)
		}},
		"loop is done": {output: "done", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			ctx := routedCtx(workflow.NodeTypeActionLoop, map[string]interface{}{"list_variable": "itens"}, edges)
			ctx.State.Set("itens", []interface{}{})
			return executed(t, NewLoopExecutor(), ctx)
		}},
		"loop has no list": {output: "fail", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewLoopExecutor(), routedCtx(workflow.NodeTypeActionLoop, map[string]interface{}{"list_variable": "nada"}, edges))
		}},
		"http answers":    {output: "sucesso", run: httpRouted(slowServer(t, http.StatusOK, 0), 5)},
		"http refuses":    {output: "falha", run: httpRouted(slowServer(t, http.StatusInternalServerError, 0), 5)},
		"http times out":  {output: "timeout", fallback: "falha", run: httpRouted(slowServer(t, http.StatusOK, 2*time.Second), 0.2)},
		"email is sent":   {output: "sucesso", run: emailRouted(nil)},
		"email fails":     {output: "falha", run: emailRouted(failing)},
		"email times out": {output: "timeout", fallback: "falha", run: emailRouted(context.DeadlineExceeded)},
		"reply to agent": {output: "default", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			ctx := replyToAgentCtx(true, replyFields())
			ctx.Graph.Edges = append([]workflow.Edge{{Source: "t", Target: "r"}}, withSource(edges, "r")...)
			return executed(t, NewReplyToAgentExecutor(), ctx)
		}},
		"agent calls a tool":    {output: "agendar", run: agentRouted(&ai.GenerateOutput{ToolCalls: []ai.ToolCall{{Name: "agendar"}}}, agentTools)},
		"agent answers in text": {output: "default", run: agentRouted(&ai.GenerateOutput{Message: ai.Message{Role: ai.RoleAssistant, Content: "Oi"}}, nil)},
		"calendar availability fails": {output: "erro", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewCheckCalendarAvailabilityExecutor(nil, nil), routedCtx(workflow.NodeTypeActionCheckCalendarAvailability, map[string]interface{}{}, edges))
		}},
		"schedule meeting fails": {output: "erro", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewScheduleMeetingExecutor(nil, nil), routedCtx(workflow.NodeTypeActionScheduleMeeting, map[string]interface{}{}, edges))
		}},
		"reschedule meeting fails": {output: "erro", run: func(t *testing.T, edges []workflow.Edge) *workflow.NodeResult {
			return executed(t, NewRescheduleMeetingExecutor(nil), routedCtx(workflow.NodeTypeActionRescheduleMeeting, map[string]interface{}{}, edges))
		}},
	}
}

func TestEveryLabelRoutedNodeFollowsItsOutputOrItsFallbackAndNeverAnArbitraryEdge(t *testing.T) {
	for name, node := range labelRoutedNodes(t) {
		t.Run(name+"/matched", func(t *testing.T) {
			result := node.run(t, []workflow.Edge{
				{Source: "n1", Target: "elsewhere", Label: unrelatedOutput},
				{Source: "n1", Target: "chosen", Label: node.output},
			})
			if result.NextNodeID != "chosen" || result.Error != "" {
				t.Fatalf("result = %+v, want the %q edge", result, node.output)
			}
		})
		if node.fallback != "" {
			t.Run(name+"/unmatched with fallback", func(t *testing.T) {
				result := node.run(t, []workflow.Edge{
					{Source: "n1", Target: "elsewhere", Label: unrelatedOutput},
					{Source: "n1", Target: "fallback", Label: node.fallback},
				})
				if result.NextNodeID != "fallback" || result.Error != "" {
					t.Fatalf("result = %+v, want the fallback %q", result, node.fallback)
				}
			})
		}
		t.Run(name+"/unmatched without fallback", func(t *testing.T) {
			result := node.run(t, []workflow.Edge{{Source: "n1", Target: "elsewhere", Label: unrelatedOutput}})
			if result.NextNodeID != "" || result.ErrorKind != workflow.ErrorKindConfig || !strings.Contains(result.Error, workflow.ErrOutputNotConnected.Error()) {
				t.Fatalf("result = %+v, want a config failure for the unconnected %q", result, node.output)
			}
		})
	}
}

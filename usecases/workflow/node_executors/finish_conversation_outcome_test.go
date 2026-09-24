package node_executors

import (
	"testing"

	"github.com/stretchr/testify/require"

	"vozko/domain/actor"
	"vozko/domain/conversation"
)

// The builder picks the outcome from the workspace catalogue, the same list a
// person chooses from when finishing a conversation.
func TestFinishConversationExecutorOffersTheOutcomeCatalogue(t *testing.T) {
	def := NewFinishConversationExecutor(&finishStatusMock{}).Definition()

	var found bool
	for _, field := range def.ConfigSchema {
		if field.Key == "outcome_code" {
			found = true
			require.Equal(t, "outcomes", field.OptionsSource)
		}
	}
	require.True(t, found, "the node has no outcome field to fill")
}

func TestFinishConversationExecutorRecordsTheWorkflowAndItsOutcome(t *testing.T) {
	st := &finishStatusMock{}
	ctx := finishConvCtx("e-1", "unofficial_whatsapp", map[string]interface{}{"outcome_code": "sale"}, finishConvEdges())
	ctx.Run.WorkflowID = "wf-1"

	res, err := NewFinishConversationExecutor(st).Execute(ctx)

	require.NoError(t, err)
	require.Equal(t, "ok", res.NextNodeID)
	require.Equal(t, "sale", st.lastOpts.OutcomeCode)
	require.Equal(t, actor.FormatWorkflow("wf-1"), st.lastOpts.ActorID)
	require.Equal(t, "sale", res.Output["close_outcome"])
}

func TestFinishConversationExecutorExplainsARefusedOutcome(t *testing.T) {
	for _, refusal := range []error{conversation.ErrOutcomeRequired, conversation.ErrOutcomeUnknown} {
		st := &finishStatusMock{err: refusal}
		ctx := finishConvCtx("e-1", "whatsapp", map[string]interface{}{}, finishConvEdges())

		res, err := NewFinishConversationExecutor(st).Execute(ctx)

		require.NoError(t, err)
		require.Equal(t, "fail", res.NextNodeID)
		require.Contains(t, res.Output["error"], "desfecho")
	}
}

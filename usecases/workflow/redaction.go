package workflow_usecase

import (
	"vozko/domain/redact"
	"vozko/domain/tracepayload"
	"vozko/domain/workflow"
)

var outbound = redact.New()

func redactedRun(run *workflow.WorkflowRun) *workflow.WorkflowRun {
	if run == nil {
		return nil
	}
	cp := *run
	cp.State = workflow.RunState{Vars: outbound.Map(run.State.Vars)}
	cp.Error = outbound.String(run.Error)
	cp.AgentResult = outbound.Map(run.AgentResult)
	cp.TriggerData = outbound.Map(run.TriggerData)
	return &cp
}

func redactedRunLogs(logs []*workflow.WorkflowRunLog) []*workflow.WorkflowRunLog {
	out := make([]*workflow.WorkflowRunLog, len(logs))
	for i, entry := range logs {
		if entry == nil {
			continue
		}
		out[i] = redactedStep(entry)
	}
	return out
}

func redactedStep(entry *workflow.WorkflowRunLog, scrubbers ...redact.Redactor) *workflow.WorkflowRunLog {
	cp := *entry
	passes := append(append(make([]redact.Redactor, 0, len(scrubbers)+1), scrubbers...), outbound)
	for _, r := range passes {
		cp.Input = r.Map(cp.Input)
		cp.Output = r.Map(cp.Output)
		cp.Error = r.String(cp.Error)
		cp.ErrorChain = r.Strings(cp.ErrorChain)
		cp.Stack = r.String(cp.Stack)
		cp.ScriptLogs = redactedScriptLogs(r, cp.ScriptLogs)
		cp.HTTP = redactedExchanges(r, cp.HTTP)
	}
	return &cp
}

const maxChainEntryBytes = tracepayload.MaxBytes / workflow.MaxErrorChain

func storedStep(entry *workflow.WorkflowRunLog, scrubbers ...redact.Redactor) *workflow.WorkflowRunLog {
	cp := redactedStep(entry, scrubbers...)
	cp.Input = tracepayload.Map(cp.Input)
	cp.Output = tracepayload.Map(cp.Output)
	cp.Error = tracepayload.MarkedText(cp.Error)
	cp.Stack = tracepayload.MarkedText(cp.Stack)
	for i, link := range cp.ErrorChain {
		cp.ErrorChain[i] = tracepayload.MarkedTextWithin(link, maxChainEntryBytes)
	}
	cp.ScriptLogs = workflow.FitScriptLogs(cp.ScriptLogs)
	cp.HTTP = workflow.FitHTTPExchanges(cp.HTTP)
	return cp
}

func redactedScriptLogs(r redact.Redactor, logs []workflow.ScriptLog) []workflow.ScriptLog {
	if logs == nil {
		return nil
	}
	out := make([]workflow.ScriptLog, len(logs))
	for i, entry := range logs {
		out[i] = workflow.ScriptLog{Level: entry.Level, Message: r.String(entry.Message)}
	}
	return out
}

func redactedExchanges(r redact.Redactor, exchanges []workflow.HTTPExchange) []workflow.HTTPExchange {
	if exchanges == nil {
		return nil
	}
	out := make([]workflow.HTTPExchange, len(exchanges))
	for i, x := range exchanges {
		out[i] = x.Redact(r)
	}
	return out
}

func redactedTestOutput(o *TestNodeOutput) *TestNodeOutput {
	if o == nil {
		return nil
	}
	cp := *o
	cp.Error = outbound.String(o.Error)
	cp.InterpolatedConfig = tracepayload.Map(outbound.Map(o.InterpolatedConfig))
	cp.ExecutionOutput = tracepayload.Map(outbound.Map(o.ExecutionOutput))
	cp.StateAfter = tracepayload.Map(outbound.Map(o.StateAfter))
	if o.PathStop != nil {
		stop := *o.PathStop
		stop.Message = outbound.String(stop.Message)
		cp.PathStop = &stop
	}
	return &cp
}

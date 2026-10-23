package workflow

import "fmt"

const LintSendOutsideWindowRisk LintIssueCode = "SEND_OUTSIDE_WINDOW_RISK"

type WindowRisk struct {
	SendNodeID    string
	TriggerNodeID string
}

func (n NodeType) SendsFreeForm() bool {
	switch {
	case n == NodeTypeActionSendText, n == NodeTypeActionSendMedia, n == NodeTypeActionAIAgent:
		return true
	}
	return n.IsInteractivePrompt() || n.IsFormPrompt()
}

func SendOutsideWindowRisks(g *Graph) []WindowRisk {
	firstTrigger := map[string]string{}
	for _, trigger := range g.TriggerNodes() {
		window := DefaultServiceWindow(TriggerType(trigger.Type))
		if window == nil || *window == ServiceWindowOpen {
			continue
		}
		for id := range reachedFrom(g, []string{trigger.ID}, opensTheWindow) {
			if _, named := firstTrigger[id]; !named {
				firstTrigger[id] = trigger.ID
			}
		}
	}
	var risks []WindowRisk
	for _, n := range g.Nodes {
		if trigger, reached := firstTrigger[n.ID]; reached && n.Type.SendsFreeForm() {
			risks = append(risks, WindowRisk{SendNodeID: n.ID, TriggerNodeID: trigger})
		}
	}
	return risks
}

func opensTheWindow(source *Node, e Edge) bool {
	if source == nil {
		return false
	}
	switch {
	case source.Type == NodeTypeActionSendTemplate:
		return true
	case source.Type.IsFormPrompt():
		return e.Label == FormHandleCompleted
	}
	return isReplyEdge(source, e)
}

func reachedFrom(g *Graph, starts []string, blocks func(source *Node, e Edge) bool) map[string]bool {
	visited := map[string]bool{}
	queue := append([]string(nil), starts...)
	for _, id := range starts {
		visited[id] = true
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		source := g.FindNode(current)
		for _, e := range g.OutgoingEdges(current) {
			if visited[e.Target] || blocks(source, e) {
				continue
			}
			visited[e.Target] = true
			queue = append(queue, e.Target)
		}
	}
	return visited
}

func lintSendsOutsideWindow(g *Graph, add func(LintIssue)) {
	for _, risk := range SendOutsideWindowRisks(g) {
		trigger := g.FindNode(risk.TriggerNodeID)
		name := trigger.DisplayName()
		if name == "" {
			name = trigger.ID
		}
		add(LintIssue{
			Code: LintSendOutsideWindowRisk, Severity: SeverityAdvisory,
			NodeID:  risk.SendNodeID,
			Message: fmt.Sprintf("O nó %q envia uma mensagem livre num caminho que começa no gatilho %q, que normalmente roda com a janela de atendimento fechada; em produção o envio falha", risk.SendNodeID, name),
			Hint:    WindowClosedFix,
		})
	}
}

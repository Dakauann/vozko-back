package workflow

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	LintDataRefNeverSet  LintIssueCode = "DATA_REF_NEVER_SET"
	LintDataRefSomePaths LintIssueCode = "DATA_REF_SOME_PATHS"
)

type fieldRead struct {
	field string
	ref   string
}

func lintDataRefAvailability(g *Graph, defs map[NodeType]NodeDefinition, add func(LintIssue)) {
	flow := newVariableFlow(g, defs, VariableSources{Specs: TriggerInputSpecsOf(g, TriggerInputSources{})})
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if !flow.reached[i] || n.Type.IsTrigger() {
			continue
		}
		for _, read := range fieldReadsOf(n) {
			switch availabilityOfRead(flow, i, read.ref) {
			case availableNever:
				add(LintIssue{
					Code: LintDataRefNeverSet, Severity: SeverityBlocking,
					NodeID: n.ID, Field: read.field, Path: read.ref,
					Message: fmt.Sprintf("O nó %q usa {{%s}} em %q, mas nenhum gatilho ou passo anterior fornece esse dado; o valor sempre fica vazio", n.ID, read.ref, read.field),
					Hint:    "Escolha um dado da lista de variáveis disponíveis neste nó, ou adicione antes um passo que defina esse dado.",
				})
			case AvailableOnSomePaths:
				add(LintIssue{
					Code: LintDataRefSomePaths, Severity: SeverityAdvisory,
					NodeID: n.ID, Field: read.field, Path: read.ref,
					Message: fmt.Sprintf("O nó %q usa {{%s}} em %q, mas só alguns caminhos até ele fornecem esse dado; nos outros o valor fica vazio", n.ID, read.ref, read.field),
					Hint:    "Faça todos os caminhos até este nó passarem por um passo que defina o dado, ou trate o valor vazio antes.",
				})
			}
		}
	}
}

func availabilityOfRead(flow *variableFlow, idx int, ref string) VariableAvailability {
	if key, isSys := strings.CutPrefix(ref, "sys."); isSys {
		if _, builtin := builtinSys(key); builtin {
			return AvailableAlways
		}
		return availableNever
	}
	return flow.availabilityAt(idx, ref)
}

func fieldReadsOf(n *Node) []fieldRead {
	var reads []fieldRead
	seen := map[string]bool{}
	for _, field := range sortedKeys(n.Config) {
		if n.Type.Canonical() == NodeTypeActionCode && field == "code" {
			continue
		}
		for _, dep := range ExtractDependencies(map[string]interface{}{field: n.Config[field]}) {
			ref := canonicalRef(dep.FullRef)
			if seen[ref] || !checkedRef(dep.Scope, ref) {
				continue
			}
			seen[ref] = true
			reads = append(reads, fieldRead{field: field, ref: ref})
		}
	}
	return reads
}

func checkedRef(scope, ref string) bool {
	if ref == "" || scope == SecretScope || scope == "node" || strings.HasPrefix(ref, "_") {
		return false
	}
	head, _, _ := strings.Cut(ref, ".")
	return !onlyDigits(head)
}

func onlyDigits(s string) bool {
	return s != "" && strings.IndexFunc(s, func(r rune) bool { return !unicode.IsDigit(r) }) < 0
}

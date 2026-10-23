package workflow

import (
	"sort"
	"strings"
)

type VariableGroup string

const (
	VariableGroupTrigger VariableGroup = "trigger"
	VariableGroupNodes   VariableGroup = "nodes"
	VariableGroupContact VariableGroup = "contact"
	VariableGroupSystem  VariableGroup = "system"
	VariableGroupSecrets VariableGroup = "secrets"
)

type VariableKind string

const (
	VariableKindDefault VariableKind = "default"
	VariableKindTrigger VariableKind = "trigger"
	VariableKindNode    VariableKind = "node"
	VariableKindSystem  VariableKind = "system"
	VariableKindSecret  VariableKind = "secret"
)

type VariableType string

const (
	VariableTypeText    VariableType = "text"
	VariableTypeNumber  VariableType = "number"
	VariableTypeBoolean VariableType = "boolean"
	VariableTypeObject  VariableType = "object"
	VariableTypeList    VariableType = "list"
	VariableTypeDate    VariableType = "date"
	VariableTypeUnknown VariableType = "unknown"
)

type VariableAvailability string

const (
	AvailableAlways      VariableAvailability = "always"
	AvailableOnSomePaths VariableAvailability = "some_paths"
)

var availableNever VariableAvailability

type VariableSource struct {
	NodeID   string `json:"nodeId"`
	NodeName string `json:"nodeName"`
}

type CatalogVariable struct {
	Ref          string               `json:"ref" example:"input.cpf"`
	Label        string               `json:"label" example:"Enviado pelo agente › cpf"`
	Group        VariableGroup        `json:"group" enums:"trigger,nodes,contact,system,secrets"`
	Kind         VariableKind         `json:"kind" enums:"default,trigger,node,system,secret"`
	Source       *VariableSource      `json:"source"`
	Type         VariableType         `json:"type" enums:"text,number,boolean,object,list,date,unknown"`
	Example      *string              `json:"example"`
	Availability VariableAvailability `json:"availability" enums:"always,some_paths"`
}

type VariableCatalog struct {
	Variables []CatalogVariable `json:"variables"`
}

type VariableSources struct {
	Specs   []TriggerInputSpec
	Forms   map[string]TriggerForm
	Secrets []string
}

var variableGroupRank = map[VariableGroup]int{
	VariableGroupTrigger: 0,
	VariableGroupNodes:   1,
	VariableGroupContact: 2,
	VariableGroupSystem:  3,
	VariableGroupSecrets: 4,
}

func VariableCatalogAt(g *Graph, catalog []NodeDefinition, sources VariableSources, nodeID string) VariableCatalog {
	flow := newVariableFlow(g, NodeCatalogMap(catalog), sources)
	return VariableCatalog{Variables: flow.catalogAt(nodeID, sources.Secrets)}
}

type catalogCandidate struct {
	variable CatalogVariable
	distance int
	order    int
	seq      int
}

func (f *variableFlow) catalogAt(nodeID string, secrets []string) []CatalogVariable {
	var candidates []catalogCandidate
	if idx, reachable := f.index[nodeID]; reachable && f.reached[idx] {
		distance := f.graph.AncestorDistances(f.graph.Nodes[idx].ID, "")
		for seq, p := range f.provisions {
			if p.hidden || !f.may[idx].has(seq) {
				continue
			}
			candidates = append(candidates, catalogCandidate{
				variable: CatalogVariable{
					Ref: p.ref, Label: p.label, Group: p.group, Kind: p.kind,
					Source: f.sourceOf(p.node), Type: p.typ, Example: p.example,
					Availability: f.availabilityAt(idx, p.ref),
				},
				distance: distance[f.graph.Nodes[p.node].ID],
				order:    p.node,
				seq:      seq,
			})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if ra, rb := variableGroupRank[a.variable.Group], variableGroupRank[b.variable.Group]; ra != rb {
			return ra < rb
		}
		if a.distance != b.distance {
			return a.distance < b.distance
		}
		if a.order != b.order {
			return a.order < b.order
		}
		return a.seq < b.seq
	})
	out := make([]CatalogVariable, 0, len(candidates)+len(systemVariables)+len(secrets))
	seen := map[string]bool{}
	for _, c := range candidates {
		if seen[c.variable.Ref] {
			continue
		}
		seen[c.variable.Ref] = true
		out = append(out, c.variable)
	}
	out = append(out, systemVariables...)
	for _, name := range sortedSecretNames(secrets) {
		out = append(out, CatalogVariable{
			Ref: SecretScope + "." + name, Label: "Segredo › " + name, Group: VariableGroupSecrets, Kind: VariableKindSecret,
			Type: VariableTypeText, Availability: AvailableAlways,
		})
	}
	return out
}

func sortedSecretNames(names []string) []string {
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

var systemVariables = []CatalogVariable{
	{Ref: "sys.date", Label: "Sistema › data de hoje", Group: VariableGroupSystem, Kind: VariableKindDefault, Type: VariableTypeDate, Availability: AvailableAlways},
	{Ref: "sys.time", Label: "Sistema › hora agora", Group: VariableGroupSystem, Kind: VariableKindDefault, Type: VariableTypeText, Availability: AvailableAlways},
	{Ref: "sys.timestamp", Label: "Sistema › timestamp Unix", Group: VariableGroupSystem, Kind: VariableKindDefault, Type: VariableTypeNumber, Availability: AvailableAlways},
}

func (f *variableFlow) sourceOf(idx int) *VariableSource {
	n := &f.graph.Nodes[idx]
	return &VariableSource{NodeID: n.ID, NodeName: f.nodeName(idx)}
}

func (f *variableFlow) nodeName(idx int) string {
	n := &f.graph.Nodes[idx]
	if name := n.DisplayName(); name != "" {
		return name
	}
	if def, ok := f.defs[n.Type]; ok && def.Label != "" {
		return def.Label
	}
	return n.ID
}

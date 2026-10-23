package workflow

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var sentinelsThatAreNotDraftProblems = map[string]bool{
	"ErrNodeNotFound":         true,
	"ErrNodeExecutorNotFound": true,
}

func declaredSentinels(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var names []string
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				for _, name := range spec.(*ast.ValueSpec).Names {
					if strings.HasPrefix(name.Name, "ErrGraph") || strings.HasPrefix(name.Name, "ErrNode") {
						names = append(names, name.Name)
					}
				}
			}
		}
	}
	sort.Strings(names)
	return names
}

func classifiedSentinels(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "validation_class.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	classified := map[string]bool{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			if len(vs.Names) != 1 || vs.Names[0].Name != "validationClasses" {
				continue
			}
			ast.Inspect(vs, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && strings.HasPrefix(id.Name, "Err") {
					classified[id.Name] = true
				}
				return true
			})
		}
	}
	return classified
}

func TestEveryGraphAndNodeSentinelIsAClassifiedValidationErrorSoNoneFallsBackToAServerError(t *testing.T) {
	classified := classifiedSentinels(t)
	if len(classified) == 0 {
		t.Fatal("validationClasses was not found in validation_class.go")
	}
	for _, name := range declaredSentinels(t) {
		if sentinelsThatAreNotDraftProblems[name] {
			if classified[name] {
				t.Errorf("%s is not a draft problem but is classified as a validation error", name)
			}
			continue
		}
		if !classified[name] {
			t.Errorf("%s is declared but not listed in validationClasses, so the API would answer it with a 500", name)
		}
	}
}

func TestValidationErrorsAreRecognisedThroughWrapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"trigger not connected", ErrGraphTriggerNotConnected, true},
		{"wrapped label reference", fmt.Errorf("%w: node %q label_id %q", ErrNodeInvalidLabelID, "n1", "l1"), true},
		{"wrapped consent rule", fmt.Errorf("%w: node %q", ErrConsentGrantNeedsReply, "c"), true},
		{"invalid status", ErrInvalidStatus, true},
		{"missing workflow", ErrWorkflowNotFound, false},
		{"node missing from the draft", ErrNodeNotFound, false},
		{"unknown failure", errors.New("boom"), false},
		{"nothing", nil, false},
	}
	for _, tc := range cases {
		if got := IsValidationError(tc.err); got != tc.want {
			t.Errorf("%s: IsValidationError = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func blockingCodes(report LintReport) map[LintIssueCode]int {
	codes := map[LintIssueCode]int{}
	for _, i := range report.Blocking() {
		codes[i.Code]++
	}
	return codes
}

func TestLintReportsEveryStructuralProblemAtOnceInsteadOfTheFirst(t *testing.T) {
	g := &Graph{Nodes: []Node{
		{ID: "t", Type: NodeTypeTriggerMessageReceived, Config: map[string]interface{}{}},
		{ID: "a", Type: NodeTypeActionSendText, Config: map[string]interface{}{}},
		{ID: "b", Type: NodeTypeActionSendText, Config: map[string]interface{}{}},
	}}
	codes := blockingCodes(LintGraph(g, WorkflowTypeMessages, nil, nil, nil))

	for code, want := range map[LintIssueCode]int{LintNoEnd: 1, LintTriggerNotConnected: 1, LintNodeNoIncoming: 2, LintNodeNoOutgoing: 2} {
		if codes[code] != want {
			t.Errorf("%s reported %d times, want %d: %v", code, codes[code], want, codes)
		}
	}
	if codes[LintOrphanNode] != 0 {
		t.Errorf("a node already reported without an incoming edge must not be reported again as unreachable: %v", codes)
	}
}

func TestLintPointsTheUnconnectedTriggerProblemAtTheTrigger(t *testing.T) {
	g := &Graph{Nodes: []Node{
		{ID: "t", Type: NodeTypeTriggerMessageReceived, Config: map[string]interface{}{}},
		{ID: "end", Type: NodeTypeEnd, Config: map[string]interface{}{}},
	}}
	for _, i := range LintGraph(g, WorkflowTypeMessages, nil, nil, nil).Blocking() {
		if i.Code == LintTriggerNotConnected && i.NodeID == "t" {
			return
		}
	}
	t.Fatal("the unconnected trigger must be reported on the trigger node so the editor can select it")
}

func TestLintReportsEveryMissingRequiredFieldAcrossNodes(t *testing.T) {
	catalog := []NodeDefinition{{Type: NodeTypeActionSendText, ConfigSchema: []ConfigField{{Key: "text", Required: true}}}}
	g := &Graph{Nodes: []Node{
		{ID: "a", Type: NodeTypeActionSendText, Config: map[string]interface{}{}},
		{ID: "b", Type: NodeTypeActionSendText, Config: map[string]interface{}{"text": ""}},
	}}
	nodes := map[string]bool{}
	for _, i := range LintGraph(g, WorkflowTypeMessages, catalog, nil, nil).Blocking() {
		if i.Code == LintMissingRequiredField {
			nodes[i.NodeID] = true
		}
	}
	if !nodes["a"] || !nodes["b"] {
		t.Fatalf("missing text reported on %v, want both a and b", nodes)
	}
}

func TestATriggerWithAnIncomingEdgeIsAClassifiedProblem(t *testing.T) {
	g := &Graph{
		Nodes: []Node{
			{ID: "t", Type: NodeTypeTriggerMessageReceived, Config: map[string]interface{}{}},
			{ID: "end", Type: NodeTypeEnd, Config: map[string]interface{}{}},
		},
		Edges: []Edge{{Source: "t", Target: "end"}, {Source: "end", Target: "t"}},
	}
	if err := ValidateGraph(g, WorkflowTypeMessages); !errors.Is(err, ErrGraphTriggerHasIncoming) {
		t.Fatalf("err = %v, want ErrGraphTriggerHasIncoming", err)
	}
}

func TestNotPublishableCollectsTheLintBlockersAndTheExtraProblems(t *testing.T) {
	g := &Graph{Nodes: []Node{{ID: "t", Type: NodeTypeTriggerMessageReceived, Config: map[string]interface{}{}}}}
	report := LintGraph(g, WorkflowTypeMessages, nil, nil, nil)
	labelProblem := fmt.Errorf("%w: node %q label_id %q", ErrNodeInvalidLabelID, "n1", "l1")

	err := NotPublishableFrom(report, []error{labelProblem})
	blocked, ok := NotPublishableOf(err)
	if !ok {
		t.Fatalf("err = %v, want a NotPublishableError", err)
	}
	if len(blocked.Issues) != len(report.Blocking())+1 {
		t.Fatalf("issues = %+v, want every lint blocker plus the label problem", blocked.Issues)
	}
	last := blocked.Issues[len(blocked.Issues)-1]
	if last.Code != LintInvalidResourceRef || last.NodeID != "n1" || last.Severity != SeverityBlocking || last.Message == "" {
		t.Fatalf("label issue = %+v", last)
	}
	if !errors.Is(err, ErrGraphNoEndNode) || !errors.Is(err, ErrNodeInvalidLabelID) {
		t.Fatal("the error must still match every underlying sentinel")
	}
	if !IsValidationError(err) {
		t.Fatal("a draft that cannot be published is a validation error")
	}
}

func TestWarningsNeverBlockPublishing(t *testing.T) {
	report := LintReport{Issues: []LintIssue{{Code: LintUnknownConfigKey, Severity: SeverityAdvisory}, {Code: LintPinnedOutput, Severity: SeverityInfo}}}
	if err := NotPublishableFrom(report, nil); err != nil {
		t.Fatalf("err = %v, want warnings to let the draft publish", err)
	}
}

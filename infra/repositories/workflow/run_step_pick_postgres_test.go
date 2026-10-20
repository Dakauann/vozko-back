package workflow_repository

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/workflow"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

type pickFixture struct {
	repo          workflow.RunStepPicker
	run, otherRun string
	steps         map[string]string
}

func seedPickedSteps(t *testing.T) (pickFixture, func(string, ...interface{}) []string) {
	t.Helper()
	db := postgresSchemaTx(t, &schema.WorkflowRunLogSchema{})
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	f := pickFixture{run: uuid.NewString(), otherRun: uuid.NewString(), steps: map[string]string{}}
	output := `{"attempt": 1}`
	for i, s := range []struct {
		key, run, node, nodeType, status string
		output                           *string
	}{
		{"trigger", f.run, "t", string(workflow.NodeTypeTriggerWebhook), "executed", &output},
		{"login-1", f.run, "login", string(workflow.NodeTypeActionHTTPRequest), "executed", &output},
		{"login-2", f.run, "login", string(workflow.NodeTypeActionHTTPRequest), "skipped", &output},
		{"login-3", f.run, "login", string(workflow.NodeTypeActionHTTPRequest), "failed", nil},
		{"calc-1", f.run, "calc", string(workflow.NodeTypeActionCode), "failed", nil},
		{"other-login", f.otherRun, "login", string(workflow.NodeTypeActionHTTPRequest), "executed", &output},
		{"other-trigger", f.otherRun, "t", string(workflow.NodeTypeTriggerWebhook), "executed", &output},
	} {
		id := uuid.NewString()
		f.steps[s.key] = id
		row := schema.WorkflowRunLogSchema{ID: id, RunID: s.run, NodeID: s.node, NodeType: s.nodeType, Status: s.status, Output: s.output, ExecutedAt: at.Add(time.Duration(i) * time.Second)}
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed %s: %v", s.key, err)
		}
	}
	f.repo = NewWorkflowRunLogRepository(db).(workflow.RunStepPicker)
	explain := func(sql string, args ...interface{}) []string {
		ddl, ok := database.ConcurrentIndexSQL(database.WorkflowRunLogNodeIndex)
		if !ok {
			t.Fatalf("%s is not declared", database.WorkflowRunLogNodeIndex)
		}
		if err := db.Exec(strings.Replace(ddl, "CONCURRENTLY ", "", 1)).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("SET LOCAL enable_seqscan = off").Error; err != nil {
			t.Fatal(err)
		}
		var plan []string
		if err := db.Raw("EXPLAIN "+sql, args...).Scan(&plan).Error; err != nil {
			t.Fatal(err)
		}
		return plan
	}
	return f, explain
}

func pickedIDs(t *testing.T, f pickFixture, pick workflow.StepPick) []string {
	t.Helper()
	steps, err := f.repo.PickSteps(f.run, pick)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for _, s := range steps {
		for key, id := range f.steps {
			if id == s.ID {
				keys = append(keys, key)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

func TestAPickReadsOnlyTheStepsItNamesFromItsOwnRunOnPostgres(t *testing.T) {
	f, _ := seedPickedSteps(t)
	cases := []struct {
		name string
		pick workflow.StepPick
		want string
	}{
		{"latest usable step per node", workflow.StepPick{LatestOfNodes: []string{"login", "calc", "ghost"}}, "login-2"},
		{"a chosen step", workflow.StepPick{StepID: f.steps["login-1"]}, "login-1"},
		{"a step of another run", workflow.StepPick{StepID: f.steps["other-login"]}, ""},
		{"a step id that is not a uuid", workflow.StepPick{StepID: "nope"}, ""},
		{"the first trigger step with output", workflow.StepPick{Trigger: true}, "trigger"},
		{"everything a reproduction needs", workflow.StepPick{LatestOfNodes: []string{"login"}, Trigger: true}, "login-2,trigger"},
		{"nothing asked", workflow.StepPick{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := strings.Join(pickedIDs(t, f, tc.pick), ","); got != tc.want {
				t.Fatalf("picked = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTheLatestStepOfANodeIsReadThroughItsIndex(t *testing.T) {
	f, explain := seedPickedSteps(t)
	plan := strings.Join(explain(latestPinnableStepsSQL, `["login"]`, f.run, workflow.PinnableStepStatuses()), "\n")
	if !strings.Contains(plan, database.WorkflowRunLogNodeIndex) {
		t.Fatalf("plan does not use %s:\n%s", database.WorkflowRunLogNodeIndex, plan)
	}
}

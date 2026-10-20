package unofficial_whatsapp_group

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var joinNow = time.Date(2026, 10, 17, 15, 2, 0, 0, time.UTC)

func TestPlanJoinsSpacesNumbersTenMinutesApart(t *testing.T) {
	tasks, err := PlanJoins(JoinPlanInput{
		PlanID: "p1", WorkspaceID: "w1", InviteLink: "https://chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234",
		InstanceIDs: []string{"n1", "n2", "n3"}, Follow: true, CreatedByID: "u1",
	}, joinNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("tasks = %d, want 3", len(tasks))
	}
	for i, task := range tasks {
		want := joinNow.Add(time.Duration(i) * JoinSpacing)
		if !task.DueAt.Equal(want) {
			t.Fatalf("task %d due %v, want %v", i, task.DueAt, want)
		}
		if task.Status != JoinPending || task.Sequence != i || task.PlanID != "p1" || !task.Follow {
			t.Fatalf("task %d = %+v", i, task)
		}
	}
	if !tasks[0].Anchor() || tasks[1].Anchor() {
		t.Fatalf("only the first task anchors the plan")
	}
}

func TestPlanJoinsDropsRepeatedNumbersAndRefusesAnEmptyPlan(t *testing.T) {
	tasks, err := PlanJoins(JoinPlanInput{PlanID: "p", WorkspaceID: "w", InviteLink: "https://chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234", InstanceIDs: []string{"n1", "n1", " ", "n2"}}, joinNow)
	if err != nil || len(tasks) != 2 {
		t.Fatalf("tasks = %d, err %v; want 2", len(tasks), err)
	}
	if _, err := PlanJoins(JoinPlanInput{PlanID: "p", WorkspaceID: "w", InviteLink: "https://chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234"}, joinNow); !errors.Is(err, ErrNoNumbersSelected) {
		t.Fatalf("empty plan error = %v", err)
	}
	if _, err := PlanJoins(JoinPlanInput{PlanID: "p", WorkspaceID: "w", InviteLink: "nope", InstanceIDs: []string{"n1"}}, joinNow); !errors.Is(err, ErrInviteInvalid) {
		t.Fatalf("bad link error = %v", err)
	}
}

func TestJoinTaskRetriesThenFails(t *testing.T) {
	task := JoinTask{Status: JoinRunning}
	for i := 1; i < MaxJoinAttempts; i++ {
		task.Retry(joinNow, "provider unavailable")
		if task.Status != JoinPending || task.Attempts != i {
			t.Fatalf("attempt %d = %+v, want pending again", i, task)
		}
		if !task.DueAt.Equal(joinNow.Add(JoinRetryDelay)) {
			t.Fatalf("retry due = %v", task.DueAt)
		}
	}
	task.Retry(joinNow, "provider unavailable")
	if task.Status != JoinFailed || task.Error != "provider unavailable" {
		t.Fatalf("after max attempts = %+v, want failed", task)
	}
}

func TestJoinTaskTerminalStates(t *testing.T) {
	for _, s := range []JoinStatus{JoinDone, JoinFailed, JoinCancelled} {
		if !s.Terminal() {
			t.Fatalf("%s should be terminal", s)
		}
	}
	for _, s := range []JoinStatus{JoinPending, JoinRunning} {
		if s.Terminal() {
			t.Fatalf("%s should not be terminal", s)
		}
	}
}

func TestAJoinPlanCarriesItsCategoryToEveryTask(t *testing.T) {
	tasks, err := PlanJoins(JoinPlanInput{PlanID: "p", WorkspaceID: "w", InviteLink: "https://chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234",
		InstanceIDs: []string{"n1", "n2"}, Category: "  Clientes VIP "}, joinNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.Category != "Clientes VIP" {
			t.Fatalf("task category = %q, want the trimmed category", task.Category)
		}
	}
	if _, err := PlanJoins(JoinPlanInput{PlanID: "p", WorkspaceID: "w", InviteLink: "https://chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234",
		InstanceIDs: []string{"n1"}, Category: strings.Repeat("a", MaxCategoryRunes+1)}, joinNow); !errors.Is(err, ErrCategoryTooLong) {
		t.Fatalf("err = %v, want the category limit", err)
	}
}

func TestTheAnchorJoinSetsTheCategoryAndLaterJoinsOnlyFillAnEmptyOne(t *testing.T) {
	anchor := JoinTask{Sequence: 0, Category: "VIP"}
	later := JoinTask{Sequence: 1, Category: "VIP"}
	none := JoinTask{Sequence: 0}
	cases := []struct {
		task    JoinTask
		current string
		want    string
	}{
		{anchor, "", "VIP"},
		{anchor, "Bairro", "VIP"},
		{later, "", "VIP"},
		{later, "Bairro", "Bairro"},
		{none, "Bairro", "Bairro"},
	}
	for _, c := range cases {
		if got := c.task.CategoryFor(c.current); got != c.want {
			t.Errorf("task %+v on %q = %q, want %q", c.task, c.current, got, c.want)
		}
	}
}

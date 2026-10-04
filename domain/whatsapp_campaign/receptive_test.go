package whatsapp_campaign

import (
	"strings"
	"testing"
	"time"
)

func TestTheReceptiveSettingsAreReadFromTheContainer(t *testing.T) {
	c := &Campaign{
		Type: CampaignTypeOrganic, AgentID: "agent-1", WorkflowID: "flow-1", PipelineID: "pipe-1",
		EnableAgentResponses: true, EnableAnalysis: true, EnableAutoMemory: true,
	}
	got := c.Receptive()
	want := ReceptiveSettings{AgentID: "agent-1", WorkflowID: "flow-1", PipelineID: "pipe-1", EnableAgentResponses: true, EnableAnalysis: true, EnableAutoMemory: true}
	if got != want {
		t.Fatalf("got %+v", got)
	}
}

func TestReceptiveSettingsAreTrimmed(t *testing.T) {
	s := ReceptiveSettings{AgentID: " agent-1 ", WorkflowID: " ", PipelineID: "\tpipe\n"}
	s.Normalize()
	if s.AgentID != "agent-1" || s.WorkflowID != "" || s.PipelineID != "pipe" {
		t.Fatalf("got %+v", s)
	}
}

func TestANewReceptiveContainerStartsWithoutAutomationAndWithoutDashes(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	c := NewReceptiveContainer("ws-1", "phone-1", "+55 11 4000-1234", now)
	if !c.IsOrganic() || c.WorkspaceID != "ws-1" || c.BusinessPhoneID != "phone-1" || c.ID == "" || !c.CreatedAt.Equal(now) {
		t.Fatalf("container %+v", c)
	}
	if c.Receptive() != (ReceptiveSettings{}) {
		t.Fatalf("a new number must not answer by itself: %+v", c.Receptive())
	}
	if c.Name != "Receptivo +55 11 4000-1234" || strings.ContainsAny(c.Name, "–—") {
		t.Fatalf("name %q", c.Name)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestOnlyTheOwnersReceptiveContainerRunsAutomation(t *testing.T) {
	owned := &Campaign{Type: CampaignTypeOrganic, WorkspaceID: "owner"}
	granted := &Campaign{Type: CampaignTypeOrganic, WorkspaceID: "granted"}
	outbound := &Campaign{Type: CampaignTypeStandard, WorkspaceID: "granted"}
	cases := []struct {
		name  string
		c     *Campaign
		owner string
		want  bool
	}{
		{"owner receptive", owned, "owner", true},
		{"granted receptive", granted, "owner", false},
		{"number without owner", owned, "", false},
		{"outbound campaign of a granted workspace", outbound, "owner", true},
	}
	for _, tc := range cases {
		if got := tc.c.RunsAutomationOn(tc.owner); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}

func TestChoosingWhoAnswersSwitchesTheOtherOff(t *testing.T) {
	current := ReceptiveSettings{AgentID: "agent-1", WorkflowID: "flow-1", EnableAgentResponses: true, EnableAnalysis: true}
	cases := []struct {
		name string
		got  ReceptiveSettings
		want ReceptiveSettings
	}{
		{"workflow", current.AnsweredBy(AnsweredByWorkflow, "flow-2"), ReceptiveSettings{AgentID: "agent-1", WorkflowID: "flow-2", EnableWorkflow: true, EnableAnalysis: true}},
		{"agent", current.AnsweredBy(AnsweredByAgent, "agent-2"), ReceptiveSettings{AgentID: "agent-2", WorkflowID: "flow-1", EnableAgentResponses: true, EnableAnalysis: true}},
		{"nobody", current.AnsweredBy(AnsweredByNobody, ""), ReceptiveSettings{AgentID: "agent-1", WorkflowID: "flow-1", EnableAnalysis: true}},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: got %+v", tc.name, tc.got)
		}
	}
}

func TestWhoAnswersIsReadFromTheSwitches(t *testing.T) {
	cases := []struct {
		s    ReceptiveSettings
		want Answerer
	}{
		{ReceptiveSettings{AgentID: "a", EnableAgentResponses: true}, AnsweredByAgent},
		{ReceptiveSettings{WorkflowID: "w", EnableWorkflow: true, EnableAgentResponses: true}, AnsweredByWorkflow},
		{ReceptiveSettings{AgentID: "a"}, AnsweredByNobody},
		{ReceptiveSettings{EnableAgentResponses: true}, AnsweredByNobody},
	}
	for _, tc := range cases {
		if got := tc.s.Answerer(); got != tc.want {
			t.Errorf("%+v: got %s", tc.s, got)
		}
	}
}

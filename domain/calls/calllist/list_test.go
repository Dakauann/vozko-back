package calllist

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/lead"
)

var listNow = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func validDraft() Draft {
	return Draft{
		ID: "list-1", WorkspaceID: "ws-1", Name: "  Retorno de outubro ", CreatedBy: "manager-1",
		AssigneeIDs: []string{"worker-2", " worker-1 ", "worker-2", ""},
		Phone:       PhoneChoice{Source: PhoneIdentity},
	}
}

func TestADraftIsNormalizedBeforeItIsChecked(t *testing.T) {
	d := validDraft().Normalized()
	if d.Name != "Retorno de outubro" {
		t.Fatalf("name = %q", d.Name)
	}
	if strings.Join(d.AssigneeIDs, ",") != "worker-1,worker-2" {
		t.Fatalf("assignees = %v, want them trimmed, without blanks or repeats, sorted", d.AssigneeIDs)
	}
}

func TestADraftRefusesWhatALiveListCannotWorkWith(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Draft)
		want   error
	}{
		{"valid", func(*Draft) {}, nil},
		{"no workspace", func(d *Draft) { d.WorkspaceID = "" }, ErrWorkspaceRequired},
		{"no creator", func(d *Draft) { d.CreatedBy = "" }, ErrActorRequired},
		{"no name", func(d *Draft) { d.Name = "  " }, ErrNameRequired},
		{"a name too long", func(d *Draft) { d.Name = strings.Repeat("a", MaxNameLength+1) }, ErrNameTooLong},
		{"nobody to call", func(d *Draft) { d.AssigneeIDs = []string{" "} }, ErrAssigneesRequired},
		{"too many callers", func(d *Draft) {
			d.AssigneeIDs = nil
			for i := 0; i <= MaxAssignees; i++ {
				d.AssigneeIDs = append(d.AssigneeIDs, "worker-"+strings.Repeat("x", i+1))
			}
		}, ErrTooManyAssignees},
		{"no phone choice", func(d *Draft) { d.Phone = PhoneChoice{} }, ErrPhoneChoiceInvalid},
		{"identity with a label", func(d *Draft) { d.Phone = PhoneChoice{Source: PhoneIdentity, Label: lead.PhoneMobile} }, ErrPhoneChoiceInvalid},
		{"contact phone without a label", func(d *Draft) { d.Phone = PhoneChoice{Source: PhoneContact} }, ErrPhoneChoiceInvalid},
		{"contact phone with an unknown label", func(d *Draft) { d.Phone = PhoneChoice{Source: PhoneContact, Label: "fax"} }, ErrPhoneChoiceInvalid},
		{"contact phone with a label", func(d *Draft) { d.Phone = PhoneChoice{Source: PhoneContact, Label: lead.PhoneLandline} }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := validDraft()
			tc.change(&d)
			if err := d.Normalized().Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestANewListStartsBuilding(t *testing.T) {
	l, err := NewList(validDraft(), 120, listNow)
	if err != nil {
		t.Fatal(err)
	}
	if l.Status != StatusBuilding || l.Selected != 120 || l.Name != "Retorno de outubro" || l.CreatedBy != "manager-1" {
		t.Fatalf("list = %+v", l)
	}
}

func TestANewListRefusesAnEmptyOrOversizedSelection(t *testing.T) {
	for _, selected := range []int{0, -1, MaxItems + 1} {
		if _, err := NewList(validDraft(), selected, listNow); err == nil {
			t.Fatalf("selected %d was accepted", selected)
		}
	}
	if _, err := NewList(validDraft(), MaxItems+1, listNow); !errors.Is(err, ErrSelectionTooLarge) {
		t.Fatalf("oversized = %v", err)
	}
	if _, err := NewList(validDraft(), 0, listNow); !errors.Is(err, ErrSelectionEmpty) {
		t.Fatalf("empty = %v", err)
	}
}

func TestOnlyAnAssigneeWorksAnActiveList(t *testing.T) {
	cases := []struct {
		name   string
		status Status
		user   string
		want   error
	}{
		{"an assignee of an active list", StatusActive, "worker-1", nil},
		{"someone not assigned", StatusActive, "stranger", ErrNotAssignee},
		{"a list still building", StatusBuilding, "worker-1", ErrListBuilding},
		{"a paused list", StatusPaused, "worker-1", ErrListNotActive},
		{"an archived list", StatusArchived, "worker-1", ErrListNotActive},
		{"a failed list", StatusFailed, "worker-1", ErrListNotActive},
		{"nobody", StatusActive, "", ErrNotAssignee},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := List{Status: tc.status, AssigneeIDs: []string{"worker-1", "worker-2"}}
			if err := l.Workable(tc.user); !errors.Is(err, tc.want) {
				t.Fatalf("Workable = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestAListIsVisibleToItsAssigneesAndToWhoeverManagesLists(t *testing.T) {
	l := List{AssigneeIDs: []string{"worker-1"}}
	if !l.VisibleTo("worker-1", false) || !l.VisibleTo("manager", true) {
		t.Fatal("assignees and managers must see the list")
	}
	if l.VisibleTo("stranger", false) || l.VisibleTo("", false) {
		t.Fatal("someone neither assigned nor managing must not see the list")
	}
}

func TestAListMovesOnlyBetweenTheStatusesAManagerMayChoose(t *testing.T) {
	cases := []struct {
		from, to Status
		ok       bool
	}{
		{StatusActive, StatusPaused, true},
		{StatusPaused, StatusActive, true},
		{StatusActive, StatusArchived, true},
		{StatusPaused, StatusArchived, true},
		{StatusArchived, StatusActive, true},
		{StatusActive, StatusActive, true},
		{StatusArchived, StatusPaused, false},
		{StatusBuilding, StatusActive, false},
		{StatusFailed, StatusActive, false},
		{StatusActive, StatusBuilding, false},
		{StatusActive, StatusFailed, false},
		{StatusActive, "deleted", false},
	}
	for _, tc := range cases {
		t.Run(string(tc.from)+"->"+string(tc.to), func(t *testing.T) {
			l := List{Status: tc.from, Name: "Lista", AssigneeIDs: []string{"worker-1"}}
			to := tc.to
			err := l.Change(Change{Status: &to}, listNow)
			if tc.ok != (err == nil) {
				t.Fatalf("Change = %v, want ok %v", err, tc.ok)
			}
			if !tc.ok && !errors.Is(err, ErrStatusTransition) && !(tc.from == StatusBuilding && errors.Is(err, ErrListBuilding)) {
				t.Fatalf("refusal = %v, want ErrStatusTransition", err)
			}
			if tc.ok && (l.Status != tc.to || !l.UpdatedAt.Equal(listNow)) {
				t.Fatalf("list = %+v", l)
			}
		})
	}
}

func TestAChangeRenamesAndReassignsWithTheDraftRules(t *testing.T) {
	l := List{Status: StatusActive, Name: "Lista", AssigneeIDs: []string{"worker-1"}}
	name, assignees := " Nova ", []string{"worker-3", "worker-3", "worker-2"}
	if err := l.Change(Change{Name: &name, AssigneeIDs: &assignees}, listNow); err != nil {
		t.Fatal(err)
	}
	if l.Name != "Nova" || strings.Join(l.AssigneeIDs, ",") != "worker-2,worker-3" {
		t.Fatalf("list = %+v", l)
	}
	empty := []string{}
	if err := l.Change(Change{AssigneeIDs: &empty}, listNow); !errors.Is(err, ErrAssigneesRequired) {
		t.Fatalf("empty assignees = %v", err)
	}
	blank := ""
	if err := l.Change(Change{Name: &blank}, listNow); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("blank name = %v", err)
	}
	if err := l.Change(Change{}, listNow); !errors.Is(err, ErrNothingToChange) {
		t.Fatalf("empty change = %v", err)
	}
	building := List{Status: StatusBuilding, Name: "Lista", AssigneeIDs: []string{"worker-1"}}
	if err := building.Change(Change{Name: &name}, listNow); !errors.Is(err, ErrListBuilding) {
		t.Fatalf("renaming a list still building = %v", err)
	}
}

func phoneLead() *lead.Lead {
	return &lead.Lead{
		ID: "lead-1", WorkspaceID: "ws-1", Number: "5511987654321",
		Phones: []lead.ContactPhone{
			{Number: "551133334444", Label: lead.PhoneLandline},
			{Number: "5511912345678", Label: lead.PhoneMobile},
		},
	}
}

func TestAPhoneChoicePicksTheIdentityOrTheFirstContactPhoneOfTheLabel(t *testing.T) {
	cases := []struct {
		name   string
		choice PhoneChoice
		lead   *lead.Lead
		want   string
	}{
		{"the WhatsApp number", PhoneChoice{Source: PhoneIdentity}, phoneLead(), "5511987654321"},
		{"the landline", PhoneChoice{Source: PhoneContact, Label: lead.PhoneLandline}, phoneLead(), "551133334444"},
		{"the mobile", PhoneChoice{Source: PhoneContact, Label: lead.PhoneMobile}, phoneLead(), "5511912345678"},
		{"no work phone", PhoneChoice{Source: PhoneContact, Label: lead.PhoneWork}, phoneLead(), ""},
		{"no WhatsApp number", PhoneChoice{Source: PhoneIdentity}, &lead.Lead{ID: "lead-2"}, ""},
		{"no lead", PhoneChoice{Source: PhoneIdentity}, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.choice.Pick(tc.lead); got != tc.want {
				t.Fatalf("Pick = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAdmissionSkipsEveryLeadTheCallListRuleRefusesWithItsReason(t *testing.T) {
	optedOut := listNow.Add(-time.Hour)
	blockedOwner := &lead.Lead{ID: "owner", Number: "551133334444", Blocked: true}
	cases := []struct {
		name       string
		lead       *lead.Lead
		choice     PhoneChoice
		identities []*lead.Lead
		context    lead.DialContext
		phone      string
		skip       SkipReason
	}{
		{"callable", phoneLead(), PhoneChoice{Source: PhoneIdentity}, nil, lead.DialContext{Purpose: lead.DialCallList}, "5511987654321", ""},
		{"gone", nil, PhoneChoice{Source: PhoneIdentity}, nil, lead.DialContext{Purpose: lead.DialCallList}, "", SkipGone},
		{"no phone of the choice", phoneLead(), PhoneChoice{Source: PhoneContact, Label: lead.PhoneWork}, nil, lead.DialContext{Purpose: lead.DialCallList}, "", SkipNoNumber},
		{"blocked", func() *lead.Lead { l := phoneLead(); l.Blocked = true; return l }(), PhoneChoice{Source: PhoneIdentity}, nil, lead.DialContext{Purpose: lead.DialCallList}, "", SkipReason(lead.DialRefusedBlocked)},
		{"opted out", func() *lead.Lead { l := phoneLead(); l.OptedOutAt = &optedOut; return l }(), PhoneChoice{Source: PhoneIdentity}, nil, lead.DialContext{Purpose: lead.DialCallList}, "", SkipReason(lead.DialRefusedOptedOut)},
		{"a landline that is a blocked lead's WhatsApp", phoneLead(), PhoneChoice{Source: PhoneContact, Label: lead.PhoneLandline}, []*lead.Lead{blockedOwner}, lead.DialContext{Purpose: lead.DialCallList}, "", SkipReason(lead.DialRefusedBlocked)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			phone, skip := Admission(tc.lead, tc.choice, tc.identities, tc.context)
			if phone != tc.phone || skip != tc.skip {
				t.Fatalf("Admission = (%q, %q), want (%q, %q)", phone, skip, tc.phone, tc.skip)
			}
		})
	}
}

func TestTheDialContextOfAListIsACallListDial(t *testing.T) {
	l := List{Status: StatusActive}
	if c := l.DialContext(); c != (lead.DialContext{Purpose: lead.DialCallList}) {
		t.Fatalf("context = %+v", c)
	}
}

func TestSkipsAddUpPerReason(t *testing.T) {
	skips := Skips{}
	skips.Add(SkipGone)
	skips.Add(SkipGone)
	skips.Add(SkipNoNumber)
	skips.Add("")
	skips.Merge(Skips{SkipNoNumber: 2, SkipReason(lead.DialRefusedBlocked): 1})
	if skips[SkipGone] != 2 || skips[SkipNoNumber] != 3 || skips[SkipReason(lead.DialRefusedBlocked)] != 1 || len(skips) != 3 {
		t.Fatalf("skips = %v", skips)
	}
	if skips.Total() != 6 {
		t.Fatalf("total = %d", skips.Total())
	}
}

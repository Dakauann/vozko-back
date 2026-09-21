package inbox_assignment

import (
	"reflect"
	"testing"
	"time"
)

var refNow = time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

func ago(d time.Duration) time.Time { return refNow.Add(-d) }

func TestBuildOnlineRing_SortsByID(t *testing.T) {
	got := BuildOnlineRing([]string{"cid", "ana", "bob"})
	if want := []string{"ana", "bob", "cid"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestBuildOnlineRing_Empty(t *testing.T) {
	if got := BuildOnlineRing(nil); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestBuildLastSeenRing_OrdersByRecency(t *testing.T) {
	ring := BuildLastSeenRing([]Candidate{
		{UserID: "cid", LastSeen: ago(20 * time.Hour)},
		{UserID: "ana", LastSeen: refNow, Online: true},
		{UserID: "bob", LastSeen: ago(2 * time.Hour)},
	}, refNow, 48*time.Hour)

	if want := []string{"ana", "bob", "cid"}; !reflect.DeepEqual(ring, want) {
		t.Fatalf("got %v want %v", ring, want)
	}
}

func TestBuildLastSeenRing_DropsStaleAndNeverSeen(t *testing.T) {
	ring := BuildLastSeenRing([]Candidate{
		{UserID: "bob", LastSeen: ago(2 * time.Hour)},
		{UserID: "dan", LastSeen: ago(72 * time.Hour)},
		{UserID: "eve"},
	}, refNow, 48*time.Hour)

	if want := []string{"bob"}; !reflect.DeepEqual(ring, want) {
		t.Fatalf("got %v want %v", ring, want)
	}
}

func TestBuildLastSeenRing_WindowBoundary(t *testing.T) {
	window := 48 * time.Hour
	cases := []struct {
		name string
		age  time.Duration
		want bool
	}{
		{"one second inside", window - time.Second, true},
		{"exactly on the boundary", window, true},
		{"one second outside", window + time.Second, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ring := BuildLastSeenRing([]Candidate{{UserID: "u", LastSeen: ago(tc.age)}}, refNow, window)
			if got := len(ring) == 1; got != tc.want {
				t.Fatalf("age %s: in ring = %v, want %v", tc.age, got, tc.want)
			}
		})
	}
}

func TestBuildLastSeenRing_OnlineIsAlwaysKept(t *testing.T) {
	ring := BuildLastSeenRing([]Candidate{
		{UserID: "ghost", Online: true},
		{UserID: "old", LastSeen: ago(300 * time.Hour), Online: true},
	}, refNow, 48*time.Hour)

	if len(ring) != 2 {
		t.Fatalf("online candidates must never be filtered out, got %v", ring)
	}
}

func TestBuildLastSeenRing_TieBreakIsStable(t *testing.T) {
	same := ago(time.Hour)
	want := []string{"ana", "bob", "cid"}
	for i := 0; i < 100; i++ {
		ring := BuildLastSeenRing([]Candidate{
			{UserID: "cid", LastSeen: same},
			{UserID: "ana", LastSeen: same},
			{UserID: "bob", LastSeen: same},
		}, refNow, 48*time.Hour)
		if !reflect.DeepEqual(ring, want) {
			t.Fatalf("iteration %d: got %v want %v", i, ring, want)
		}
	}
}

func TestBuildLastSeenRing_FutureTimestampSortsToHead(t *testing.T) {
	ring := BuildLastSeenRing([]Candidate{
		{UserID: "bob", LastSeen: ago(time.Hour)},
		{UserID: "skewed", LastSeen: refNow.Add(time.Hour)},
	}, refNow, 48*time.Hour)

	if want := []string{"skewed", "bob"}; !reflect.DeepEqual(ring, want) {
		t.Fatalf("got %v want %v", ring, want)
	}
}

func TestBuildLastSeenRing_SkipsEmptyUserID(t *testing.T) {
	ring := BuildLastSeenRing([]Candidate{{UserID: "", LastSeen: refNow}}, refNow, 48*time.Hour)
	if len(ring) != 0 {
		t.Fatalf("got %v", ring)
	}
}

func TestBuildLastSeenRing_Empty(t *testing.T) {
	if ring := BuildLastSeenRing(nil, refNow, time.Hour); len(ring) != 0 {
		t.Fatalf("got %v", ring)
	}
}

func TestNextIndex_InsertionPointResume(t *testing.T) {
	ring := []string{"ana", "bob", "cid"}
	cases := []struct {
		name         string
		lastAssigned string
		want         int
	}{
		{"no pointer yet", "", 0},
		{"successor of head", "ana", 1},
		{"successor of middle", "bob", 2},
		{"wraps around at the tail", "cid", 0},
		{"departed user before everyone", "aaa", 0},
		{"departed user in the middle", "bza", 2},
		{"departed user after everyone", "zzz", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NextIndex(ring, tc.lastAssigned, ResumeAtInsertionPoint); got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}

func TestNextIndex_HeadResume(t *testing.T) {
	ring := []string{"cid", "ana", "bob"}
	cases := []struct {
		name         string
		lastAssigned string
		want         int
	}{
		{"no pointer yet", "", 0},
		{"successor of head", "cid", 1},
		{"wraps around at the tail", "bob", 0},
		{"departed user resumes at the most recently online", "gone", 0},
		{"an AI actor id is never in the ring", "ai:copilot", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NextIndex(ring, tc.lastAssigned, ResumeAtHead); got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}

func TestNextIndex_EmptyRing(t *testing.T) {
	if got := NextIndex(nil, "ana", ResumeAtHead); got != 0 {
		t.Fatalf("got %d", got)
	}
}

func TestNextIndex_SingleMemberRing(t *testing.T) {
	for _, resume := range []ResumePolicy{ResumeAtInsertionPoint, ResumeAtHead} {
		if got := NextIndex([]string{"ana"}, "ana", resume); got != 0 {
			t.Fatalf("resume %v: got %d", resume, got)
		}
	}
}

func TestNextAfter(t *testing.T) {
	ring := []string{"ana", "bob", "cid"}
	cases := []struct {
		name   string
		ring   []string
		userID string
		want   string
	}{
		{"middle", ring, "bob", "cid"},
		{"wraps around", ring, "cid", "ana"},
		{"absent user falls back to the head", ring, "gone", "ana"},
		{"ring of one holding the same user", []string{"ana"}, "ana", ""},
		{"ring of one holding someone else", []string{"bob"}, "ana", "bob"},
		{"empty ring", nil, "ana", ""},
		{"empty user", ring, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NextAfter(tc.ring, tc.userID); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

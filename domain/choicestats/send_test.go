package choicestats

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestPromptKeysNameOneSendPerSource(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"a workflow prompt is the run plus the node visit, so a retried node keeps its key", WorkflowPromptKey("run-1", "ask#4"), "wf:run-1:ask#4"},
		{"an official campaign sends once per entry", WhatsAppCampaignPromptKey("entry-1"), "wc:entry-1"},
		{"an unofficial campaign sends once per entry", UnofficialCampaignPromptKey("entry-1"), "uwc:entry-1"},
		{"a billed template send is its attempt, known before the provider answers", TemplateAttemptPromptKey("attempt-1"), "tpl:attempt-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("key = %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestShardIsStableAndBounded(t *testing.T) {
	seen := map[int]bool{}
	for _, key := range []string{"wf:a:n#1", "wf:a:n#2", "wc:e1", "uwc:e2", "tpl:x", "tpl:y", "tpl:z", "wc:e3", "wc:e4", "wc:e5"} {
		shard := ShardOf(key)
		if shard < 0 || shard >= ShardCount {
			t.Fatalf("shard of %q = %d, outside [0,%d)", key, shard, ShardCount)
		}
		if ShardOf(key) != shard {
			t.Fatalf("shard of %q moved between calls", key)
		}
		seen[shard] = true
	}
	if len(seen) < 2 {
		t.Fatalf("ten keys landed on %d shard(s), the counters would contend", len(seen))
	}
}

func TestShownOptionsKeepOnlyWhatTheChannelShowed(t *testing.T) {
	offered := []string{"a", "b", "c", "d"}
	cases := []struct {
		name     string
		reported []string
		want     []string
	}{
		{"a channel that reports nothing showed everything it was given", nil, []string{"a", "b", "c", "d"}},
		{"a channel limit drops the tail", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"the offered order wins over the reported order", []string{"c", "a"}, []string{"a", "c"}},
		{"an id the prompt never offered is not shown", []string{"a", "zzz"}, []string{"a"}},
		{"a channel that reported an empty list showed nothing", []string{}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShownOptions(offered, tc.reported); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("shown = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestASendCountsOnlyWhatTheProviderAccepted(t *testing.T) {
	at := time.Date(2026, 10, 22, 2, 30, 0, 0, time.UTC)
	saoPaulo := time.FixedZone("BRT", -3*3600)
	valid := Send{
		WorkspaceID: "ws", PromptKey: "wf:r:n#1", ProviderMessageID: "wamid.1",
		Source: Source{Kind: SourceWorkflow, ID: "wf-1", NodeID: "n", Version: 3},
		Shown:  []string{"a", "b", "a"}, SentAt: at,
	}
	cases := []struct {
		name   string
		mutate func(*Send)
		want   error
	}{
		{"an accepted prompt is a send", func(*Send) {}, nil},
		{"a prompt held back has no provider id and is not a send", func(s *Send) { s.ProviderMessageID = " " }, ErrNotASend},
		{"a prompt without options is not a send", func(s *Send) { s.Shown = nil }, ErrNotASend},
		{"a send without a workspace is refused", func(s *Send) { s.WorkspaceID = "" }, ErrNotASend},
		{"a send without a key is refused", func(s *Send) { s.PromptKey = "" }, ErrNotASend},
		{"an unknown source is refused", func(s *Send) { s.Source.Kind = "sms" }, ErrNotASend},
		{"a send without a time is refused", func(s *Send) { s.SentAt = time.Time{} }, ErrNotASend},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := valid
			s.Shown = append([]string(nil), valid.Shown...)
			tc.mutate(&s)
			_, err := s.Record(saoPaulo)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}

	rec, err := valid.Record(saoPaulo)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rec.CounterOptions, []string{OptionTotal, "a", "b"}) {
		t.Fatalf("counter options = %v, want the prompt total then each shown option once", rec.CounterOptions)
	}
	if !rec.SentDay.Equal(time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("sent day = %v, want the workspace's local day (21st in Sao Paulo at 02:30 UTC)", rec.SentDay)
	}
	if rec.Shard != ShardOf(valid.PromptKey) {
		t.Fatalf("shard = %d", rec.Shard)
	}
	if _, err := valid.Record(nil); !errors.Is(err, ErrZoneUnknown) {
		t.Fatalf("no zone = %v, want ErrZoneUnknown so nothing is recorded on a guessed day", err)
	}
}

func TestAResponseIsAClickOnlyWhenItNamesAShownOption(t *testing.T) {
	offered := []string{"sim", "nao"}
	cases := []struct {
		name     string
		selected string
		want     string
	}{
		{"a tap on an option clicks it", "sim", "sim"},
		{"typed text is free text even when it reads like a title", "", OptionFreeText},
		{"an id the prompt never offered is free text", "talvez", OptionFreeText},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResponseOption(offered, tc.selected); got != tc.want {
				t.Fatalf("response = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAClickBumpsTheOptionAndThePromptButFreeTextOnlyItself(t *testing.T) {
	cases := []struct {
		name   string
		option string
		want   []string
		err    error
	}{
		{"an option click counts for the option and as a clicked prompt", "sim", []string{"sim", OptionTotal}, nil},
		{"free text never counts as a clicked prompt", OptionFreeText, []string{OptionFreeText}, nil},
		{"a click without an option is refused", " ", nil, ErrInvalidClick},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ClickCounterOptions(tc.option)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("options = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAClickCountsOnlyInsideTheAttributionWindow(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	if got := AttributionCutoff(now); !got.Equal(now.Add(-30 * 24 * time.Hour)) {
		t.Fatalf("cutoff = %v", got)
	}
}

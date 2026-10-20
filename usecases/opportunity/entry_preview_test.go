package opportunity_usecase

import (
	"errors"
	"testing"
)

func TestAPreviewSaysWhereTheDealWouldLandWithoutWritingIt(t *testing.T) {
	cases := []struct {
		name    string
		command func() EntryCommand
		stage   string
	}{
		{"create lands on the first open stage", func() EntryCommand { return entryCommand(EntryCreate) }, "stage-new"},
		{"win lands on the won stage", func() EntryCommand { return entryCommand(EntryWin) }, "stage-won"},
		{"lose lands on the lost stage", func() EntryCommand { return entryCommand(EntryLose) }, "stage-lost"},
		{"move lands on the chosen stage", func() EntryCommand {
			cmd := entryCommand(EntryMove)
			cmd.StageID = "stage1"
			return cmd
		}, "stage1"},
		{"a value update keeps its stage", func() EntryCommand { return entryCommand(EntryUpdateValue) }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeOppRepo()
			preview, err := newAutomationService(repo).PreviewForEntry("ws1", tc.command())
			if err != nil {
				t.Fatalf("PreviewForEntry() error = %v", err)
			}
			got := ""
			if preview.Stage != nil {
				got = preview.Stage.ID
			}
			if got != tc.stage || preview.PipelineID != "pipe1" || preview.Title != "Plano Pro" || preview.ValueCents == nil || *preview.ValueCents != 7900 {
				t.Fatalf("preview = %+v, stage %q", preview, got)
			}
			if len(repo.store) != 0 || repo.locks != 0 || len(repo.links) != 0 {
				t.Fatalf("a preview wrote: %d deals, %d locks, %d links", len(repo.store), repo.locks, len(repo.links))
			}
		})
	}
}

func TestAPreviewRefusesWhatTheRealCommandWouldRefuse(t *testing.T) {
	cases := []struct {
		name    string
		command func() EntryCommand
		want    error
	}{
		{"unknown pipeline", func() EntryCommand {
			cmd := entryCommand(EntryCreate)
			cmd.PipelineID = "nope"
			return cmd
		}, ErrPipelineNotFound},
		{"a conversation pipeline", func() EntryCommand {
			cmd := entryCommand(EntryCreate)
			cmd.PipelineID = "pipe-conv"
			return cmd
		}, ErrNotOpportunityPipeline},
		{"a stage of another workspace", func() EntryCommand {
			cmd := entryCommand(EntryMove)
			cmd.StageID = "stage-foreign"
			return cmd
		}, ErrStageNotFound},
		{"a move without a stage", func() EntryCommand { return entryCommand(EntryMove) }, nil},
		{"a new deal with an id", func() EntryCommand {
			cmd := entryCommand(EntryCreateNew)
			cmd.OpportunityID = "opp1"
			return cmd
		}, ErrDealIDOnNewDeal},
		{"no actor", func() EntryCommand {
			cmd := entryCommand(EntryCreate)
			cmd.Actor = ""
			return cmd
		}, ErrActorRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newAutomationService(newFakeOppRepo()).PreviewForEntry("ws1", tc.command())
			if err == nil {
				t.Fatal("PreviewForEntry() accepted a command the real one refuses")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("PreviewForEntry() error = %v, want %v", err, tc.want)
			}
		})
	}
}

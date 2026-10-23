package emailhygiene

import (
	"context"
	"errors"
	"testing"
	"time"

	h "vozko/domain/emailhygiene"
)

func TestNewDisposableRefusesToStartWithoutAUsableSnapshot(t *testing.T) {
	if _, err := NewDisposable(DisposableDeps{Store: &fakeLists{}, Source: fakeSource{}}, "broken"); !errors.Is(err, h.ErrDisposableListInvalid) {
		t.Fatalf("err = %v, a broken snapshot must stop the boot", err)
	}
}

func TestLoadPrefersTheStoredListAndKeepsTheSnapshotWhenItIsUnusable(t *testing.T) {
	lists := &fakeLists{stored: &h.StoredList{Content: listWith("stored-only.com"), FetchedAt: clock}}
	d, err := NewDisposable(DisposableDeps{Store: lists, Source: fakeSource{}}, listWith("snapshot-only.com"))
	if err != nil {
		t.Fatal(err)
	}
	if !d.Set().Contains("snapshot-only.com") {
		t.Fatal("the snapshot is the set before anything is loaded")
	}
	if err := d.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !d.Set().Contains("stored-only.com") || d.Set().Contains("snapshot-only.com") {
		t.Fatal("the newest stored list replaces the snapshot")
	}
	broken := &fakeLists{stored: &h.StoredList{Content: "x", FetchedAt: clock}}
	d2, _ := NewDisposable(DisposableDeps{Store: broken, Source: fakeSource{}}, listWith("snapshot-only.com"))
	if err := d2.Load(context.Background()); err == nil {
		t.Fatal("an unusable stored list is reported")
	}
	if !d2.Set().Contains("snapshot-only.com") || d2.Set().Len() == 0 {
		t.Fatal("an unusable stored list never empties the set")
	}
}

func TestRefreshStoresAValidDownloadAndSwapsTheSet(t *testing.T) {
	lists := &fakeLists{}
	d, err := NewDisposable(DisposableDeps{Store: lists, Source: fakeSource{content: listWith("brand-new-temp.com")}}, listWith())
	if err != nil {
		t.Fatal(err)
	}
	d.now = func() time.Time { return clock }
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if lists.saves != 1 || lists.kept != KeepDisposableLists || !d.Set().Contains("brand-new-temp.com") {
		t.Fatalf("saves=%d kept=%d", lists.saves, lists.kept)
	}
}

func TestRefreshRefusesATruncatedOrBrokenDownloadAndKeepsTheCurrentSet(t *testing.T) {
	cases := []struct {
		name   string
		source fakeSource
	}{
		{name: "a failed download", source: fakeSource{err: errors.New("timeout")}},
		{name: "a truncated download", source: fakeSource{content: "a.com\nb.com\n"}},
		{name: "an html error page", source: fakeSource{content: "<html>rate limited</html>"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lists := &fakeLists{}
			d, err := NewDisposable(DisposableDeps{Store: lists, Source: tc.source}, listWith("kept.com"))
			if err != nil {
				t.Fatal(err)
			}
			if err := d.Refresh(context.Background()); err == nil {
				t.Fatal("a refused download is an error for the job")
			}
			if lists.saves != 0 || !d.Set().Contains("kept.com") {
				t.Fatal("the current list stays")
			}
		})
	}
}

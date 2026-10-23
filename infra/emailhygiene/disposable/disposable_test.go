package disposable

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/emailhygiene"
)

func TestTheShippedSnapshotIsAUsableList(t *testing.T) {
	set, err := emailhygiene.ParseDisposableList(Snapshot())
	if err != nil {
		t.Fatalf("the embedded snapshot must parse or the boot refuses to start: %v", err)
	}
	if set.Len() < emailhygiene.MinDisposableDomains {
		t.Fatalf("snapshot has %d domains", set.Len())
	}
	for _, d := range []string{"mailinator.com", "yopmail.com"} {
		if !set.Contains(d) {
			t.Fatalf("%s is a well known disposable provider", d)
		}
	}
	for _, d := range []string{"gmail.com", "hotmail.com", "outlook.com", "yahoo.com.br", "uol.com.br", "bol.com.br", "terra.com.br", "ig.com.br", "globo.com"} {
		if set.Contains(d) {
			t.Fatalf("%s is a real mailbox and must never be disposable", d)
		}
	}
}

func TestSourceFetchesTheListAndRefusesBadAnswers(t *testing.T) {
	var status int
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	src := NewSource(srv.Client(), srv.URL)
	status, body = http.StatusOK, "a.com\nb.com\n"
	got, err := src.Fetch(context.Background())
	if err != nil || got != body {
		t.Fatalf("got %q, %v", got, err)
	}
	status = http.StatusTooManyRequests
	if _, err := src.Fetch(context.Background()); err == nil {
		t.Fatal("an error status is never a list")
	}
	status, body = http.StatusOK, strings.Repeat("a", maxFetchBytes+1)
	if _, err := src.Fetch(context.Background()); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
}

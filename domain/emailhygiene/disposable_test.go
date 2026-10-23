package emailhygiene

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func listOf(n int, extra ...string) string {
	lines := make([]string, 0, n+len(extra))
	for i := 0; i < n; i++ {
		lines = append(lines, fmt.Sprintf("temp%05d.com", i))
	}
	return strings.Join(append(lines, extra...), "\n") + "\n"
}

func TestParseDisposableListMatchesDomainsAndParents(t *testing.T) {
	set, err := ParseDisposableList(listOf(MinDisposableDomains, "mailinator.com", "Yopmail.COM"))
	if err != nil {
		t.Fatalf("ParseDisposableList: %v", err)
	}
	cases := []struct {
		domain string
		want   bool
	}{
		{domain: "mailinator.com", want: true},
		{domain: "inbox.mailinator.com", want: true},
		{domain: "yopmail.com", want: true},
		{domain: "MAILINATOR.COM", want: true},
		{domain: "gmail.com", want: false},
		{domain: "com", want: false},
		{domain: "notmailinator.com", want: false},
		{domain: "", want: false},
	}
	for _, tc := range cases {
		if got := set.Contains(tc.domain); got != tc.want {
			t.Fatalf("Contains(%q) = %v, want %v", tc.domain, got, tc.want)
		}
	}
	if set.Len() != MinDisposableDomains+2 {
		t.Fatalf("Len = %d", set.Len())
	}
}

func TestParseDisposableListRefusesListsThatCannotBeTrusted(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{name: "an empty list would let every disposable address through", content: ""},
		{name: "a truncated download is refused", content: listOf(MinDisposableDomains - 1)},
		{name: "a line that is not a domain means the file is not the list", content: listOf(MinDisposableDomains, "<html>")},
		{name: "a single label is not a domain", content: listOf(MinDisposableDomains, "localhost")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseDisposableList(tc.content); !errors.Is(err, ErrDisposableListInvalid) {
				t.Fatalf("err = %v, want ErrDisposableListInvalid", err)
			}
		})
	}
}

func TestParseDisposableListSkipsBlankLinesAndComments(t *testing.T) {
	set, err := ParseDisposableList("# header\n\n" + listOf(MinDisposableDomains, "  trash-mail.com  "))
	if err != nil {
		t.Fatalf("ParseDisposableList: %v", err)
	}
	if !set.Contains("trash-mail.com") {
		t.Fatal("surrounding spaces are not part of the domain")
	}
}

func TestNilDisposableSetContainsNothingButIsNeverUsedEmpty(t *testing.T) {
	var set *DisposableSet
	if set.Contains("mailinator.com") || set.Len() != 0 {
		t.Fatal("a nil set answers nothing")
	}
}

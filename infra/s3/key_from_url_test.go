package s3

import "testing"

func TestKeyFromURLOnlyAcceptsOurOwnStorage(t *testing.T) {
	const endpoint = "https://files.vozko.test"
	cases := map[string]bool{
		"https://files.vozko.test/ws1/a.csv":       true,
		"https://evil.test/ws1/a.csv":              false,
		"https://files.vozko.test.evil.test/a.csv": false,
		"https://files.vozko.test/../etc/passwd":   false,
		"https://files.vozko.test/a.csv?x=1":       false,
		"https://files.vozko.test/":                false,
		"http://169.254.169.254/latest/meta-data/": false,
	}
	for url, want := range cases {
		if _, ok := keyFromURL(endpoint, url); ok != want {
			t.Errorf("keyFromURL(%q) ok = %v, want %v", url, ok, want)
		}
	}
	if key, _ := keyFromURL(endpoint+"/", "https://files.vozko.test/ws1/a.csv"); key != "ws1/a.csv" {
		t.Errorf("key = %q", key)
	}
	if _, ok := keyFromURL("", "https://files.vozko.test/a"); ok {
		t.Error("an unset endpoint must refuse")
	}
}

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

func TestOwnsURLCoversOurCDNAndOurBucketHost(t *testing.T) {
	const endpoint, account = "https://files.vozko.test", "acc123"
	cases := map[string]bool{
		"https://files.vozko.test/ws1/a.png":                   true,
		"https://FILES.vozko.test/ws1/a.png?x=1":               true,
		"http://files.vozko.test/a.png":                        true,
		"https://acc123.r2.cloudflarestorage.com/bucket/a.png": true,
		"https://bucket.acc123.r2.cloudflarestorage.com/a.png": true,
		"https://files.vozko.test.evil.test/a.png":             false,
		"https://other.r2.cloudflarestorage.com/bucket/a.png":  false,
		"https://images.example.com/a.png":                     false,
		"https://evil.test/https://files.vozko.test/ws1/a.png": false,
		"::not a url": true,
	}
	for url, want := range cases {
		if got := ownsURL(endpoint, account, url); got != want {
			t.Errorf("ownsURL(%q) = %v, want %v", url, got, want)
		}
	}
	if ownsURL("", "", "https://images.example.com/a.png") {
		t.Error("with no storage configured nothing is ours")
	}
}

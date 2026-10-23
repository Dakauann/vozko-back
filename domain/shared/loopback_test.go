package shared

import "testing"

func TestIsLoopbackHost(t *testing.T) {
	cases := map[string]bool{
		"localhost":         true,
		"LOCALHOST":         true,
		"api.localhost":     true,
		"127.0.0.1":         true,
		"127.8.9.1":         true,
		"::1":               true,
		"localhost.evil.io": false,
		"api.example.com":   false,
		"10.0.0.1":          false,
		"":                  false,
	}
	for host, want := range cases {
		if got := IsLoopbackHost(host); got != want {
			t.Errorf("IsLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

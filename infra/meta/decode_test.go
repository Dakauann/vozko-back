package meta

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestGraphIDAcceptsStringsAndNumbers(t *testing.T) {
	cases := map[string]string{
		`"17841405822304914"`: "17841405822304914",
		`17841405822304914`:   "17841405822304914",
		`null`:                "",
		`" 42 "`:              "42",
	}
	for raw, want := range cases {
		var id GraphID
		if err := json.Unmarshal([]byte(raw), &id); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if id.String() != want {
			t.Fatalf("%s: got %q want %q", raw, id, want)
		}
	}
}

func TestPermissionListAcceptsArrayAndCSV(t *testing.T) {
	cases := map[string][]string{
		`["a","b","a"," c "]`: {"a", "b", "c"},
		`"a,b, c"`:            {"a", "b", "c"},
		`null`:                nil,
	}
	for raw, want := range cases {
		var p PermissionList
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if !reflect.DeepEqual(p.Strings(), want) {
			t.Fatalf("%s: got %v want %v", raw, p.Strings(), want)
		}
	}
}

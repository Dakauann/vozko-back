package copilot

import (
	"reflect"
	"testing"
)

func TestDescribeArgsShowsPlainValuesInAStableOrder(t *testing.T) {
	got := DescribeArgs(map[string]interface{}{
		"name":     "Cobrança",
		"isActive": true,
		"tags":     []interface{}{"vip", "sp"},
		"limit":    float64(5),
	})
	want := []Field{
		{Key: "isActive", Value: "true"},
		{Key: "limit", Value: "5"},
		{Key: "name", Value: "Cobrança"},
		{Key: "tags", Value: "vip, sp"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DescribeArgs = %+v, want %+v", got, want)
	}
}

func TestDescribeArgsNeverShowsInternalIdentifiers(t *testing.T) {
	got := DescribeArgs(map[string]interface{}{
		"id":            "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f",
		"stage_id":      "anything",
		"departmentIds": []interface{}{"a", "b"},
		"entryId":       "x",
		"note":          "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f",
		"text":          "Olá",
	})
	// A person approving a change cannot judge a UUID; tools that need to show one resolve it to a name.
	want := []Field{{Key: "text", Value: "Olá"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DescribeArgs = %+v, want %+v", got, want)
	}
}

func TestDescribeArgsSkipsStructuresAndEmptyValues(t *testing.T) {
	got := DescribeArgs(map[string]interface{}{
		"config": map[string]interface{}{"url": "x"},
		"empty":  "  ",
		"none":   nil,
		"list":   []interface{}{},
		"long":   string(make([]byte, 0)),
	})
	if len(got) != 0 {
		t.Fatalf("DescribeArgs = %+v, want nothing", got)
	}
}

func TestDescribeArgsTrimsLongValues(t *testing.T) {
	long := ""
	for i := 0; i < 400; i++ {
		long += "a"
	}
	got := DescribeArgs(map[string]interface{}{"prompt": long})
	if n := len([]rune(got[0].Value)); n != MaxFieldRunes+1 {
		t.Fatalf("value runes = %d, want %d plus the ellipsis", n, MaxFieldRunes)
	}
}

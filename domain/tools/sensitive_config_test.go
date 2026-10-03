package tools

import (
	"reflect"
	"testing"
)

func TestADefinitionNamesItsSensitiveConfigInOrder(t *testing.T) {
	d := Definition{ConfigSchema: map[string]ConfigParameter{
		"url":     {Type: "string"},
		"headers": {Type: "object", Sensitive: true},
		"api_key": {Type: "string", Sensitive: true},
	}}
	if got := d.SensitiveConfig(); !reflect.DeepEqual(got, []string{"api_key", "headers"}) {
		t.Fatalf("sensitive = %v", got)
	}
	if (Definition{}).SensitiveConfig() != nil {
		t.Fatal("a tool without config has nothing sensitive")
	}
}

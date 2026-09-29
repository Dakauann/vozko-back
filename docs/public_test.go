package docs

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// These artifacts are uploaded to the customer-facing API reference.
func TestSwaggerIsPublic(t *testing.T) {
	file, err := os.ReadFile("swagger.json")
	if err != nil {
		t.Fatal(err)
	}
	yamlFile, err := os.ReadFile("swagger.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string]struct {
		raw    []byte
		decode func([]byte, interface{}) error
	}{
		"swagger.json":     {file, json.Unmarshal},
		"swagger.yaml":     {yamlFile, yaml.Unmarshal},
		"embedded Swagger": {[]byte(SwaggerInfo.ReadDoc()), json.Unmarshal},
	} {
		t.Run(name, func(t *testing.T) {
			var spec struct {
				Paths map[string]map[string]struct {
					Tags []string `json:"tags"`
				} `json:"paths"`
				Definitions map[string]interface{} `json:"definitions"`
			}
			if err := input.decode(input.raw, &spec); err != nil {
				t.Fatal(err)
			}
			for path, methods := range spec.Paths {
				if path == "/admin" || strings.HasPrefix(path, "/admin/") {
					t.Errorf("system-admin path exposed: %s", path)
				}
				for method, operation := range methods {
					for _, tag := range operation.Tags {
						if strings.Contains(strings.ToLower(tag), "admin") {
							t.Errorf("admin tag exposed: %s %s (%s)", method, path, tag)
						}
					}
				}
			}
			for name := range spec.Definitions {
				if strings.Contains(name, "SendCap") || name == "livedecision.SummaryResponse" || strings.Contains(name, ".Admin") {
					t.Errorf("internal schema exposed: %s", name)
				}
			}
			// Keep customer operations, including legitimate workspace administration.
			for path, method := range map[string]string{
				"/auth/login": "post",
				"/workspaces": "get",
				"/workspaces/{workspaceId}/members/{userId}/role": "put",
				"/whatsapp/templates":                             "post",
				"/tickets":                                        "get",
			} {
				if _, ok := spec.Paths[path][method]; !ok {
					t.Errorf("customer operation missing: %s %s", method, path)
				}
			}
		})
	}
}

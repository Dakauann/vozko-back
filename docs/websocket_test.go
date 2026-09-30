package docs

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

var neverSentOnASocket = map[string]string{
	"conversation:media_ready": "declared in delivery/ws/types.go but never emitted",
	"conversation:unread":      "BroadcastUnreadCount has no callers",
	"conversation:error":       "documented as the error envelope of both sockets",
}

func socketDescriptions(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile("swagger.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths map[string]map[string]struct {
			Description string `json:"description"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for path, methods := range spec.Paths {
		if strings.HasPrefix(path, "/ws/") {
			out[path] = methods["get"].Description
		}
	}
	return out
}

func messageNames(t *testing.T, pattern string, files ...string) []string {
	t.Helper()
	expr := regexp.MustCompile(pattern)
	seen := map[string]bool{}
	var names []string
	for _, file := range files {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range expr.FindAllStringSubmatch(string(source), -1) {
			if name := match[1]; !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		t.Fatalf("no message names found in %v; the pattern no longer matches the code", files)
	}
	return names
}

func requireDocumented(t *testing.T, description string, names []string) {
	t.Helper()
	for _, name := range names {
		if _, excused := neverSentOnASocket[name]; excused {
			continue
		}
		if !strings.Contains(description, "`"+name+"`") {
			t.Errorf("socket message %q is not in the Swagger description", name)
		}
	}
}

func TestEverySocketMessageIsInTheSwaggerDescriptions(t *testing.T) {
	docs := socketDescriptions(t)
	for _, path := range []string{"/ws/conversations", "/ws/call-session", "/ws/workflows/{id}/simulate", "/ws/workflows/{id}/ai-builder", "/ws/workflows/ai-builder"} {
		if docs[path] == "" {
			t.Fatalf("%s is missing from Swagger", path)
		}
	}

	t.Run("conversations and calls", func(t *testing.T) {
		names := messageNames(t, `WSEventType = "([a-z_:-]+)"`, "../delivery/ws/types.go")
		names = append(names, messageNames(t, `= "(call:[a-z_]+)"`, "../domain/callsession/inbound.go", "../domain/callsession/transfer_status.go")...)
		requireDocumented(t, docs["/ws/conversations"]+docs["/ws/call-session"], names)
	})

	t.Run("workflow simulator", func(t *testing.T) {
		files := []string{"../usecases/workflow/ws_workflow_simulation.go", "../usecases/workflow/sim_voice_session.go"}
		names := messageNames(t, `(?:send\(|Type: |case )"([a-z_]+)"`, files...)
		requireDocumented(t, docs["/ws/workflows/{id}/simulate"], names)
	})

	t.Run("workflow ai builder", func(t *testing.T) {
		names := messageNames(t, `(?:emit\(|aiBuilderServerMsg\{Type: )"([a-z_]+)"`, "../usecases/workflow/ws_workflow_ai_builder.go")
		names = append(names, messageNames(t, `Event[A-Za-z]+ += "([a-z_]+)"`, "../usecases/agentloop/agentloop.go")...)
		for _, path := range []string{"/ws/workflows/{id}/ai-builder", "/ws/workflows/ai-builder"} {
			requireDocumented(t, docs[path], names)
		}
	})
}

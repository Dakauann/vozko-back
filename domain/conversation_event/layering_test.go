package conversation_event_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Timeline events belong to the use-case layer, never to delivery.
//
// They used to be written by the stage and label HTTP handlers, and only by
// them. The CRM's bulk action, the AI's manage_entry_stage tool and the HTTP
// send-template endpoint all reach the same use cases directly, so a bulk move
// changed the board and left every affected conversation's history blank — the
// change was real, the record of it did not exist.
//
// A handler is one caller of a use case. Anything the handler does after the
// mutation, that mutation's OTHER callers do not do. That is the whole bug, and
// it is invisible in review: the handler reads correctly, and the missing write
// is somewhere else entirely.
//
// Broadcasting is deliberately exempt: telling connected clients is the
// transport's job, which is why the operator-send port documents it as NOT one
// of the finalizer's side effects.
func TestTimelineEventsAreNotWrittenFromTheDeliveryLayer(t *testing.T) {
	// The WebSocket hub keeps two assignment writes for the degraded path taken
	// when no assignment service is wired (unreachable in production; the
	// container always wires one). They are grandfathered rather than silently
	// allowed: the file is listed, so a NEW delivery-layer event write anywhere
	// else still fails this test.
	grandfathered := map[string]bool{
		filepath.Join("delivery", "ws", "hub.go"): true,
	}

	root := filepath.Join("..", "..")
	var offenders []string

	err := filepath.Walk(filepath.Join(root, "delivery"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if grandfathered[rel] {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for i, line := range strings.Split(string(body), "\n") {
			if strings.Contains(line, "eventLogger.Log(") || strings.Contains(line, "events.Log(") {
				offenders = append(offenders, rel+":"+itoa(i+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking delivery: %v", err)
	}

	if len(offenders) > 0 {
		t.Fatalf(
			"conversation events written from the delivery layer:\n  %s\n\n"+
				"Move the write into the use case the handler calls, and pass the actor on\n"+
				"its input. Otherwise every OTHER caller of that use case — bulk actions,\n"+
				"AI tools, schedulers — mutates without leaving a trace.",
			strings.Join(offenders, "\n  "),
		)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

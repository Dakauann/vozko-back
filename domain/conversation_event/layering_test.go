package conversation_event_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTimelineEventsAreNotWrittenFromTheDeliveryLayer(t *testing.T) {
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

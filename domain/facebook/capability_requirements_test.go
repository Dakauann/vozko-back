package facebook

import "testing"

func TestCapabilityRequirementsDescribeWhatCanChecks(t *testing.T) {
	for _, c := range AllCapabilities() {
		t.Run(string(c), func(t *testing.T) {
			scopes, tasks, ok := CapabilityRequirements(c)
			if !ok || len(scopes) == 0 || len(tasks) == 0 {
				t.Fatalf("requirements of %s = %v %v %v", c, scopes, tasks, ok)
			}
			for _, task := range tasks {
				page := &Page{Status: StatusConnected, GrantedScopes: scopes, Tasks: []Task{task}}
				if !page.Can(c) {
					t.Fatalf("a page with the scopes and task %s cannot %s", task, c)
				}
			}
			short := &Page{Status: StatusConnected, GrantedScopes: scopes[1:], Tasks: tasks}
			if short.Can(c) {
				t.Fatalf("a page missing scope %s can still %s", scopes[0], c)
			}
			scopes[0] = "tampered"
			if again, _, _ := CapabilityRequirements(c); again[0] == "tampered" {
				t.Fatal("callers can change the shared rule")
			}
		})
	}
	if _, _, ok := CapabilityRequirements(Capability("unknown")); ok {
		t.Fatal("an unknown capability has requirements")
	}
}

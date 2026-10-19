package codexnative

import "testing"

func TestNewThreadPermissionPolicy(t *testing.T) {
	normal := newThreadParams("/project", false)
	if _, ok := normal["approvalPolicy"]; ok {
		t.Fatal("default policy overridden")
	}
	if _, ok := normal["sandbox"]; ok {
		t.Fatal("default sandbox overridden")
	}
	automatic := newThreadParams("/selected/project", true)
	if automatic["approvalPolicy"] != "never" || automatic["sandbox"] != "danger-full-access" || automatic["cwd"] != "/selected/project" {
		t.Fatalf("wrong create parameters: %v", automatic)
	}
}

func TestTurnPermissionPolicy(t *testing.T) {
	params := map[string]any{"threadId": "existing", "input": "keep"}
	applyTurnPermissionPolicy(params, false)
	if _, ok := params["approvalPolicy"]; ok {
		t.Fatal("default changed")
	}
	applyTurnPermissionPolicy(params, true)
	if params["approvalPolicy"] != "never" || params["sandboxPolicy"].(map[string]any)["type"] != "dangerFullAccess" || params["threadId"] != "existing" || params["input"] != "keep" {
		t.Fatal(params)
	}
}

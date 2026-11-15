package mcp

import (
	"context"
	"testing"
)

func TestTrashedProjectHiddenFromMCP(t *testing.T) {
	e := newEnv(t)
	cs := e.connect(t, e.admin)
	ok(t, cs, "create_project", map[string]any{"name": "Web", "identifier": "WEB"})
	ok(t, cs, "create_project", map[string]any{"name": "Api", "identifier": "API"})
	ok(t, cs, "create_work_item", map[string]any{"project": "WEB", "name": "Fix login"})
	ok(t, cs, "get_work_item", map[string]any{"item": "WEB-1"})

	p, err := e.s.ProjectByRef(context.Background(), e.ws.ID, "WEB")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.s.DeleteProject(context.Background(), e.ws.ID, p.ID); err != nil {
		t.Fatal(err)
	}

	list := ok(t, cs, "list_projects", nil).([]any)
	if len(list) != 1 || obj(list[0])["identifier"] != "API" {
		t.Fatalf("list_projects = %v", list)
	}
	fails(t, cs, "get_work_item", map[string]any{"item": "WEB-1"}, "not found")
	fails(t, cs, "list_work_items", map[string]any{"project": "WEB"}, "unknown project", "API")
	fails(t, cs, "create_work_item", map[string]any{"project": "web", "name": "nope"}, "unknown project")
	fails(t, cs, "list_states", map[string]any{"project": p.ID}, "unknown project")
	fails(t, cs, "update_work_item", map[string]any{"item": "WEB-1", "name": "nope"}, "not found")
}

package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"mostlydone/internal/store"
)

func searchHits(c *client) int {
	return len(c.must(200, "GET", W+"/work-items/search/?search=Findable", nil)["issues"].([]any))
}

func TestProjectTrash(t *testing.T) {
	h, s := newHandler(t)
	ctx := context.Background()
	adm := &client{t: t, h: h, csrf: true}
	adm.must(201, "POST", "/auth/setup", map[string]any{"email": "ada@example.com", "password": "password123", "display_name": "Ada"})
	ws, err := s.Workspace(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser(ctx, ws.ID, "bob@example.com", "password123", "Bob", store.RoleMember); err != nil {
		t.Fatal(err)
	}
	bob := &client{t: t, h: h, csrf: true}
	bob.must(200, "POST", "/auth/login", map[string]any{"email": "bob@example.com", "password": "password123"})

	p := adm.must(201, "POST", W+"/projects/", map[string]any{"name": "Web", "identifier": "WEB"})
	pid := p["id"].(string)
	PJ := W + "/projects/" + pid
	eq(t, "live deleted_at", p["deleted_at"], nil)
	item := adm.must(201, "POST", PJ+"/work-items/", map[string]any{"name": "Findable thing"})
	iid := item["id"].(string)
	eq(t, "search before", searchHits(adm), 1)
	adm.must(200, "GET", W+"/work-items/WEB-1/", nil)

	// Non-admins cannot touch the trash, nor delete the project.
	bob.must(403, "DELETE", PJ+"/", nil)
	bob.must(403, "GET", W+"/trashed-projects/", nil)
	bob.must(403, "POST", W+"/trashed-projects/"+pid+"/restore/", nil)
	bob.must(403, "DELETE", W+"/trashed-projects/"+pid+"/", nil)

	// Live projects are not purgeable or restorable.
	adm.must(404, "DELETE", W+"/trashed-projects/"+pid+"/", nil)
	adm.must(404, "POST", W+"/trashed-projects/"+pid+"/restore/", nil)

	adm.must(204, "DELETE", PJ+"/", nil)
	adm.must(404, "DELETE", PJ+"/", nil)

	// Hidden everywhere.
	adm.must(404, "GET", PJ+"/", nil)
	eq(t, "projects list", len(adm.list("GET", W+"/projects/", nil)), 0)
	adm.must(404, "GET", PJ+"/work-items/", nil)
	adm.must(404, "GET", PJ+"/work-items/"+iid+"/", nil)
	adm.must(404, "POST", PJ+"/work-items/", map[string]any{"name": "nope"})
	adm.must(404, "PATCH", PJ+"/work-items/"+iid+"/", map[string]any{"name": "nope"})
	adm.must(404, "DELETE", PJ+"/work-items/"+iid+"/", nil)
	adm.must(404, "GET", PJ+"/issues/"+iid+"/", nil)
	adm.must(404, "POST", PJ+"/work-items/"+iid+"/comments/", map[string]any{"comment_html": "<p>x</p>"})
	adm.must(404, "GET", PJ+"/states/", nil)
	adm.must(404, "PATCH", PJ+"/", map[string]any{"description": "x"})
	adm.must(404, "GET", W+"/work-items/WEB-1/", nil)
	adm.must(404, "GET", W+"/issues/WEB-1/", nil)
	eq(t, "search after", searchHits(adm), 0)

	// Identifier and name stay reserved, with a clear message.
	c := adm.must(409, "POST", W+"/projects/", map[string]any{"name": "Other", "identifier": "WEB"})
	if msg, _ := c["identifier"].(string); !strings.Contains(msg, "in the trash; restore it or delete it permanently") {
		t.Fatalf("identifier conflict = %v", c)
	}
	c = adm.must(409, "POST", W+"/projects/", map[string]any{"name": "Web", "identifier": "OTH"})
	if msg, _ := c["name"].(string); !strings.Contains(msg, "in the trash") {
		t.Fatalf("name conflict = %v", c)
	}

	// Trash list: real deleted_at and purge_at = deleted_at + 30 days.
	tr := adm.list("GET", W+"/trashed-projects/", nil)
	eq(t, "trashed count", len(tr), 1)
	row := tr[0].(map[string]any)
	eq(t, "trashed id", row["id"], pid)
	del, err := time.Parse(store.TimeLayout, row["deleted_at"].(string))
	if err != nil {
		t.Fatalf("deleted_at = %v", row["deleted_at"])
	}
	purge, err := time.Parse(store.TimeLayout, row["purge_at"].(string))
	if err != nil {
		t.Fatalf("purge_at = %v", row["purge_at"])
	}
	if purge.Sub(del) != 30*24*time.Hour {
		t.Fatalf("purge_at - deleted_at = %v", purge.Sub(del))
	}
	if time.Since(del) > time.Minute || time.Since(del) < -time.Minute {
		t.Fatalf("deleted_at not now: %v", del)
	}
	eq(t, "identifier", row["identifier"], "WEB")

	// Restore.
	r := adm.must(200, "POST", W+"/trashed-projects/"+pid+"/restore/", nil)
	eq(t, "restored deleted_at", r["deleted_at"], nil)
	eq(t, "trash empty", len(adm.list("GET", W+"/trashed-projects/", nil)), 0)
	adm.must(200, "GET", PJ+"/", nil)
	eq(t, "projects list", len(adm.list("GET", W+"/projects/", nil)), 1)
	adm.must(200, "GET", PJ+"/work-items/"+iid+"/", nil)
	adm.must(200, "GET", W+"/work-items/WEB-1/", nil)
	eq(t, "search restored", searchHits(adm), 1)
	adm.must(404, "POST", W+"/trashed-projects/"+pid+"/restore/", nil)

	// Permanent delete.
	adm.must(204, "DELETE", PJ+"/", nil)
	adm.must(204, "DELETE", W+"/trashed-projects/"+pid+"/", nil)
	adm.must(404, "POST", W+"/trashed-projects/"+pid+"/restore/", nil)
	adm.must(404, "DELETE", W+"/trashed-projects/"+pid+"/", nil)
	eq(t, "trash empty", len(adm.list("GET", W+"/trashed-projects/", nil)), 0)
	adm.must(201, "POST", W+"/projects/", map[string]any{"name": "Web", "identifier": "WEB"})
}

package api

import "testing"

func TestAccountLifecycle(t *testing.T) {
	h, browser, api := boot(t)
	const adminMsg = "The workspace needs at least one active admin."
	pw := func(cur, next string) map[string]any {
		return map[string]any{"current_password": cur, "new_password": next}
	}
	login := func(c *client, email, password string, status int) {
		t.Helper()
		c.must(status, "POST", "/auth/login", map[string]any{"email": email, "password": password})
	}

	// Password change: session only, CSRF, field errors, 204.
	api.must(403, "POST", "/auth/password", pw("password123", "newpassword1"))
	browser.csrf = false
	browser.must(403, "POST", "/auth/password", pw("password123", "newpassword1"))
	browser.csrf = true
	bad := browser.must(400, "POST", "/auth/password", pw("nope-nope", "newpassword1"))
	eq(t, "wrong current", len(bad["current_password"].([]any)), 1)
	short := browser.must(400, "POST", "/auth/password", pw("password123", "short"))
	eq(t, "short new", short["password"].([]any)[0], "Password must be at least 8 characters.")

	other := &client{t: t, h: h, csrf: true}
	login(other, "ada@example.com", "password123", 200)
	other.must(200, "GET", "/auth/tokens", nil)
	browser.must(204, "POST", "/auth/password", pw("password123", "newpassword1"))
	browser.must(200, "GET", "/auth/tokens", nil)
	other.must(401, "GET", "/auth/tokens", nil)
	api.must(200, "GET", W+"/projects/", nil)
	anon := &client{t: t, h: h, csrf: true}
	login(anon, "ada@example.com", "password123", 401)
	login(anon, "ada@example.com", "newpassword1", 200)

	// User management.
	gus := browser.must(201, "POST", "/auth/users", map[string]any{"email": "gus@example.com", "password": "password123", "role": 15})
	gusID := gus["id"].(string)
	gusC := &client{t: t, h: h, csrf: true}
	login(gusC, "gus@example.com", "password123", 200)
	gusC.must(403, "GET", "/auth/users", nil)
	gusC.must(403, "PATCH", "/auth/users/"+gusID, map[string]any{"role": 20})

	users := browser.list("GET", "/auth/users", nil)
	eq(t, "user count", len(users), 2)
	ada := users[0].(map[string]any) // ordered by display name: "Ada L" before "gus"
	eq(t, "first user", ada["email"], "ada@example.com")
	eq(t, "list keys", len(ada), 5)
	eq(t, "first is active", ada["is_active"], true)
	adaID := ada["id"].(string)

	browser.csrf = false
	browser.must(403, "PATCH", "/auth/users/"+gusID, map[string]any{"role": 5})
	browser.csrf = true
	browser.must(404, "PATCH", "/auth/users/no-such-id", map[string]any{"role": 5})
	badRole := browser.must(400, "PATCH", "/auth/users/"+gusID, map[string]any{"role": 7})
	eq(t, "bad role", badRole["role"].([]any)[0], "Role must be one of 5, 15, 20.")
	got := browser.must(200, "PATCH", "/auth/users/"+gusID, map[string]any{"role": 5})
	eq(t, "role", got["role"], 5)
	eq(t, "is_active", got["is_active"], true)
	eq(t, "email", got["email"], "gus@example.com")

	got = browser.must(200, "PATCH", "/auth/users/"+gusID, map[string]any{"is_active": false})
	eq(t, "deactivated", got["is_active"], false)
	gusC.must(401, "GET", "/auth/tokens", nil)
	login(gusC, "gus@example.com", "password123", 401)
	eq(t, "deactivated listed", browser.list("GET", "/auth/users", nil)[1].(map[string]any)["is_active"], false)
	browser.must(200, "PATCH", "/auth/users/"+gusID, map[string]any{"is_active": true})
	login(gusC, "gus@example.com", "password123", 200)

	// Last-admin guard, including self.
	eq(t, "self demote", browser.must(400, "PATCH", "/auth/users/"+adaID, map[string]any{"role": 15})["error"], adminMsg)
	eq(t, "self deactivate", browser.must(400, "PATCH", "/auth/users/"+adaID, map[string]any{"is_active": false})["error"], adminMsg)
	browser.must(200, "GET", "/auth/tokens", nil)
}

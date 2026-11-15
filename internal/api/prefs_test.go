package api

import "testing"

func TestPrefs(t *testing.T) {
	h, browser, api := boot(t)
	anon := &client{t: t, h: h}
	eq(t, "logged out prefs", anon.must(200, "GET", "/auth/me", nil)["prefs"], nil)
	eq(t, "logged in prefs", browser.must(200, "GET", "/auth/me", nil)["prefs"], "map[density:comfortable theme:system]")

	eq(t, "patch", browser.must(200, "PATCH", "/auth/me/prefs", map[string]any{"theme": "dark"}), "map[density:comfortable theme:dark]")
	eq(t, "me after patch", browser.must(200, "GET", "/auth/me", nil)["prefs"], "map[density:comfortable theme:dark]")
	eq(t, "empty patch", browser.must(200, "PATCH", "/auth/me/prefs", map[string]any{}), "map[density:comfortable theme:dark]")

	browser.csrf = false
	browser.must(403, "PATCH", "/auth/me/prefs", map[string]any{"theme": "light"})
	browser.csrf = true
	eq(t, "refused patch saved nothing", browser.must(200, "GET", "/auth/me", nil)["prefs"], "map[density:comfortable theme:dark]")

	eq(t, "token patch", api.must(200, "PATCH", "/auth/me/prefs", map[string]any{"density": "compact"}), "map[density:compact theme:dark]")

	eq(t, "unknown key", browser.must(400, "PATCH", "/auth/me/prefs", map[string]any{"font": "mono"}), "map[font:[Unknown preference.]]")
	eq(t, "bad value", browser.must(400, "PATCH", "/auth/me/prefs", map[string]any{"theme": "neon"}), "map[theme:[\"neon\" is not a valid choice.]]")
	browser.must(400, "PATCH", "/auth/me/prefs", map[string]any{"theme": 3})
	browser.must(400, "PATCH", "/auth/me/prefs", "[]")

	anon.csrf = true
	anon.must(401, "PATCH", "/auth/me/prefs", map[string]any{"theme": "dark"})

	if _, ok := api.must(200, "GET", "/api/v1/users/me/", nil)["prefs"]; ok {
		t.Fatal("/api/v1/users/me/ must not include prefs")
	}
}

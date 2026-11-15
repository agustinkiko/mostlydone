# MostlyDone — spec

A small, self-hosted project tracker modeled on Plane's core features.
One Go binary, one SQLite file, web UI embedded. New code only: Plane is
AGPL-3.0, so nothing is copied from its source. Wire shapes follow Plane's
public v1 API so existing Plane clients and scripts keep working.

## Decisions (settled)

| Topic | Decision |
| --- | --- |
| Scope | Core tracker: projects, work items, states, priorities, labels, cycles, modules, comments, activity, list + board views, REST API, MCP |
| Out of scope | Pages, intake, analytics, saved views, estimates, notifications, attachments, webhooks, public sharing, admin panel |
| Backend | Go, standard `net/http` (Go 1.22+ route patterns), no web framework |
| Storage | SQLite via `modernc.org/sqlite` (pure Go, no CGO), WAL mode |
| UI | React + TypeScript + Vite, built to static files, embedded with `embed` |
| API | Plane v1-shaped REST under `/api/v1/`. Field shapes: `docs/plane-api-reference.md` |
| MCP | Streamable HTTP at `/mcp`, official `github.com/modelcontextprotocol/go-sdk` |
| Deploy | One container, one volume for the `.db` file |

## Layout

```
cmd/mostlydone/main.go     entrypoint: config, open DB, start HTTP server
internal/store/         schema, migrations, all SQL. No HTTP types here.
internal/api/           REST handlers, auth middleware, JSON shapes
internal/mcp/           MCP tools; calls internal/store directly
internal/webui/         embed.FS of web/dist + SPA fallback handler
web/                    React source (Vite)
docs/                   plane-api-reference.md, REA capture notes
```

## Config (environment variables)

| Name | Default | Meaning |
| --- | --- | --- |
| `MOSTLYDONE_ADDR` | `:8080` | listen address. The default listens on all interfaces on purpose: containers and Kubernetes need `0.0.0.0` to be reachable, and the first-run setup token (see Auth) closes the open first-run window. For bare-metal runs behind a local reverse proxy, set `127.0.0.1:8080` |
| `MOSTLYDONE_DB` | `./mostlydone.db` | SQLite file path |
| `MOSTLYDONE_ADMIN_EMAIL` / `MOSTLYDONE_ADMIN_PASSWORD` | unset | if set and no users exist, create the first admin at start |
| `MOSTLYDONE_WORKSPACE_SLUG` | `main` | slug of the workspace created at first start |
| `MOSTLYDONE_SECURE_COOKIES` | `false` | set `true` behind HTTPS |
| `MOSTLYDONE_CLIENT_IP_HEADER` | unset | header with the real client address when behind a proxy (e.g. `CF-Connecting-IP`). Used only by the login throttle. Set it only when all traffic comes through that proxy |

## Auth

- Users: email + bcrypt password hash.
- First run with no users: `POST /auth/setup` (email, password, display name,
  `setup_token`) creates the admin and the workspace. Refused once any user exists.
  - At startup, if there are no users (and the `MOSTLYDONE_ADMIN_*` bootstrap
    did not create one), the server generates a one-time setup token
    (`setup_` + 192 random bits), logs it once, and writes it to
    `<MOSTLYDONE_DB>.setup-token` with mode 0600 (best effort; a failure is
    logged as a warning). The token is held in memory only, in the API value.
  - A missing or wrong `setup_token` gets 403 `{"detail": ...}`. It is compared
    in constant time. Each failure counts against the login throttle (same
    limiter, same client address), so guessing ends in 429.
  - After a successful setup the token is cleared from memory and the file is
    removed; it cannot be reused. With the env-var bootstrap no token exists.
  - `GET /auth/me` returns `setup_required`, plus `setup_token_required: true`
    while setup is pending. The UI's setup form asks for the token.
- Browser: `POST /auth/login` sets an HttpOnly, SameSite=Lax session cookie.
  Only a SHA-256 hash of the session token is stored. `POST /auth/logout`.
- API clients: `X-API-Key: <token>` (Plane style). MCP also accepts
  `Authorization: Bearer <token>`. Tokens are shown once at creation;
  only a SHA-256 hash is stored.
- Token management (session only): `GET/POST /auth/tokens`, `DELETE /auth/tokens/{id}`.
- Cookie-authenticated writes must send header `X-Requested-With: mostlydone`
  (CSRF guard; a cross-site form cannot set it). API-key requests are exempt.
- Every `/api/v1/` and `/mcp` request needs a session or a token. 401 otherwise.

## Migrations

- Migrations are embedded SQL files applied in version order at startup.
- If the database's highest applied version is newer than the binary's, startup
  fails with `database schema vN is newer than this binary (max vM); upgrade
  MostlyDone or restore a backup`, and the database is not modified.
- Before applying pending migrations to an existing database (not a brand-new
  one), a snapshot is made with `VACUUM INTO`: `<db>.pre-v<N>.bak`, where N is
  the first pending version. An existing file is never overwritten; a UTC
  timestamp is appended (`<db>.pre-v<N>.bak.20060102T150405Z`). If the snapshot
  fails, startup fails. In-memory databases are skipped.

## Data model

IDs are UUID v4 strings. Timestamps are RFC 3339 UTC strings. Dates are `YYYY-MM-DD`.
Foreign keys on, `ON DELETE CASCADE` for children. Hard deletes, except projects: deleting a project moves it to a 30-day trash (`projects.deleted_at`) and it is hard-deleted when purged.

- `users`, `sessions`, `api_tokens`
- `workspaces` (name, slug unique), `workspace_members` (role)
- `projects` (workspace, name, identifier unique per workspace, description, next issue sequence, `deleted_at` — NULL means live, set means in the trash)
- `states` (project, name, color, group: backlog | unstarted | started | completed | cancelled, sequence, default flag)
- `labels` (project, name, color)
- `issues` (project, sequence_id unique per project, name, description_html, state, priority: urgent | high | medium | low | none, parent, start_date, target_date, sort_order, created_by, completed_at)
- `issue_assignees`, `issue_labels`
- `cycles` (project, name, description, start_date, end_date), `cycle_issues` (an issue is in at most one cycle)
- `modules` (project, name, description, start_date, target_date, status), `module_issues`
- `comments` (issue, actor, comment_html)
- `issue_links` (issue, title, url)
- `activities` (issue, actor, verb, field, old_value, new_value) — written by the store on every issue change

Rules:
- New project gets Plane's default states (see reference doc) and one is default.
- `sequence_id` comes from the project's counter inside the same transaction as the insert.
- Moving an issue into a `completed` group state sets `completed_at`; moving out clears it.
- `description_html` and `comment_html` are sanitized on write (allow-list of tags). The UI never renders unsanitized HTML.

## REST API

Plane v1 paths and shapes from `docs/plane-api-reference.md`, limited to the
resources in scope. Both `/issues/` and `/work-items/` spellings route to the
same handlers. Plane's list envelope and cursor pagination are kept.
Errors: JSON body, 400 validation, 401 no auth, 403 forbidden, 404 not found, 409 conflict.

Rulings where the reference is ambiguous or we differ on purpose:

| Topic | Ruling |
| --- | --- |
| Missing or bad credentials | 401 `{"detail": "..."}` |
| Error bodies | Reference section 1 shapes: field errors `{"field": ["msg"]}`, view errors `{"error": "msg"}`, 409 adds `"id"` |
| Pagination | Reference envelope and cursor, source form of `prev_cursor`. `per_page` default 100, max 1000 |
| Datetimes out | UTC, `2006-01-02T15:04:05.000000Z` |
| Deletes | Work items, comments, links, states, labels, cycles and modules are hard-deleted. **Projects go to a 30-day trash**: `DELETE .../projects/{pid}/` (admin) returns 204 and sets `deleted_at`. A trashed project and everything in it is invisible and read/write-blocked everywhere (REST, workspace-level identifier and search routes, MCP: 404 / "unknown project"); live project responses keep `deleted_at: null`. It keeps its identifier and name until purged, so creating a project that collides with a trashed one is 409 ("... is used by a project in the trash; restore it or delete it permanently"). Retention is 30 days. The server purges expired projects (hard delete, cascading) at startup and then hourly |
| Project trash (extension, not in Plane's public API) | Admin only. `GET /workspaces/{ws}/trashed-projects/` lists trashed projects in the list envelope, newest deleted first, as project objects with the real `deleted_at` plus `purge_at` (`deleted_at` + 30 days). `POST .../trashed-projects/{id}/restore/` returns 200 with the restored project (`deleted_at: null`; 404 if not trashed or past retention; 409 if a live project took its name or identifier). `DELETE .../trashed-projects/{id}/` permanently deletes it now (204; 404 if not in the trash). The web UI exposes this under workspace settings → Trash. MCP has no trash tools, and `list_projects` excludes trashed projects |
| Status codes | As the reference, including state create = 200 and add-to-cycle/module = 200 |
| Rate limiting | None on the API, no rate-limit headers. Login only: 10 failed sign-ins from one client address in 15 minutes, then 429 with `Retry-After` until the window ends. In memory; a restart clears it |
| Roles | 20 admin, 15 member, 5 guest. No per-project membership: every workspace member belongs to every project. Guests can read and comment only. Project members endpoints return the workspace members |
| Triage state | Not created. New projects get the five visible default states |
| `cycle_view`, `module_view` | Default `true` (Plane defaults to false). Other feature flags are stored and returned but do nothing |
| Out-of-scope fields | Returned with Plane's default value (`estimate_point: null`, `type_id: null`, `is_draft: false`, `archived_at: null`, `point: null`, `logo_props: {}` …) and ignored on write. No archive, transfer or summary endpoints. `projects-lite`, `cycles-lite` and `modules-lite` ARE implemented (read-only lists, Plane envelope, no metric counts; `include_archived` accepted and ignored) because Plane's official MCP server lists projects, cycles and modules through them and cannot discover ids without them. Other `-lite` endpoints (`members-lite`, `project-members-lite`) remain out of scope |
| Activity `epoch` | Stored as REAL, emitted as an integer (unix seconds, truncated). The reference lists float, but the official Python SDK validates it as an integer |
| `external_source` / `external_id` | Stored on projects, states, labels, work items, cycles, modules, comments, with the 409 duplicate rule. Needed later to import from the old Plane |
| Cycle dates | Accept `YYYY-MM-DD` or an ISO datetime, both-or-neither. Stored as dates. Returned as `…T00:00:00.000000Z` (start) and `…T23:59:59.000000Z` (end) |
| Modules | A work item may be in many modules. Adding never removes it from another module |
| `fields` / `expand` | Supported on list and detail GETs. Unknown names ignored. `expand` supports `assignees`, `labels`, `state`, `project`, `parent`, `actor`, `created_by` |
| Activities | Written in the same transaction as the change (no background task). `verb`: created, updated, deleted. `field`: name, description, state, priority, assignees, labels, parent, start_date, target_date, cycles, modules, link, comment |
| Link titles | No crawler. `title` is whatever the client sends |
| Project `network` | Stored, default 2, no effect |

Extensions the UI needs (not in Plane's public API):
- Work item responses add `cycle_id` (uuid or null), `module_ids` (uuid array)
  and `sub_issues_count` (int). `cycle_id` and `module_ids` are writable on
  create and PATCH (`module_ids` replaces the set).
- Issue list filters as query params: `state`, `state_group`, `priority`,
  `assignee`, `label`, `cycle`, `module`, `parent`, `search`, `order_by`.
  Comma-separated values mean "any of".
- `sort_order` (float) is writable on issues so the board can reorder cards with one PATCH.

## MCP

Endpoint `/mcp`, same auth as the API. Tools take human-readable references
so an agent rarely needs a lookup first: project by identifier or name,
work item by `PROJ-12`, state/label/cycle/module by name, user by email or `me`.

Tools: `list_projects`, `create_project`, `list_work_items` (same filters as REST),
`get_work_item` (includes comments), `create_work_item`, `update_work_item`,
`add_comment`, `list_states`, `list_labels`, `create_label`, `list_cycles`,
`create_cycle`, `list_modules`, `create_module`, `list_members`.
`create_work_item` / `update_work_item` accept `cycle` and `modules` directly.
Results are compact JSON (no null-padded fields). Lists default to 50 rows with a cursor.

## Performance budget

| Measure | Target |
| --- | --- |
| Idle memory (RSS) | under 50 MB |
| Container image | under 30 MB |
| Issue list, 10k issues, 50 per page | under 20 ms server time |
| Containers | 1 |

How: one process, no queue or cache service, SQLite WAL with one writer
connection and a small reader pool, indexes on every filter column, no N+1
queries on list endpoints (assignees/labels fetched in one extra query per
page), UI assets pre-compressed and served with long cache headers.

## Verification

- `go test ./...` passes offline. Tests call handlers directly
  (`httptest.NewRecorder`); the sandbox blocks opening ports.
- MCP tools tested through the SDK's in-memory transport.
- Final check: real binary run, memory measured, UI clicked through in a browser.

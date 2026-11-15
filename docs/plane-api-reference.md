# Plane public REST API v1 - wire-compat reference (subset)

Written in my own words from reading docs + source. No source code copied.
Legend: **[D]** confirmed in docs, **[S]** confirmed in source, **[D!=S]** docs and source disagree (source = observed server behaviour, listed first), `UNCONFIRMED` = not verified.
Base prefix for everything: `/api/v1`. All bodies JSON. `{ws}` = workspace slug, `{pid}` = project UUID. IDs are UUID strings.

## 0. URLs read
Docs (VitePress static HTML, text extracted):
- https://developers.plane.so/api-reference/introduction
- https://developers.plane.so/api-reference/{issue/overview, issue/add-issue, issue/list-issues, issue/get-issue-sequence-id, issue/search-issues, issue/delete-issue, project/overview, project/add-project, state/overview, label/overview, cycle/overview, cycle/add-cycle-work-items, cycle/transfer-cycle-work-items, module/overview, module/add-module-work-items, issue-comment/overview, link/overview, issue-activity/overview, members/overview, user/overview, user/get-current-user}
Source (raw.githubusercontent.com/makeplane/plane/preview/apps/api/plane/...), tree via api.github.com `git/trees/preview?recursive=1`:
- api/urls/{__init__,work_item,project,state,label,cycle,module,member,user}.py
- api/views/{base,issue,project,state,cycle,module,member,user}.py
- api/serializers/{base,issue,project,state,cycle,module,member,user}.py
- api/middleware/api_authentication.py, api/rate_limit.py
- db/models/{base,issue,project,state,label,cycle,module}.py, db/mixins.py
- utils/{paginator,order_queryset,timezone_converter,cycle_transfer_issues}.py, app/permissions/project.py
NOT read: bgtasks/issue_activities_task.py, settings (rate limit value), IssueType/IssueSequence/EstimatePoint models, openapi examples.

## 1. Auth, rate limit, errors

| Item | Value |
|---|---|
| API key header | `X-API-Key: plane_api_<token>` [D][S] (header name matched case-insensitively). Token row must be active, not expired (`expired_at` null or future), owner user active. `last_used` updated per call. |
| OAuth | docs also describe `Authorization: Bearer <oauth token>` [D]. Not present in the `apps/api` auth class [S] -> out of scope/UNCONFIRMED. |
| Missing/invalid key | Docs: 401 [D]. Source: auth class has no `WWW-Authenticate` header, so DRF would emit **403** with `{"detail": "..."}`; messages: "Authentication credentials were not provided." / "Given API token is not valid". [D!=S] 403 is inference from DRF semantics, `UNCONFIRMED` at runtime. |
| Rate limit | Docs: 60 req/min per API key [D]. Setting `API_KEY_RATE_LIMIT` value `UNCONFIRMED` (settings not read). Keyed by API key string. |
| Rate headers (on every response when key present) | `X-RateLimit-Remaining` (int), `X-RateLimit-Reset` (unix epoch seconds, now+window) [S]. 429 on excess; body `UNCONFIRMED` (DRF default `{"detail":"Request was throttled. Expected available in N seconds."}`). |
| Body must be object/array | POST/PUT/PATCH with other JSON -> 400 `{"detail":"Request body must be a JSON object or array."}` [S] |
| Timezone | Server activates the user's `user_timezone` per request (affects datetime rendering). Output format `UNCONFIRMED`; docs show ISO-8601 with `Z`. |
| Deletes | All are **soft deletes** (`deleted_at` set) [S]; soft-deleted rows free up unique names/identifiers. |

### Error body shapes [S]
| Situation | Status | Body |
|---|---|---|
| Serializer validation | 400 | `{"<field>": ["msg", ...]}`; cross-field/`validate()` errors -> `{"non_field_errors": ["msg"]}` |
| View-level checks | 400/403/404/409 | `{"error": "msg"}` (409 adds `"id": "<existing uuid>"`) |
| Any model-not-found raised in a view | 404 | `{"error": "The requested resource does not exist."}` |
| DB integrity error not otherwise caught | 400 | `{"error": "The payload is not valid"}` |
| Django ValidationError / KeyError | 400 | `{"error": "Please provide valid detail"}` / `{"error": "The required key does not exist."}` |
| Permission denied (role too low, not a member) | 403 | `{"detail": "You do not have permission to perform this action."}` |
| Unhandled | 500 | `{"error": "Something went wrong please try again later"}` |
| Bad cursor / per_page | 400 | `{"detail": "Invalid cursor parameter."}`, `{"detail": "Invalid per_page parameter."}`, `{"detail": "Invalid per_page value. Must be greater than zero."}`, `{"detail": "Invalid per_page value. Cannot exceed N."}` |
| Unsupported method on a route | 405 | DRF default |

Success codes: GET/PATCH 200; POST create 201 (exceptions: **state create = 200**, cycle-issues add = 200, module-issues add = 200, transfer = 200); DELETE and archive/unarchive 204 (empty body).

## 2. Pagination (list endpoints that paginate)

Query: `per_page` (int), `cursor` (string).
Cursor format: `"<page_size>:<page_index>:<is_prev>"`, e.g. `"20:1:0"`. page_index is 0-based; is_prev is 0/1. Server offset = page_index * per_page. The `per_page` param (not the cursor's first number) decides page size. Cursor must have exactly 3 colon parts; first in 0..max, second >= 0, else 400.
Default `cursor` when absent = page 0.

| per_page limits | value |
|---|---|
| Source (generic paginate defaults) | default **1000**, max **1000** [S] |
| Intro docs | default 100, max 100 [D] |
| Work-item list docs | default 20, max 100 [D] |
-> [D!=S]. Recommendation: default 100, max 100 satisfies every client that follows the docs.

Envelope (all keys always present) [S]; docs list a subset [D]:
```json
{
  "grouped_by": null, "sub_grouped_by": null,
  "total_count": 150,
  "next_cursor": "20:1:0", "prev_cursor": "20:-1:1",
  "next_page_results": true, "prev_page_results": false,
  "count": 20, "total_pages": 8, "total_results": 150,
  "extra_stats": null,
  "results": [ ... ]
}
```
| Key | Type | Meaning |
|---|---|---|
| next_cursor | string | always `"<per_page>:<page+1>:0"` even on last page |
| prev_cursor | string | always `"<per_page>:<page-1>:1"`; on page 0 source yields `"<per_page>:-1:1"` while docs example shows `""` [D!=S] |
| next_page_results | bool | true iff more rows exist after this page (server fetches limit+1) |
| prev_page_results | bool | page_index > 0 |
| count | int | number of items in this page |
| total_results / total_count | int | total rows matching the query (for work-item list: all non-archived, non-draft issues in the project, regardless of external filter; guests: only own) |
| total_pages | int | ceil(total_results / per_page) |
| extra_stats / grouped_by / sub_grouped_by | null | always null in v1 |
Ordering is by the view's `order_by` then `-created_at` tiebreak (OFFSET-style paging).

Which list endpoints are NOT paginated (plain JSON array): workspace members, project members, cycle list with `cycle_view=current`, POST cycle-issues / module-issues responses, search (object `{"issues":[...]}`).

## 3. `fields` and `expand`
Both are comma-separated query params, available on list + detail GETs of most resources (not on -lite endpoints, create/patch responses, or member endpoints) [S].
- `fields=id,name`: response objects keep only the named keys. Unknown names are silently ignored in source (docs claim an error) [D!=S]; `UNCONFIRMED` runtime.
- `expand=a,b`: a name is honoured only if it is a field on that serializer. Expandable names and their nested shape [S]:

| expand name | replaces id with |
|---|---|
| user, actor, created_by, updated_by, owned_by, default_assignee, project_lead, members | UserLite `{id, first_name, last_name, email, avatar, avatar_url, display_name}` |
| state | StateLite `{id, name, color, group}` |
| project | ProjectLite `{id, identifier, name, cover_image, icon_prop, emoji, description, cover_image_url, archived_at}` |
| parent | IssueLite `{id, sequence_id, project_id}` |
| issue | full work item object |
| workspace | WorkspaceLite (`UNCONFIRMED` shape) |
| estimate_point | EstimatePoint object (`UNCONFIRMED`) |
| `assignees` (work item only) | array of UserLite (unexpanded = array of id strings) |
| `labels` (work item only) | array of full Label objects (unexpanded = array of id strings) |
A null relation stays null. Any other name that is a plain FK field is left as an id. Docs also claim `type`, `module` expand [D]; source has no mapping for them [S] -> `UNCONFIRMED`.

## 4. Common audit fields (on nearly every object) [S]
`id` uuid; `created_at`, `updated_at` datetime; `deleted_at` datetime|null; `created_by`, `updated_by` uuid|null (updated_by null until first update); `project`, `workspace` uuid. All of these are read-only. Role ints used everywhere: **20 admin, 15 member, 5 guest**.

Permission summary (for the 403 rules) [S]: read endpoints need active project membership (any role); write endpoints on project entities (issues, cycles, modules, links, relations) need project role 15/20; labels/states/project-member writes need project admin (or project member + workspace admin); comments allow any active project member including guests; workspace-members list needs workspace admin. Restricted guest = role 5 on a project with `guest_view_all_features=false`: sees only work items they created (others -> 404).

## 5. Projects
| Method + path | Status | Notes |
|---|---|---|
| GET `/workspaces/{ws}/projects/` | 200 paginated | projects the caller is an active member of OR `network=2` (public). Archived are **not** excluded [S]. `order_by` allowlist: created_at, updated_at, name, network, sort_order (leading `-` ok); default `sort_order` (caller's own per-project sort order). Invalid -> default. |
| GET `/workspaces/{ws}/projects-lite/` | 200 paginated | ProjectLite items; `include_archived=true|1` (default excludes archived); default order `-created_at`. 404 `{"error":"Provided workspace does not exist"}` |
| POST `/workspaces/{ws}/projects/` | 201 | any workspace admin/member (guest -> 403) |
| GET `/workspaces/{ws}/projects/{pk}/` | 200 | supports fields/expand |
| PATCH `/workspaces/{ws}/projects/{pk}/` | 200 | project admin; 400 `{"error":"Archived project cannot be updated"}` |
| DELETE `/workspaces/{ws}/projects/{pk}/` | 204 | project admin |
| POST / DELETE `/workspaces/{ws}/projects/{pid}/archive/` | 204 / 204 | archive sets `archived_at=now`; DELETE unarchives |
| GET `/workspaces/{ws}/projects/{pid}/summary/` | 200 | workspace admin. `?fields=members,states,labels,cycles,modules,issues,intakes,pages` (default all) -> `{"id","name","identifier","counts":{...}}` |

**Project object** [S] (docs [D] differ: it names `inbox_view` for `intake_view`):
| Field | Type | Null | Create | Update | Notes |
|---|---|---|---|---|---|
| id | uuid | no | RO | RO | |
| name | string(255) | no | **required** | yes | must not contain any of `& + , : ; $ ^ } { * = ? @ # \| ' < > . ( ) % ! -`; unique per workspace among live projects |
| identifier | string(12) | no | **required** | yes | same forbidden chars; stripped + UPPERCASED; unique per workspace (case-folded) |
| description | string | no ("" default) | yes | yes | |
| description_text, description_html | json | yes | RO-ish (not in create/update field list) | | |
| network | int | no | not writable via API (default **2** public) | not writable | enum 0 secret, 2 public |
| project_lead, default_assignee | uuid | yes | yes | yes | must be active workspace members |
| emoji | string | yes | yes | RO | |
| icon_prop | json | yes | yes | yes | |
| logo_props | json | no | RO (server fills random icon `{"in_use":"icon","icon":{"name","color"}}` if absent) | RO | |
| module_view, cycle_view, issue_views_view, page_view, intake_view | bool | no | yes | yes | defaults **false, false, false, true, false** [S]. `cycle_view=false` blocks cycle creation, `module_view=false` blocks module creation (see sec 9/10). |
| is_time_tracking_enabled, is_issue_type_enabled, guest_view_all_features | bool | no | yes | yes | default false |
| cover_image | string(url) | yes | yes | yes | `cover_image_url` read-only string|null (asset url or cover_image) |
| archive_in, close_in | int 0..12 | no | yes | yes | default 0 |
| timezone | string | no | yes | yes | pytz common name; default = workspace timezone |
| external_source, external_id | string(255) | yes | yes | yes | |
| default_state | uuid | yes | no | yes (must be state in project) | null at creation |
| estimate | uuid | yes | no | yes | |
| archived_at | datetime | yes | RO (use /archive/) | RO | |
| cover_image_asset | uuid | yes | RO | RO | |
| total_members, total_cycles, total_modules | int | no | RO | RO | members excludes bots/inactive |
| is_member | bool | | RO | | caller is active member |
| member_role | int|null | | RO | | caller's role |
| is_deployed | bool | | RO | | has publish board |
| sort_order | float|null | | RO | | caller's per-project order |

Create behaviours [S]: runs in one transaction. Creator becomes ProjectMember role 20; if `project_lead` differs it also becomes role 20. Six default states are inserted (sec 6). No default labels/cycles/modules. Errors: duplicate/blank identifier -> **409** `{"identifier":"The project identifier is already taken"}` (blank identifier also lands here); duplicate name -> **409** `{"name":"The project name is already taken"}`; unknown workspace -> 404 `{"error":"Workspace does not exist"}`; forbidden chars / non-member lead -> 400 field errors. (Identifier uniqueness is also tracked in a separate project_identifiers table; `ProjectIdentifier` row creation path `UNCONFIRMED`.)

## 6. States
Paths: GET/POST `/workspaces/{ws}/projects/{pid}/states/`, GET/PATCH/DELETE `/workspaces/{ws}/projects/{pid}/states/{state_id}/`. Read = any project member; write = project admin. Lists exclude triage-group states and states of archived projects.

| Field | Type | Null | Create | Update | Notes |
|---|---|---|---|---|---|
| id | uuid | no | RO | | |
| name | string(255) | no | **required** | yes | unique per project (live). Dup on create -> 409 `{"error":"State with the same name already exists in the project","id":..}`; on PATCH -> 400 `{"error":"The payload is not valid"}` |
| color | string(255) | no | **required** | yes | hex like `#60646C`; not validated |
| group | enum | no | optional, default `backlog` [S] (docs say required [D!=S]) | yes | `backlog, unstarted, started, completed, cancelled` (+ `triage` exists internally but sending it -> 400 `non_field_errors: "Cannot create triage state"`) |
| description | string | no | optional ("") | yes | |
| default | bool | no | optional (false) | yes | setting true clears `default` on all other states of the project (atomic) |
| sequence | float | no | optional | yes | **on create overwritten** with `max(sequence in project)+15000` whenever the project has any state; default 65535 if none |
| slug | string | no | RO | RO | slugify(name) (docs call it `workspace_slug` [D] - typo) |
| is_triage | bool | no | RO-ish | | false for API-visible states |
| external_source, external_id | string | yes | yes | yes | create dup pair -> 409 `{"error":"State with the same external id and external source already exists","id":..}` |
| project, workspace | uuid | no | RO | RO | |
+ audit fields. Response of create = **200** (not 201), of PATCH 200.
DELETE -> 204; 400 `{"error":"Default state cannot be deleted"}` if `default`; 400 `{"error":"The state is not empty, only empty states can be deleted"}` if any work item uses it.

**Default states created with every new project** [S] (`sequence`, color, group, default):
| name | group | color | sequence | default |
|---|---|---|---|---|
| Backlog | backlog | #60646C | 15000 | **true** |
| Todo | unstarted | #60646C | 25000 | false |
| In Progress | started | #F59E0B | 35000 | false |
| Done | completed | #46A758 | 45000 | false |
| Cancelled | cancelled | #9AA4BC | 55000 | false |
| Triage | triage | #4E5355 | 65000 | false (hidden from API lists) |
Group meaning for `completed_at`: see sec 7.

## 7. Work items (issues)
### 7.1 Path spellings
Both exist and route to the same views [S]. Source labels the `/issues/` set "Deprecated"; `/work-items/` is current and the only one in the docs [D]. The deprecated set **still works** (same handlers). Differences: attachments path is `issue-attachments` (old) vs `attachments` (new); relations exist only on `/work-items/`.

| Method + path (new) | Old equivalent | Status |
|---|---|---|
| GET `/workspaces/{ws}/projects/{pid}/work-items/` | `.../issues/` | 200 paginated |
| POST `/workspaces/{ws}/projects/{pid}/work-items/` | `.../issues/` | 201 |
| GET/PATCH/DELETE `/workspaces/{ws}/projects/{pid}/work-items/{id}/` | `.../issues/{id}/` | 200/200/204 |
| GET `/workspaces/{ws}/work-items/{PROJ}-{seq}/` | `/workspaces/{ws}/issues/{PROJ}-{seq}/` | 200 |
| GET `/workspaces/{ws}/work-items/search/` | `/workspaces/{ws}/issues/search/` | 200 |
PUT on `{id}/` is not routed (405) even though a handler exists. Relations (`.../work-items/{id}/relations/`) out of scope.

### 7.2 Work item object [S] (docs list `description_stripped`, which source excludes [D!=S])
| Field | Type | Null | Create | Update | Notes |
|---|---|---|---|---|---|
| id | uuid | no | RO | | |
| name | string(255) | no | **required** | yes | |
| description_html | string | no | optional, default `"<p></p>"` | yes | parsed/re-serialised as HTML, sanitised; invalid -> 400 |
| description_binary | binary/string | yes | yes | yes | encoding `UNCONFIRMED` |
| (description_json, description_stripped) | | | never returned | | server keeps a stripped text copy internally |
| priority | enum | no | optional, default `none` | yes | `urgent, high, medium, low, none` |
| state | uuid | no (server fills) | optional | yes | must belong to project else 400 `non_field_errors: "State is not valid please pass a valid state_id"`. Omitted -> project's `default=true` non-triage state, else first non-triage state by sequence |
| parent | uuid | yes | optional | yes | must be work item in same project else 400 |
| start_date, target_date | date `YYYY-MM-DD` | yes | optional | yes | start > target -> 400 `non_field_errors: "Start date cannot exceed target date"` (checked only when both in payload) |
| point | int 0..12 | yes | optional | yes | legacy |
| estimate_point | uuid | yes | optional | yes | must exist in project (docs describe int 0-7 [D!=S]) |
| sort_order | float | no | optional | yes | server default: `max(sort_order in same project+state)+10000`, else 65535 |
| sequence_id | int | no | RO | RO | see 7.4 |
| completed_at | datetime | yes | RO | RO | see 7.4 |
| archived_at | date | yes | RO | RO | |
| is_draft | bool | no | optional | RO after create | |
| external_source, external_id | string(255) | yes | optional | yes | |
| type_id | uuid | yes | optional | yes | work-item type (out of scope). Response key name for the type FK: `UNCONFIRMED` (`type` vs `type_id`); default = project's default issue type if any |
| assignees | uuid[] | | optional (write) | yes (replaces whole set; `[]` clears) | each must be an active project member with role >= 15 else 400 `non_field_errors: "Assignees [..] are not active members of this project"`. Response: array of id strings (objects with `expand=assignees`) |
| labels | uuid[] | | optional | yes (replaces set) | each must belong to project else 400 "Labels [..] do not belong to this project". Response id strings (or Label objects with expand) |
| project, workspace | uuid | no | RO | RO | |
| created_by, updated_by, created_at, updated_at, deleted_at | | | RO | | |
Create without assignees: if the project has `default_assignee` who is an active member (role >= 15), that user is auto-assigned.

### 7.3 Endpoints in detail
**List** `GET .../work-items/` query: `cursor, per_page, fields, expand, order_by, external_id, external_source`.
- `order_by` allowlist (leading `-` allowed): created_at, updated_at, sequence_id, sort_order, target_date, start_date, completed_at, archived_at, priority, state__name, state__group, assignees__first_name, labels__name, issue_module__module__name. Invalid -> `-created_at`. `priority` sorts urgent>high>medium>low>none (asc); state sorts by group order backlog, unstarted, started, completed, cancelled.
- If **both** `external_id` and `external_source` are sent: returns a single work item object (200, NOT an envelope); 404 if none.
- `pql` or `filters` params -> 400 `{"pql": "...not supported on this Plane edition...", "unsupported_parameters": [...]}`. There are **no other filter params** (no state/priority/assignee filters) in this API [S].
- Excludes archived, draft, deleted, and triage-state work items and items in archived projects.
**Create** POST: 201 + work item. If `external_id` AND `external_source` both present and a pair already exists in project -> 409 `{"error":"Issue with the same external id and external source already exists","id":"<uuid>"}`. Queues activity (`created`).
**Get** GET `{id}/` 200; 404 for archived/draft/triage items (uses visible manager) or restricted guest.
**PATCH** partial; 200 + full object. 409 if changing external_id to one already used (same message). Queues activity `updated`.
**DELETE** 204; only creator or project admin (role 20) else 403 `{"error":"Only admin or creator can delete the work item"}`.
**By identifier** GET `/workspaces/{ws}/work-items/{PROJ}-{seq}/`: `PROJ` = project `identifier` matched exactly (stored uppercase), `seq` = `sequence_id`. Non-numeric `seq` -> 404 `{"error":"Work item not found"}`; missing -> 404 `{"error":"The requested resource does not exist."}`. Supports fields/expand. Returns the same work item object.
**Search** GET `/workspaces/{ws}/work-items/search/`: params `search` (str), `limit` (default 10), `workspace_search` (default `"false"`), `project_id`. Empty/absent `search` -> 200 `{"issues": []}`. Matches name icontains OR project identifier icontains OR `sequence_id` equal to any standalone integer token in the query (e.g. "PROJ-12" matches seq 12). `project_id` filter applies only when `workspace_search=="false"`. Only items visible to the caller. Response 200: `{"issues":[{"name","id","sequence_id"(int),"project__identifier","project_id","workspace__slug"}]}` (no envelope, no ordering guarantee).

### 7.4 Server behaviours
- `sequence_id`: per project, assigned at insert under a per-project advisory lock = `max(existing sequence in project)+1`, first = 1. Never client-writable. Reuse after delete `UNCONFIRMED` (derived from a separate sequence table that is not cleaned up on delete).
- `completed_at`: evaluated on create and whenever `state` changes: new state group `completed` -> `completed_at = now`; any other group -> `null`. Not touched if state unchanged.
- Default state on create: see `state` row above.
- Deletion is soft.

## 8. Labels
Paths: GET/POST `/workspaces/{ws}/projects/{pid}/labels/`; GET/PATCH/DELETE `.../labels/{pk}/`. Read = project member; write = project admin. List paginated, default order `-created_at`.
| Field | Type | Null | Create | Update | Notes |
|---|---|---|---|---|---|
| id | uuid | no | RO | | |
| name | string(255) | no | **required** | yes | unique per project among live labels -> 409 `{"error":"Label with the same name already exists in the project","id":..}` |
| color | string(255) | no ("" ok) | optional | yes | hex |
| description | string | no | optional | yes | |
| parent | uuid | yes | optional | yes | parent label |
| sort_order | float | no | optional | yes | on create overwritten by `max(sort_order in project)+10000` if any label exists; default 65535 |
| external_source, external_id | string | yes | optional | yes | dup pair -> 409 (`"Label with the same external id and external source already exists"`) |
| project, workspace + audit | | | RO | | |
POST 201, PATCH 200, DELETE 204. GET detail ignores fields/expand.

## 9. Cycles
| Method + path | Status |
|---|---|
| GET/POST `/workspaces/{ws}/projects/{pid}/cycles/` | 200 paginated (or array, see below) / 201 |
| GET `.../cycles-lite/` | 200 paginated, items = Cycle (no metrics), `order_by` allowlist created_at, updated_at, name, start_date, end_date, sort_order |
| GET/PATCH/DELETE `.../cycles/{pk}/` | 200/200/204 |
| GET `.../cycles/{cycle_id}/cycle-issues/` | 200 paginated; items = **work item objects** (sec 7.2) |
| POST `.../cycles/{cycle_id}/cycle-issues/` | 200, body `{"issues":[uuid,...]}` |
| GET `.../cycles/{cycle_id}/cycle-issues/{issue_id}/` | 200 CycleIssue row |
| DELETE `.../cycles/{cycle_id}/cycle-issues/{issue_id}/` | 204 (removes association only) |
| POST `.../cycles/{cycle_id}/transfer-issues/` | 200 `{"message":"Success"}`, body `{"new_cycle_id": uuid}` |
| POST `.../cycles/{cycle_id}/archive/`, GET `.../archived-cycles/`, DELETE `.../archived-cycles/{cycle_id}/unarchive/` | 204 / 200 paginated / 204 |
Cycle list `cycle_view` param: `all` (default), `current` (start<=now<=end; returns a **plain array**, unpaginated), `upcoming` (start>now), `completed` (end<now), `draft` (both dates null), `incomplete` (end>=now or null). Archived excluded.

**Cycle object** [S]:
| Field | Type | Null | Create | Update | Notes |
|---|---|---|---|---|---|
| id | uuid | no | RO | | |
| name | string(255) | no | **required** | yes | |
| description | string | no | optional | yes | |
| start_date, end_date | datetime | yes | optional | yes | **both or neither** else 400 `{"error":"Both start date and end date are either required or are to be null"}`; start > end -> 400 "Start date cannot exceed end date". Input must parse as an ISO-8601 datetime (date-only acceptance `UNCONFIRMED`; docs say "date"). Stored normalised: start = project-timezone midnight of that date +1s -> UTC (if that date is "today" in project tz, start = now); end = project-tz 23:59 of that date -> UTC |
| owned_by | uuid | no | optional, default caller | yes | |
| timezone | string | no | optional, default `UTC` | yes | pytz common names |
| sort_order | float | no | RO on create (server: `min(sort_order in project)-10000`, default 65535) | writable via PATCH only (the one field allowed after completion) | |
| view_props, progress_snapshot, logo_props | json | no | RO | RO | default `{}` |
| version | int | no | RO | | default 1 |
| archived_at | datetime | yes | RO | RO | |
| external_source, external_id | string | yes | optional | yes | dup pair -> 409 `{"error":"Cycle with the same external id and external source already exists","id":..}` |
| total_issues, completed_issues, cancelled_issues, started_issues, unstarted_issues, backlog_issues | int | | RO | | counts of linked work items that are non-archived, non-draft; per state group (total = all groups) |
| total_estimates, completed_estimates, started_estimates | float|null | | RO | | sums of estimate point `key` values (`UNCONFIRMED` semantics) |
| project, workspace + audit | | | RO | | |
Preconditions: project `cycle_view` must be true else 400 `non_field_errors: "Cycles are not enabled for this project"`. PATCH: archived -> 400 `{"error":"Archived cycle cannot be edited"}`; cycle ended (end_date < now) and no `sort_order` in body -> 400 `{"error":"The Cycle has already been completed so it cannot be edited"}`. DELETE: creator or project admin else 403 `{"error":"Only admin or creator can delete the cycle"}`; associations removed.
Archive: only ended cycles, else 400 `{"error":"Only completed cycles can be archived"}`.

**Add work items** POST body `{"issues":[...]}`: empty -> 400 `{"error":"Work items are required","code":"MISSING_WORK_ITEMS"}`; cycle ended -> 400 `{"code":"CYCLE_COMPLETED","message":"The Cycle has already been completed so no new issues can be added"}`. An issue already in a different cycle is **moved** (a work item is in at most one cycle); unknown/foreign/draft/archived ids are silently skipped; existing links are idempotent. Response 200 = plain array of CycleIssue rows of that cycle: `{id, created_at, updated_at, deleted_at, created_by, updated_by, project, workspace, issue (uuid), cycle (uuid), sub_issues_count}`.
**Transfer**: old cycle must have ended (`end_date <= now`) else 400 `{"error":"The old cycle is not completed yet"}`; missing `new_cycle_id` -> 400 `{"error":"New Cycle Id is required"}`; target missing/completed or other failures -> 400 `{"error": "<text>"}` (exact texts `UNCONFIRMED`). Moves only work items whose state group is backlog/unstarted/started (not completed/cancelled) to the new cycle and stores a progress snapshot on the old cycle (shape `UNCONFIRMED`).

## 10. Modules
| Method + path | Status |
|---|---|
| GET/POST `/workspaces/{ws}/projects/{pid}/modules/` | 200 paginated (archived excluded) / 201 |
| GET `.../modules-lite/` | 200 paginated, no metrics; order_by allowlist created_at, updated_at, name, start_date, target_date, status, sort_order |
| GET/PATCH/DELETE `.../modules/{pk}/` | 200/200/204 |
| GET `.../modules/{module_id}/module-issues/` | 200 paginated work item objects |
| POST `.../modules/{module_id}/module-issues/` | 200, body `{"issues":[uuid,...]}` |
| DELETE `.../modules/{module_id}/module-issues/{issue_id}/` | 204 (GET on this path is not routed) |
| POST `.../modules/{pk}/archive/`, GET `.../archived-modules/`, DELETE `.../archived-modules/{pk}/unarchive/` | 204 / 200 / 204 |

**Module object** [S]:
| Field | Type | Null | Create | Update | Notes |
|---|---|---|---|---|---|
| id | uuid | no | RO | | |
| name | string(255) | no | **required** | yes | unique per project (live). Create dup -> 400 body `{"id":"<existing>","code":"MODULE_NAME_ALREADY_EXISTS","error":"Module with this name already exists","message":"Module with this name already exists"}`; PATCH dup -> 400 `{"error":"Module with this name already exists"}` |
| description | string | no | optional | yes | |
| description_text, description_html | json | yes | not writable via API | | returned |
| start_date, target_date | date | yes | optional | yes | start > target -> 400 "Start date cannot exceed target date" |
| status | enum | no | optional, default `planned` | yes | `backlog, planned, in-progress, paused, completed, cancelled` |
| lead | uuid | yes | optional | yes | |
| members | uuid[] | no | optional (write) | yes (replaces set) | non-project-members silently dropped; always returned as array of id strings |
| view_props, logo_props | json | no | RO | RO | `{}` default |
| sort_order | float | no | RO (server: `min-10000`, default 65535) | RO | |
| archived_at | datetime | yes | RO | RO | |
| external_source, external_id | string | yes | optional | yes | dup pair -> 409 `{"error":"Module with the same external id and external source already exists","id":..}` |
| total_issues, completed_issues, cancelled_issues, started_issues, unstarted_issues, backlog_issues | int | | RO | | per-group counts (exact filter rules `UNCONFIRMED`) |
| project, workspace + audit | | | RO | | |
Preconditions: project `module_view` true else 400 `non_field_errors: "Modules are not enabled for this project"`. PATCH on archived -> 400 `{"error":"Archived module cannot be edited"}`. DELETE: creator or project admin else 403 `{"error":"Only admin or creator can delete the module"}`.
**Add work items** empty -> 400 `{"error":"Issues are required"}`. Response 200 = plain array of ModuleIssue rows `{id, created_at, updated_at, deleted_at, created_by, updated_by, project, workspace, module, issue, sub_issues_count}` (source quirk: an issue already linked to another module is re-pointed to this module rather than added to a second one; exact semantics for multi-module `UNCONFIRMED`).

## 11. Issue comments
Paths: GET/POST `/workspaces/{ws}/projects/{pid}/work-items/{issue_id}/comments/`, GET/PATCH/DELETE `.../comments/{pk}/` (also under `/issues/`). Any active project member (incl. guests) may call. List paginated, default `-created_at`. Hidden issue (restricted guest) -> 404.
**Comment object** [S]:
| Field | Type | Null | Create | Update | Notes |
|---|---|---|---|---|---|
| id | uuid | no | RO | | |
| comment_html | string | no | optional, default `"<p></p>"` | yes | sanitised; invalid -> 400 `{"comment_html":"HTML content is not valid"}` |
| comment_json | json | | accepted on create/patch | | **never returned** |
| access | enum | no | optional, default `INTERNAL` | yes | `INTERNAL, EXTERNAL` |
| external_source, external_id | string | yes | optional | yes | create dup pair (project-wide) -> 409 `{"error":"Work item comment with the same external id and external source already exists","id":..}` |
| actor | uuid | yes | RO (= caller) | RO | |
| is_member | bool | | RO | | caller is active project member |
| attachments | url[] | no | RO | | `[]` |
| edited_at | datetime | yes | RO | RO | whether server sets it on edit `UNCONFIRMED` |
| parent | uuid | yes | RO (not in create fields) | | threading |
| description | uuid | yes | RO | | internal link |
| issue, project, workspace + audit | | | RO | | |
(`comment_stripped` is internal, not returned.) POST 201; PATCH 200 (author who is still an active member, or project/workspace admin; else 403 `{"error":"You are not allowed to edit this comment"}`); DELETE 204 (same rule; 403 `{"error":"You are not allowed to delete this comment"}`). PATCH dup external pair -> 409.

## 12. Issue links
Paths: GET/POST `.../work-items/{issue_id}/links/`, GET/PATCH/DELETE `.../links/{pk}/`. Project member read; role 15/20 write. List paginated, default `-created_at`.
| Field | Type | Null | Create | Update | Notes |
|---|---|---|---|---|---|
| id | uuid | no | RO | | |
| url | text | no | **required** | yes | create: must pass URL validation and start with `http://` or `https://` else 400 `{"url":["Invalid URL format."]}` / `"Invalid URL scheme."`; duplicate url on same issue -> 400 `{"error":"URL already exists for this Issue"}`. PATCH uses the looser full serializer (no scheme check) |
| title | string(255) | yes | optional | yes | a background job later fetches the page title (when it runs/what it fills: `UNCONFIRMED`) |
| metadata | json | no | RO on create | yes on PATCH | default `{}` |
| issue, project, workspace + audit | | | RO | | `issue_id` also accepted-in-list but read-only |
POST 201, PATCH 200, DELETE 204 (activity queued for each).

## 13. Issue activities (read-only)
Paths: GET `.../work-items/{issue_id}/activities/` (paginated), GET `.../activities/{pk}/`. Project member. Default `order_by=created_at` (**ascending**); allowlist created_at, updated_at (leading `-` ok). Rows with `field` in (`comment`, `vote`, `reaction`, `draft`) are hidden. Detail 404 body: `{"message":"Activity not found.","code":"NOT_FOUND"}`.
| Field | Type | Null |
|---|---|---|
| id | uuid | no |
| verb | string | no; docs: `created`/`updated`; `deleted` also plausible `UNCONFIRMED` |
| field | string | yes (null for create); values such as state, priority, name, description, assignees, labels, parent, dates, cycles, modules, link `UNCONFIRMED` (generator task not read) |
| old_value, new_value | string | yes |
| comment | string | no; human text generated by server e.g. "created the work item" |
| attachments | url[] | no |
| old_identifier, new_identifier | uuid | yes |
| epoch | float | yes (unix seconds) |
| issue, issue_comment, actor | uuid | issue_comment yes; actor yes |
| project, workspace, created_at, updated_at, deleted_at | | |
(`created_by`/`updated_by` excluded.) Activity rows are produced asynchronously by a background task after every write (create/update/delete of work items, links, comments, cycle/module membership); exact text/field vocabulary `UNCONFIRMED`.

## 14. Members and current user
UserLite object [S]: `{id uuid, first_name str, last_name str, email str, avatar str, avatar_url str|null, display_name str}`.
| Method + path | Status | Response |
|---|---|---|
| GET `/users/me/` | 200 | UserLite of the key owner |
| GET `/workspaces/{ws}/members/` | 200 | plain array of UserLite + `role` (int 20/15/5). Workspace admin only. Not paginated, **not filtered by is_active** [S]. 400 `{"error":"Provided workspace does not exist"}` for bad slug |
| GET `/workspaces/{ws}/members-lite/` | 200 paginated | items `{id, first_name, last_name, email, avatar, avatar_url, display_name, role, is_active, is_bot}` (id = user id); 404 for bad slug |
| GET `/workspaces/{ws}/projects/{pid}/members/` (alias `/project-members/`) | 200 | plain array of UserLite (no role) for all ProjectMember rows incl. inactive [S]; project member read |
| GET `.../projects/{pid}/project-members-lite/` | 200 paginated | same item shape as members-lite but project role; 404 if workspace/project missing |
| POST `.../members/` | 201 | body `{"member": user uuid (required), "role": 5|15|20 (default 5)}` -> `{"id": ProjectMember id, "member": uuid, "role": int}`. Member must be an active workspace member; a workspace guest cannot be given role 15/20; other roles -> 400 field errors. Project admin only |
| GET `.../members/{pk}/` | 200 | UserLite (pk = ProjectMember id) |
| PATCH `.../members/{pk}/` | 200 | `{id, member, role}`; role rules as above |
| DELETE `.../members/{pk}/` | 204 | soft: sets `is_active=false` |
Docs also list "remove workspace member" and other member endpoints [D]; not in source urls -> out of scope.

## 15. Gaps for the implementer
- Rate-limit numeric default, 429 body, 401-vs-403 on bad key, datetime string format (microseconds, `Z` vs offset) - `UNCONFIRMED`.
- Activity vocabulary (field names, comment text) - requires reading `bgtasks/issue_activities_task.py`.
- Link title crawler, cycle transfer snapshot shape, estimate sums - `UNCONFIRMED`.
- OAuth Bearer / scopes (`projects.work_items:read` etc. in docs) not in OSS `apps/api` auth class - skip unless needed.
- Work-item `type` key in responses, `module` field on work item (docs list it) not in source serializer - `UNCONFIRMED`.
- Webhooks fire from these writes (not in scope).

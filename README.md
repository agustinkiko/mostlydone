# MostlyDone

MostlyDone is a self-hosted project tracker. It is one Go binary with a
SQLite file and an embedded web UI. It speaks the shapes of Plane's public v1
REST API and has an MCP server. The target footprint: under 50 MB idle memory,
a container image under 30 MB, and an issue list of 10k issues (50 per page)
in under 20 ms of server time.

It covers projects, work items, states, priorities, labels, cycles, modules,
comments, activity, and list and board views.

## Quickstart

### From source

You need Go and Node (npm).

```sh
make build
./mostlydone
```

The server listens on `:8080` and stores data in `./mostlydone.db`. Change
both with environment variables:

```sh
MOSTLYDONE_ADDR=127.0.0.1:8080 MOSTLYDONE_DB=/var/lib/mostlydone/mostlydone.db ./mostlydone
```

### Docker

There is no published image yet. Build it yourself:

```sh
docker build -t mostlydone .
docker run -d --name mostlydone -p 8080:8080 -v mostlydone-data:/data mostlydone
```

The image keeps the database at `/data/mostlydone.db` and runs as a non-root
user (uid 65532). Mount a volume on `/data`.

## First run and the setup token

On first start with no users, the server creates a one-time setup token. It
logs the token once and writes it to `<MOSTLYDONE_DB>.setup-token`
(for example `./mostlydone.db.setup-token`, mode 0600). Open the web UI and
enter the token in the setup form along with your email, password and name.

The token is cleared and the file is removed after setup succeeds. A wrong
token gets 403 and counts against the login throttle.

To skip the token, set `MOSTLYDONE_ADMIN_EMAIL` and `MOSTLYDONE_ADMIN_PASSWORD`.
If no users exist, the server creates that admin at start and no setup token
is made.

The default address listens on all interfaces, so containers work. On bare
metal behind a local reverse proxy, set `MOSTLYDONE_ADDR=127.0.0.1:8080`.

## Config

| Name | Default | Meaning |
| --- | --- | --- |
| `MOSTLYDONE_ADDR` | `:8080` | Listen address. |
| `MOSTLYDONE_DB` | `./mostlydone.db` | SQLite file path. Also the default `-db` for `backup` and `restore`. |
| `MOSTLYDONE_ADMIN_EMAIL` | unset | With the password, creates the first admin at start if no users exist. |
| `MOSTLYDONE_ADMIN_PASSWORD` | unset | Password for that first admin. |
| `MOSTLYDONE_WORKSPACE_SLUG` | `main` | Slug of the workspace created at first start. |
| `MOSTLYDONE_SECURE_COOKIES` | `false` | Set `true` behind HTTPS. |
| `MOSTLYDONE_RATE_LIMIT` | unset (off) | API requests per minute per credential. 0 or unset turns the limiter off. |
| `MOSTLYDONE_RATE_LIMIT_BURST` | the per-minute value | Token bucket size: how many requests a client may spend at once. |
| `MOSTLYDONE_CLIENT_IP_HEADER` | unset | Header with the real client address behind a proxy, such as `CF-Connecting-IP`. Only the login throttle uses it. Set it only when all traffic comes through that proxy. |

## Upgrades and migrations

Migrations run at startup. Before it applies migrations to an existing
database, the server saves a snapshot to `<db>.pre-v<N>.bak`. If the database
schema is newer than the binary, startup fails and leaves the database
untouched. Upgrade MostlyDone or restore a backup.

## Backups

Back up while the server runs:

```sh
mostlydone backup [-db PATH] [-force] DEST
```

Restore with the server stopped:

```sh
mostlydone restore [-db PATH] [-force] SRC
```

`backup` writes one self-contained `.db` file and checks it before it renames
it into place. `restore` validates `SRC` and refuses to run while a server holds
`<db>.lock`. With `-force` it replaces an existing database and moves the old
one aside to `<db>.pre-restore-<UTC timestamp>.bak`. See
[docs/OPERATIONS.md](docs/OPERATIONS.md) for cron, Kubernetes and the restore
runbook.

## API tokens and MCP

Create an API token in the web UI under workspace settings, "API tokens &
MCP". The UI shows the token once. MostlyDone stores only a hash.

Send the token in the `X-API-Key` header on REST calls:

```sh
curl -H "X-API-Key: $TOKEN" http://localhost:8080/api/v1/workspaces/main/projects/
```

The MCP server is at `/mcp` and uses streamable HTTP. Send the same token as
`Authorization: Bearer <token>`. A generic client config looks like this. The
exact keys depend on your client:

```json
{
  "mcpServers": {
    "mostlydone": {
      "type": "http",
      "url": "http://localhost:8080/mcp",
      "headers": { "Authorization": "Bearer <token>" }
    }
  }
}
```

Tools: `list_projects`, `create_project`, `list_work_items`, `get_work_item`,
`create_work_item`, `update_work_item`, `add_comment`, `list_states`,
`list_labels`, `create_label`, `list_cycles`, `create_cycle`, `list_modules`,
`create_module`, `list_members`. Tools take readable references such as a
project identifier, `PROJ-12`, or a user email.

## Plane API compatibility

MostlyDone follows the field shapes of Plane's public v1 API for projects,
states, work items, labels, cycles, modules, comments, links, activities and
members. Both `/issues/` and `/work-items/` paths work. Field-level notes are
in [docs/plane-api-reference.md](docs/plane-api-reference.md).

Deliberate differences:

- `per_page` defaults to 100 and has a maximum of 250.
- Deleting a project moves it to a 30-day trash. Admins can list, restore or
  purge trashed projects through the API and under workspace settings. Other
  resources are hard-deleted.
- Rate limiting is off by default. When on, it applies per credential and
  adds `X-RateLimit-*` headers.
- Only `projects-lite`, `cycles-lite` and `modules-lite` exist, as read-only
  lists. Other `-lite` endpoints, archive, transfer and summary endpoints are
  not implemented.
- Activity `epoch` is an integer in unix seconds.
- Out-of-scope fields come back with Plane's default values and are ignored
  on write.

MostlyDone is not affiliated with Plane and shares no code with it. The full
list of rulings is in [SPEC.md](SPEC.md).

## Development

```sh
make build   # install UI dependencies, build the UI, build ./mostlydone
make test    # go test ./... plus the web typecheck and tests
```

CI runs `make build`, `gofmt -l .`, `go vet ./...` and `make test` on every
push and pull request.

## License

MIT. See [LICENSE](LICENSE).

# MostlyDone

<!-- Facts about this repo only. All rules and workflow come from estack.
     Do not copy estack rules here. -->

Context: personal

## Deploy targets

estack has no Kubernetes file in `plugins/core/skills/deploy/platforms/`.
Agents never apply manifests or touch the cluster. Kiko applies prod by hand.

| Env | Platform | Target | URL | Health path |
|---|---|---|---|---|
| prod | k8s (manual; see deploy/CUTOVER.md) | `deploy/mostlydone.yaml` | https://mostlydone.jericoagustin.com | `/healthz` |

There is no shared dev environment. Run the binary locally (see Ports).

## Checks

- `make test`: runs `go test ./...`, then `cd web && npm run typecheck && npm test`
- `make build`: builds the UI and the static binary
- `go vet ./...`

## Repo rules

- `SPEC.md` is the source of truth. If code and SPEC disagree, fix the code
  or change the SPEC ruling in the same branch, with the reason.
- Clean room. Plane is AGPL-3.0. Never copy, fetch, or read Plane source.
  Match API shapes only, from `docs/plane-api-reference.md`.
- Ports: API on `MOSTLYDONE_ADDR=:8030`, Vite dev on 3030. Run
  `/workspace/ops/check-ports.sh` before starting a server. Use a temp DB
  such as `/tmp/md-011.db` and stop the server when done.
- Engine: AI-assisted coding and writing goes through
  `claude -p --model sonnet`. Sonnet only, never Opus.
- 3-strike rule: if a build, test, or lint loop fails 3 times, stop. Add an
  entry to `/workspace/ops/blockers.md` (same format as existing entries,
  PHT timestamp, owner MostlyDone-Lead) and report it.

## Marketing (personal context)

- Audience: self-hosters and small teams on Plane-style trackers
- Voice: plain, no hype
- Channels: r/selfhosted, Show HN, X

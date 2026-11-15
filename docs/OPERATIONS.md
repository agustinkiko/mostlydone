# Operations

## Running

`mostlydone` with no arguments serves. Configuration is by environment
variable; see "Config" in `SPEC.md`. The data lives in one SQLite file
(`MOSTLYDONE_DB`, default `./mostlydone.db`, `/data/mostlydone.db` in the image).

The server takes an exclusive lock on `<db>.lock` while it runs. A second
server on the same file, or `restore` against a running server, fails with
"database is in use by another MostlyDone process". The lock is released when
the process exits, so a crash never leaves it stuck.

## Backups

```
mostlydone backup [-db PATH] [-force] DEST
```

- Safe while the server runs. It reads a consistent snapshot with
  `VACUUM INTO`; it does not take the lock, migrate or modify the source.
- `-db` defaults to `$MOSTLYDONE_DB`, then `./mostlydone.db`.
- Refuses to overwrite `DEST` unless `-force`.
- Writes to a temp file next to `DEST`, runs `PRAGMA integrity_check` and reads
  the schema version from the copy, then renames into place. A failed backup
  leaves nothing under `DEST`. DEST's directory is created if missing.
- Prints one line: path, size, schema version, duration. Exit code is non-zero on
  any failure, so cron and Kubernetes see it.
- The backup is one self-contained `.db` file (no `-wal`/`-shm`). It can briefly
  need as much extra disk as the database size.

### Cron (bare metal / VM)

```
# daily 02:15, keep 14 days
15 2 * * * mostlydone backup -db /var/lib/mostlydone/mostlydone.db /var/backups/mostlydone/md-$(date -u +\%Y\%m\%dT\%H\%M\%SZ).db && find /var/backups/mostlydone -name 'md-*.db' -mtime +14 -delete
```

### Kubernetes

The image is distroless: there is no shell, `tar` or `cp`. Call the binary
directly.

Simplest: exec into the running pod.

```
kubectl exec deploy/paperplane -- /mostlydone backup /data/backups/md-$(date -u +%Y%m%dT%H%M%SZ).db
```

`backup` creates the destination directory if needed. Backups stored on the same volume do not protect against losing the volume, so
copy them off-box too (below).

CronJob: it must mount the same PVC. The volume is ReadWriteOnce, and with
`local-path` it is a directory on one node, so the Job pod must be scheduled on
the same node as the server (pod affinity to the server pod, or `nodeName`).
Without that the Job stays Pending or mounts an empty volume.

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: paperplane-backup
spec:
  schedule: "15 2 * * *"
  concurrencyPolicy: Forbid
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: OnFailure
          securityContext: {runAsUser: 65532, runAsGroup: 65532, fsGroup: 65532}
          affinity:
            podAffinity:
              requiredDuringSchedulingIgnoredDuringExecution:
                - topologyKey: kubernetes.io/hostname
                  labelSelector:
                    matchLabels: {app: paperplane}
          containers:
            - name: backup
              image: mostlydone:20261115   # same image as the server
              command: ["/mostlydone", "backup", "-db", "/data/mostlydone.db", "/data/backups/md-latest.db"]
              volumeMounts:
                - {name: data, mountPath: /data}
          volumes:
            - name: data
              persistentVolumeClaim: {claimName: paperplane-data}
```

A fixed `DEST` fails once it exists. Either add `-force` (always keeps one
latest copy, rotated by your off-box job) or use `kubectl exec` with a
timestamped name. Timestamps in a CronJob need a shell, which this image lacks.

### Retention and off-box copy

- Suggested: daily backups, keep 14 daily and 8 weekly. Delete by age.
- Copy backups off the machine (object storage, another host). The backup is a
  single file, so `rclone copy`, `scp` or `restic backup` all work. The image has
  no `tar`, `cat` or shell, so `kubectl cp` does not work: run the copy job from
  a helper pod that mounts the PVC (same node for local-path), or read the
  volume's directory on the node.
- Test a restore somewhere else occasionally.

### Verifying a backup

`backup` already checks integrity and prints the schema version. To re-check a
file later, `mostlydone restore` validates it before touching anything, or use
any sqlite3: `sqlite3 FILE 'PRAGMA integrity_check; SELECT MAX(version) FROM schema_migrations;'`
(expects `ok` and a number).

## Restore

```
mostlydone restore [-db PATH] [-force] SRC
```

Offline only. It:

1. Validates `SRC`: integrity_check is `ok`, has `schema_migrations`, and its
   version is not newer than this binary's migrations.
2. Takes the `<db>.lock` lock; refuses if the server is running.
3. If the target DB exists, requires `-force` (without it, it prints the aside
   path and stops). Then checkpoints the current DB (`wal_checkpoint(TRUNCATE)`),
   moves it to `<db>.pre-restore-<UTC timestamp>.bak`, and removes the stale
   `-wal` and `-shm`.
4. Copies `SRC` to a temp file beside the DB and renames it into place.
   If the copy fails the old DB is put back.

Runbook:

1. Stop the server: `kubectl scale deploy/paperplane --replicas=0` (or stop the
   service/container). Confirm the pod is gone.
2. Run the restore against the data volume. In Kubernetes use a one-off pod with
   the same image and the PVC mounted (same node for local-path), command
   `/mostlydone restore -force -db /data/mostlydone.db /data/backups/FILE.db`.
   Check the output names the aside file and says `restore ok`.
3. Start the server: `kubectl scale deploy/paperplane --replicas=1`. The server
   applies any newer migrations on start.
4. Verify: `GET /healthz` returns `ok`; sign in and check projects and work item
   counts look right for the backup's date.
5. Roll back if wrong: stop the server, then (as the same user) move the aside file
   back: `mv mostlydone.db mostlydone.db.bad; mv mostlydone.db.pre-restore-<ts>.bak mostlydone.db`,
   remove any `mostlydone.db-wal`/`-shm`, start. The aside file is a complete
   standalone DB, so you can also run `restore -force` with it as `SRC`.
6. Once satisfied, delete the aside file and `.bad` file to free space.

## Files next to the database

| File | Meaning |
| --- | --- |
| `mostlydone.db` | The data. This is what matters. |
| `mostlydone.db-wal` | Recent committed writes not yet merged into the `.db`. Must stay with the `.db` while the server runs; do not copy the `.db` alone from a live server, use `backup` |
| `mostlydone.db-shm` | WAL index. Rebuilt automatically; safe to ignore |
| `mostlydone.db.lock` | Lock file, empty. Safe to leave or delete when no process runs |
| `mostlydone.db.pre-restore-*.bak` | Previous DB moved aside by `restore -force` |
| `pre-v*.bak` | Automatic snapshot taken just before pending migrations are applied to an existing DB (added by MD-004, branch `feature/agustinkiko-setup-token-migration-guard`). A valid restore source if an upgrade goes wrong. Prune old ones by hand |

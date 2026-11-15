# Production cutover runbook: rename to MostlyDone object names (PV rebind, no data copy)

**Who runs this:** Kiko, by hand, from a machine with `kubectl` access to the homelab k3s cluster and a checkout of this repo (it needs commit `99728c0` for the old names and this branch for the new manifest). No agent runs any of this.

**What it does:** moves the live install at `mostlydone.jericoagustin.com` from the pre-rename Kubernetes objects (namespace, Deployment, Service, Ingress, PVC, Secret and DB file all carry the app's old name) to the names in `deploy/mostlydone.yaml`. The data is **not copied**: the existing PersistentVolume is detached from the old PVC and re-attached to a new PVC in the new namespace. The SQLite file is renamed in place while the app is stopped.

**Downtime:** one maintenance window of about **1.5–2 minutes** (budget 5). Pre-flight has no downtime.

> **The old hostname goes away.** The pre-rename Ingress also served `$OLD.jericoagustin.com` (`OLD` is set in step 0). That rule is not in the new manifest, so the old hostname stops working at T0 (it will return Traefik's 404 through the tunnel) and does not come back. This is intended. Only `mostlydone.jericoagustin.com` (and the future `mostlydone.app`) is served afterwards.

Old names are never written literally in this repo. They come from `git show 99728c0:...` or from the live cluster, held in shell variables.

---

## Contents

0. [Variables](#0-variables-run-in-every-shell-you-use)
1. [Pre-flight (no downtime)](#1-pre-flight-no-downtime), about 15–20 min
2. [Maintenance window](#2-maintenance-window-t0--scale-down), about 1.5–2 min
3. [Start and verify](#3-verify-before-you-call-it-done)
4. [Rollback](#4-rollback), with timed decision points
5. [Cleanup](#5-cleanup-after-verification-not-the-same-day)
6. [Cloudflare tunnel](#6-cloudflare-tunnel)
7. [Why it is done this way](#7-why-it-is-done-this-way)

---

## 0. Variables (run in every shell you use)

Run from the repo root on this branch.

```sh
# The old name, from the commit before the rename (prints nothing secret).
OLD=$(git show 99728c0:deploy/mostlydone.yaml | awk '/^kind: Namespace/{f=1} f&&/^  name:/{print $2; exit}')
OLD_NS=$OLD
OLD_DEPLOY=$OLD
OLD_PVC=$OLD-data
OLD_SECRET=$OLD-admin
OLD_DB_FILE=$OLD.db          # file name inside /data on the volume

NEW_NS=mostlydone
NEW_PVC=mostlydone-data
NEW_SECRET=mostlydone-admin
NEW_DB_FILE=mostlydone.db

SQLITE_IMAGE=keinos/sqlite3:3.54.1   # Alpine + sqlite3 3.54.1; needs a Docker Hub pull on the node
HOST=https://mostlydone.jericoagustin.com
BACKUP_DIR=$HOME/mostlydone-backups/$(date +%Y%m%d-%H%M)
mkdir -p "$BACKUP_DIR"
echo "OLD=$OLD"
```

Expected: `OLD=` followed by the old app name. If it prints `OLD=` with nothing after it, stop: the checkout is missing commit `99728c0` (`git fetch origin main`).

### Helper functions

These start a short-lived pod with `sqlite3` that mounts a PVC. It runs as uid 65532, the same uid as the app, so any file it touches keeps the app's ownership. Paste these into the same shell.

```sh
# helper_pod NAMESPACE PVC POD_NAME
helper_pod() {
kubectl -n "$1" apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: $3
spec:
  restartPolicy: Never
  automountServiceAccountToken: false
  securityContext:
    runAsNonRoot: true
    runAsUser: 65532
    runAsGroup: 65532
    seccompProfile: {type: RuntimeDefault}
  containers:
    - name: sqlite
      image: $SQLITE_IMAGE
      command: ["sleep", "3600"]
      env:
        - {name: HOME, value: /tmp}
        - {name: TMPDIR, value: /tmp}
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities: {drop: ["ALL"]}
      volumeMounts:
        - {name: data, mountPath: /data}
        - {name: backup, mountPath: /backup}
        - {name: tmp, mountPath: /tmp}
  volumes:
    - name: data
      persistentVolumeClaim: {claimName: $2}
    - name: backup
      emptyDir: {}
    - name: tmp
      emptyDir: {}
EOF
kubectl -n "$1" wait --for=condition=Ready pod/"$3" --timeout=120s
}

# mv_db NAMESPACE POD FROM_FILE TO_FILE
# Only for a STOPPED app. Folds any WAL into the main file, checks it, then renames.
# Refuses if TO already exists or if the WAL could not be emptied.
mv_db() {
kubectl -n "$1" exec -i "$2" -- sh -s "$3" "$4" <<'EOF'
set -eu
cd /data
FROM=$1; TO=$2
[ -f "$FROM" ] || { echo "ABORT: $FROM not found"; exit 1; }
[ ! -e "$TO" ] || { echo "ABORT: $TO already exists"; exit 1; }
[ ! -e "$TO-wal" ] && [ ! -e "$TO-shm" ] || { echo "ABORT: stray $TO-wal or $TO-shm"; exit 1; }
sqlite3 "$FROM" ".timeout 10000" "PRAGMA wal_checkpoint(TRUNCATE);" "PRAGMA quick_check;"
[ ! -s "$FROM-wal" ] || { echo "ABORT: $FROM-wal still has data"; exit 1; }
rm -f "$FROM-wal" "$FROM-shm"
mv "$FROM" "$TO"
ls -ln /data
echo "RENAMED OK"
EOF
}
```

---

## 1. Pre-flight (no downtime)

Everything in this section is read-only for the running app, or creates new objects it does not use.

### 1.1 Confirm the cluster and the old objects

```sh
kubectl config current-context
kubectl get nodes -o wide
kubectl -n "$OLD_NS" get deploy,svc,ingress,pvc,secret
```

Expected: your homelab context and one `Ready` node. In the old namespace you should see `deployment.apps/$OLD` at `1/1`, `service/$OLD`, `ingress.networking.k8s.io/$OLD` with hosts `mostlydone.jericoagustin.com`, `mostlydone.app` and the old hostname, `persistentvolumeclaim/$OLD-data` `Bound` on `local-path`, and `secret/$OLD-admin`. If any name differs, **stop**: this runbook assumes the names from `99728c0`.

Nothing else should claim the hostname:

```sh
kubectl get ingress -A | grep -i jericoagustin
kubectl get ingressroute -A 2>/dev/null | grep -i mostlydone || true
```

Expected: only the ingress in `$OLD_NS`, and no IngressRoute.

### 1.2 Discover the PV, node, capacity, image and DB path

```sh
PV=$(kubectl -n "$OLD_NS" get pvc "$OLD_PVC" -o jsonpath='{.spec.volumeName}')
kubectl get pv "$PV" -o jsonpath='{"pv: "}{.metadata.name}{"\nclass: "}{.spec.storageClassName}{"\ncapacity: "}{.spec.capacity.storage}{"\naccess: "}{.spec.accessModes}{"\nreclaim: "}{.spec.persistentVolumeReclaimPolicy}{"\nmode: "}{.spec.volumeMode}{"\nnode: "}{.spec.nodeAffinity.required.nodeSelectorTerms[0].matchExpressions[0].values[0]}{"\npath: "}{.spec.hostPath.path}{.spec.local.path}{"\nclaim: "}{.spec.claimRef.namespace}/{.spec.claimRef.name}{"\n"}'
PV_CAP=$(kubectl get pv "$PV" -o jsonpath='{.spec.capacity.storage}')
[ -n "$PV" ] && [ -n "$PV_CAP" ] && echo "PV=$PV PV_CAP=$PV_CAP" || echo "STOP: PV not found"
kubectl -n "$OLD_NS" get deploy "$OLD_DEPLOY" -o jsonpath='{"image: "}{..image}{"\ndb: "}{.spec.template.spec.containers[0].env[?(@.name=="MOSTLYDONE_DB")].value}{"\nuid: "}{.spec.template.spec.securityContext.runAsUser}{"\n"}'
grep -n 'image: mostlydone:' deploy/mostlydone.yaml
```

Expected:

```
pv: pvc-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
class: local-path
capacity: 1Gi
access: ["ReadWriteOnce"]
reclaim: Delete                 <- changed to Retain in 1.6
mode: Filesystem
node: <your node name>
path: /var/lib/rancher/k3s/storage/pvc-..._<old ns>_<old pvc>   (the folder name keeps the old name; that is fine)
claim: <old ns>/<old ns>-data
image: mostlydone:20261115
db: /data/<old name>.db
uid: 65532
```

**Write down `PV` and the node name.** You need them for rollback.

Gates, so stop if any of these fail:
- `capacity` must equal `1Gi`, the request in `deploy/mostlydone.yaml`. If it is different, change `storage:` in the manifest to the same value first, or a later `kubectl apply` will try to resize the claim and fail.
- `image` must equal the `image:` line in `deploy/mostlydone.yaml` (`mostlydone:20261115`). **Do not change the image in the same window.** Same binary means same schema, so the start-up migration has nothing to do.
- `db` must be `/data/$OLD_DB_FILE` (check with `echo /data/$OLD_DB_FILE`).
- `uid` must be `65532`, the same as the new manifest.

### 1.3 Online backup while the app runs

The app image is distroless (no shell, no `sqlite3`), so a helper pod mounts the same PVC on the same node and takes the backup.

```sh
helper_pod "$OLD_NS" "$OLD_PVC" md-backup
kubectl -n "$OLD_NS" exec md-backup -- ls -ln /data
```

Expected: `pod/md-backup created`, then `pod/md-backup condition met` (the first image pull can take 10–30 s). The listing shows `$OLD.db`, usually with `$OLD.db-wal` and `$OLD.db-shm` next to it, **all owned by `65532 65532`**. The main `.db` file may be much smaller than you would expect, because recent writes sit in the `-wal` file. That is exactly why a plain file copy is not a backup.

Take the backup with SQLite's online backup API from a **read-only connection**. It reads a consistent snapshot while the app keeps writing:

```sh
kubectl -n "$OLD_NS" exec -i md-backup -- sh -s "$OLD_DB_FILE" <<'EOF'
set -eu
sqlite3 -readonly "/data/$1" ".timeout 10000" ".backup /backup/mostlydone-preflight.db"
sqlite3 /backup/mostlydone-preflight.db "PRAGMA integrity_check;" \
  "SELECT 'users', count(*) FROM users UNION ALL SELECT 'projects', count(*) FROM projects
   UNION ALL SELECT 'issues', count(*) FROM issues UNION ALL SELECT 'schema', max(version) FROM schema_migrations;"
sha256sum /backup/mostlydone-preflight.db
EOF
```

Expected:

```
ok
users|<n ≥ 1>
projects|<your project count>
issues|<your work item count>
schema|1
<sha256>  /backup/mostlydone-preflight.db
```

Copy it off the node and check the copy on your machine:

```sh
kubectl -n "$OLD_NS" cp md-backup:/backup/mostlydone-preflight.db "$BACKUP_DIR/mostlydone-preflight.db" -c sqlite
chmod 600 "$BACKUP_DIR/mostlydone-preflight.db"
shasum -a 256 "$BACKUP_DIR/mostlydone-preflight.db"      # Linux: sha256sum
sqlite3 "$BACKUP_DIR/mostlydone-preflight.db" "PRAGMA integrity_check;" "SELECT count(*) FROM projects;" "SELECT count(*) FROM issues;"
# No local sqlite3? docker run --rm -v "$BACKUP_DIR:/b" keinos/sqlite3:3.54.1 sqlite3 /b/mostlydone-preflight.db "PRAGMA integrity_check;"
kubectl -n "$OLD_NS" delete pod md-backup
```

Expected: `kubectl cp` may print `tar: removing leading '/' from member names`, which is harmless. The checksum matches the one printed in the pod, `integrity_check` prints `ok`, and the counts match. Write the counts down (**projects = P, issues = I**). Step 3 compares against them.

Restore test: the copy *is* a complete, self-contained database file, so `integrity_check` = `ok` plus matching counts on the copy is the restore check. For extra assurance you can boot it locally: `MOSTLYDONE_ADDR=127.0.0.1:18080 MOSTLYDONE_DB=/tmp/restore-test.db ./mostlydone` after `cp` of the backup to `/tmp/restore-test.db`. Then `curl -s 127.0.0.1:18080/auth/me` shows `"setup_required":false`. Do not set `MOSTLYDONE_ADMIN_*` for that test.

Keep this backup for **30 days**. Do the window soon after this step (ideally within 15 minutes, with no edits in between), because anything written after the backup is only in the live volume.

> **Later:** MD-003 adds `mostlydone backup DEST` (online `VACUUM INTO` from a read-only connection, plus integrity check and atomic rename). Once a release with it is running, that command replaces this helper-pod step. It is not used here because it is not in the running image, and changing the image during this cutover would add risk.

### 1.4 Create the new namespace and Secret (no effect on the running app)

The new Deployment cannot start without `mostlydone-admin` (keys `email`, `password`). Copy the existing Secret's data across without ever printing it:

```sh
kubectl create namespace "$NEW_NS"
kubectl -n "$OLD_NS" get secret "$OLD_SECRET" -o json \
  | jq --arg ns "$NEW_NS" --arg name "$NEW_SECRET" \
      '{apiVersion, kind, type, data, metadata: {name: $name, namespace: $ns}}' \
  | kubectl create -f -
kubectl -n "$NEW_NS" get secret "$NEW_SECRET" -o json | jq '.data | keys'
```

Expected: `namespace/mostlydone created`, `secret/mostlydone-admin created`, then `["email","password"]` (key names only, no values).

Note: these values are only used when the database has **no users**. On the existing database they are ignored. That is also why step 3 has a check for the case where the app silently started on an empty volume.

### 1.5 Check the tunnel target (read-only)

See [section 6](#6-cloudflare-tunnel). On the server: `python3 deploy/tunnel-hostname.py check mostlydone.jericoagustin.com` should print `has mostlydone.jericoagustin.com: True`. Nothing needs to change.

### 1.6 Set the PV's reclaim policy to Retain

This is the safety net for the whole cutover. With `Delete`, deleting the old PVC in step 2.3 would make local-path **erase the data directory**.

```sh
kubectl patch pv "$PV" -p '{"spec":{"persistentVolumeReclaimPolicy":"Retain"}}'
kubectl get pv "$PV" -o jsonpath='{.spec.persistentVolumeReclaimPolicy}{"\n"}'
```

Expected: `persistentvolume/pvc-... patched`, then `Retain`. **Do not continue unless it prints `Retain`.**

### 1.7 Pre-flight checklist

- [ ] `PV`, node name, P and I written down
- [ ] Backup on your machine, `integrity_check` = `ok`, checksum matches
- [ ] `mostlydone` namespace and Secret exist (keys only checked)
- [ ] PV reclaim policy = `Retain`
- [ ] Image in the manifest = running image; capacity = `1Gi`
- [ ] The sqlite helper image is cached on the node (it was pulled in 1.3)
- [ ] `curl -s -o /dev/null -w '%{http_code}\n' $HOST/healthz` prints `200`

---

## 2. Maintenance window (T0 = scale-down)

Run these back to back in the same shell (variables from step 0, plus `PV` and `PV_CAP` from 1.2). The times are cumulative targets.

### 2.1 T0: stop the old app (≈ 0:15)

```sh
date
kubectl -n "$OLD_NS" scale deploy/"$OLD_DEPLOY" --replicas=0
kubectl -n "$OLD_NS" wait --for=delete pod -l app="$OLD" --timeout=60s
```

Expected: `deployment.apps/... scaled`, then `pod/... condition met`, or `error: no matching resources found` if the pod was already gone, which is also fine. On SIGTERM the app shuts down cleanly and SQLite folds the WAL back into the main file. The site now returns 502/404.

### 2.2 Remove the old Ingress (≈ 0:20)

Both the old and the new Ingress would claim `mostlydone.jericoagustin.com`, and Traefik would pick between them arbitrarily. This step also ends the old hostname.

```sh
kubectl -n "$OLD_NS" delete ingress "$OLD"
```

Expected: `ingress.networking.k8s.io "..." deleted`.

### 2.3 Release the volume from the old claim (≈ 0:30)

```sh
kubectl get pv "$PV" -o jsonpath='{.spec.persistentVolumeReclaimPolicy}{"\n"}'   # must print Retain
kubectl -n "$OLD_NS" delete pvc "$OLD_PVC" --wait=true --timeout=30s
kubectl get pv "$PV"
```

Expected: `Retain`, then `persistentvolumeclaim "..." deleted`, and the PV shows `STATUS Released`, `RECLAIM POLICY Retain`. If the delete hangs, a pod still mounts the claim (`kubectl -n "$OLD_NS" get pods`). Delete that pod; never remove the finalizer by hand.

### 2.4 Re-point the PV at the new claim (≈ 0:35)

Replace the old `claimRef` with one that names the new claim, without a `uid`. This pre-binds the PV so no other claim can take it.

```sh
kubectl patch pv "$PV" --type merge -p \
  '{"spec":{"claimRef":{"namespace":"'"$NEW_NS"'","name":"'"$NEW_PVC"'","uid":null,"resourceVersion":null}}}'
kubectl get pv "$PV" -o jsonpath='{.status.phase} {.spec.claimRef.namespace}/{.spec.claimRef.name}{"\n"}'
```

Expected: `persistentvolume/... patched`, then `Available mostlydone/mostlydone-data`.

### 2.5 Create the new claim bound to that PV (≈ 0:45)

`kubectl create`, not `apply`, on purpose. Without an apply annotation, the later `kubectl apply -f deploy/mostlydone.yaml` will not try to remove `volumeName`, which is immutable.

```sh
kubectl create -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: $NEW_PVC
  namespace: $NEW_NS
spec:
  accessModes: ["ReadWriteOnce"]
  storageClassName: local-path
  volumeMode: Filesystem
  volumeName: $PV
  resources:
    requests:
      storage: $PV_CAP
EOF
kubectl -n "$NEW_NS" get pvc "$NEW_PVC"
kubectl get pv "$PV" -o jsonpath='{.status.phase} {.spec.claimRef.uid}{"\n"}'
```

Expected: `persistentvolumeclaim/mostlydone-data created`, then `STATUS Bound`, `VOLUME <PV>`, `CAPACITY 1Gi`, and the PV `Bound` with a uid. The PV's local-path `nodeAffinity` is untouched, so every pod using this claim is scheduled on the node that holds the data.

### 2.6 Rename the DB file while nothing is running (≈ 1:10)

```sh
helper_pod "$NEW_NS" "$NEW_PVC" md-rename
mv_db "$NEW_NS" md-rename "$OLD_DB_FILE" "$NEW_DB_FILE"
kubectl -n "$NEW_NS" delete pod md-rename --wait=true
```

Expected (image already cached, so the pod is ready in a few seconds):

```
0|0|0
ok
total ...
-rw-r--r--    1 65532    65532   <size>  ... mostlydone.db
RENAMED OK
pod "md-rename" deleted
```

The first `0` in `0|0|0` means the checkpoint was not blocked. `ok` is the quick check. Only `mostlydone.db` is left, owned by `65532`. If it prints `ABORT`, do not improvise: the file is untouched, so go to [rollback A](#rollback-a-rebind-to-the-old-install-about-3-min).

### 2.7 Start the new install (≈ 1:30)

```sh
kubectl apply -f deploy/mostlydone.yaml
kubectl -n "$NEW_NS" rollout status deploy/mostlydone --timeout=120s
```

Expected: warnings like `resource namespaces/mostlydone is missing the kubectl.kubernetes.io/last-applied-configuration annotation` for the namespace and PVC (harmless; they are patched automatically). Then `namespace/mostlydone configured`, `persistentvolumeclaim/mostlydone-data configured`, `deployment.apps/mostlydone created`, `service/mostlydone created`, `ingress.networking.k8s.io/mostlydone created`, and `deployment "mostlydone" successfully rolled out` within about 15 s.

---

## 3. Verify before you call it done

```sh
kubectl -n "$NEW_NS" get pods -o wide
kubectl -n "$NEW_NS" logs deploy/mostlydone
curl -s -o /dev/null -w '%{http_code}\n' "$HOST/healthz"
curl -s "$HOST/auth/me"; echo
```

Expected:
- The pod is `1/1 Running` on the node from 1.2.
- The only log line is `listening on :8080 (db /data/mostlydone.db, workspace "main")`, and **no `created admin` line**.
- `/healthz` returns `200`.
- `/auth/me` returns `{"role":null,"setup_required":false,"user":null,"workspace_slug":"main"}`.

**Important:** `setup_required:false` alone does not prove the old data was picked up. Because the Secret holds an admin email and password, the app creates a fresh admin on an **empty** database and then also reports `setup_required:false`. The real proof is the combination of:
1. no `created admin` in the logs, and
2. you log in at `https://mostlydone.jericoagustin.com` with your normal account and see all your projects (P) and work items. Optional: create and delete a throwaway comment to prove writes work.

If you see `created admin` or an empty workspace, the app is on an empty file. Go to rollback A now. Your data is still on the PV.

**Done at about T+2.** Note the time.

---

## 4. Rollback

### Decision points

| When | Condition | Action |
|---|---|---|
| Any step in section 2 | A step's output doesn't match "Expected" and the cause isn't obvious in 1 minute | Stop forward steps and go to rollback A |
| T+3 min | Pod not `Running`, or PVC not `Bound` | Look at `kubectl -n mostlydone describe pod/pvc` for at most 2 minutes |
| **T+5 min** | Not all of the section 3 checks green | **Rollback A, no more debugging.** Target: old install serving again by T+10 |
| T+15 min | Rollback A did not bring the old install back | Rollback B (restore from backup) |

### Rollback A: rebind to the old install (about 3 min)

Run only the parts that apply, in order. Each part is safe to skip if its "only if" isn't true.

```sh
# A1. Stop the new app (only if it was started).
kubectl -n "$NEW_NS" scale deploy/mostlydone --replicas=0 2>/dev/null
kubectl -n "$NEW_NS" wait --for=delete pod -l app=mostlydone --timeout=60s
kubectl -n "$NEW_NS" delete ingress mostlydone --ignore-not-found

# A2. Rename the file back (only if step 2.6 printed RENAMED OK).
helper_pod "$NEW_NS" "$NEW_PVC" md-rename
kubectl -n "$NEW_NS" exec md-rename -- ls -ln /data
mv_db "$NEW_NS" md-rename "$NEW_DB_FILE" "$OLD_DB_FILE"
kubectl -n "$NEW_NS" delete pod md-rename --wait=true
```

A2 expected: `RENAMED OK`, with only `$OLD.db` left. If it prints `ABORT: <old>.db already exists`, the rename never happened, or the new app created a second, empty `mostlydone.db` next to the real file. The real file is intact, and the old app ignores the extra file, so carry on.

```sh
# A3. Detach the PV from the new claim (only if step 2.5 ran).
kubectl get pv "$PV" -o jsonpath='{.spec.persistentVolumeReclaimPolicy}{"\n"}'   # must print Retain
kubectl -n "$NEW_NS" delete pvc "$NEW_PVC" --wait=true --timeout=30s

# A4. Pre-bind the PV back to the old claim name (only if step 2.4 ran).
kubectl patch pv "$PV" --type merge -p \
  '{"spec":{"claimRef":{"namespace":"'"$OLD_NS"'","name":"'"$OLD_PVC"'","uid":null,"resourceVersion":null}}}'

# A5. Re-create the old claim on that PV (only if step 2.3 ran).
kubectl create -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: $OLD_PVC
  namespace: $OLD_NS
spec:
  accessModes: ["ReadWriteOnce"]
  storageClassName: local-path
  volumeMode: Filesystem
  volumeName: $PV
  resources:
    requests:
      storage: $PV_CAP
EOF
kubectl -n "$OLD_NS" get pvc "$OLD_PVC"          # Bound to $PV

# A6. Restore the old Ingress and start the old app: re-apply the exact pre-rename manifest.
git show 99728c0:deploy/mostlydone.yaml | kubectl apply -f -
kubectl -n "$OLD_NS" rollout status deploy/"$OLD_DEPLOY" --timeout=120s
kubectl -n "$OLD_NS" logs deploy/"$OLD_DEPLOY"
curl -s -o /dev/null -w '%{http_code}\n' "$HOST/healthz"
```

A6 expected: `deployment ... successfully rolled out`. The logs show `listening on :8080 (db /data/<old>.db ...)` and no `created admin`, `/healthz` returns `200`, and you can log in and see your projects. The old hostname works again too. Leave the `mostlydone` namespace in place (it is empty apart from the Secret and a scaled-down Deployment) and work out what went wrong before trying again.

### Rollback B: restore from the pre-flight backup (last resort, about 5 min)

Use this only if the PV or its file is unusable. Anything written after the 1.3 backup is lost. **Leave the original PV alone** (it is `Retain`, so it stays on disk for later recovery). Run the A1 cleanup first, so that no `mostlydone` pod or PVC holds the PV, then:

```sh
# Park the original PV on a claim name that never exists, so the new claim below
# gets a fresh volume and cannot grab the original one.
kubectl -n "$NEW_NS" delete pvc "$NEW_PVC" --ignore-not-found --wait=true
kubectl patch pv "$PV" --type merge -p \
  '{"spec":{"claimRef":{"namespace":"'"$NEW_NS"'","name":"quarantined-original","uid":null,"resourceVersion":null}}}'
kubectl -n "$OLD_NS" scale deploy/"$OLD_DEPLOY" --replicas=0
kubectl -n "$OLD_NS" delete pvc "$OLD_PVC" --ignore-not-found --wait=true
kubectl create -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: $OLD_PVC
  namespace: $OLD_NS
spec:
  accessModes: ["ReadWriteOnce"]
  storageClassName: local-path
  resources:
    requests:
      storage: 1Gi
EOF
helper_pod "$OLD_NS" "$OLD_PVC" md-restore        # also makes local-path provision a fresh, empty PV
kubectl -n "$OLD_NS" cp "$BACKUP_DIR/mostlydone-preflight.db" md-restore:/backup/restore.db -c sqlite
kubectl -n "$OLD_NS" exec -i md-restore -- sh -s "$OLD_DB_FILE" <<'EOF'
set -eu
[ "$(sqlite3 /backup/restore.db 'PRAGMA integrity_check;')" = ok ] || { echo "ABORT: backup failed integrity_check"; exit 1; }
rm -f "/data/$1" "/data/$1-wal" "/data/$1-shm"
cp /backup/restore.db "/data/$1.tmp" && mv "/data/$1.tmp" "/data/$1"
ls -ln /data
echo "RESTORED OK"
EOF
kubectl -n "$OLD_NS" delete pod md-restore --wait=true
git show 99728c0:deploy/mostlydone.yaml | kubectl apply -f -
kubectl -n "$OLD_NS" rollout status deploy/"$OLD_DEPLOY" --timeout=120s
```

Expected: `RESTORED OK` with `<old>.db` owned by `65532`, a rollout success, `/healthz` returning 200, and your projects visible as of the backup time.

---

## 5. Cleanup (after verification, not the same day)

Wait at least **24 hours** of normal use on the new install, and take one fresh backup of it (repeat 1.3 against `$NEW_NS`/`$NEW_PVC`). Then:

```sh
kubectl -n "$OLD_NS" get all,pvc,secret        # expect: the scaled-down deployment (0/0), the service, the secret, no PVC
kubectl delete namespace "$OLD_NS"
kubectl get pv "$PV"                           # still Bound to mostlydone/mostlydone-data, Retain
```

- **Reclaim policy: keep `Retain` (recommended).** With `Retain`, deleting the namespace or PVC by mistake can never erase the database. The cost is that, if you ever do retire the volume, you delete the PV object and its folder on the node by hand. With a single irreplaceable SQLite file, that trade is worth it. Do not set it back to `Delete`.
- Keep the pre-flight backup for **30 days**, then delete it.
- On the node, the volume folder is still named after the old PVC (`/var/lib/rancher/k3s/storage/pvc-..._<old ns>_<old pvc>`). That is cosmetic. Do not rename it, because the PV points at that path.
- Move the importer files on the node from `~/$OLD/import` to `~/mostlydone/import` if you will ever run `plane-import-pod.yaml` again.
- Optionally remove the old hostname from Cloudflare (see section 6).

---

## 6. Cloudflare tunnel

`deploy/tunnel-hostname.py` manages the remotely-managed tunnel's public hostnames. Each rule points at **`http://127.0.0.1:30950`** (Traefik's NodePort on the node), with `httpHostHeader` set to the public hostname. It does **not** use a namespace-qualified service name (no `*.svc.cluster.local`). Traefik routes by Host header to whichever Ingress claims that host.

So:
- **`mostlydone.jericoagustin.com` needs no tunnel change.** Once the new Ingress exists, Traefik sends that host to `mostlydone/mostlydone`. Confirm the route exists (read-only GET of the tunnel config; the token is never printed): `python3 deploy/tunnel-hostname.py check mostlydone.jericoagustin.com`. It should print `has mostlydone.jericoagustin.com: True`. Confirm Traefik still listens there: `kubectl -n kube-system get svc traefik -o jsonpath='{.spec.ports[*].nodePort}{"\n"}'` should include `30950`.
- **The old hostname** keeps its tunnel rule and DNS record, but with no Ingress behind it, Traefik answers 404. The script can only `check` and `add`, so to remove it, delete that public hostname in the Cloudflare Zero Trust dashboard (Networks, then Tunnels, then this tunnel, then Public hostnames) and remove its DNS record. This is optional and can wait until after cleanup.

---

## 7. Why it is done this way

**Rename the file vs. pointing `MOSTLYDONE_DB` at the old file name.** The file is renamed while the app is stopped (2.6), and the manifest keeps `MOSTLYDONE_DB=/data/mostlydone.db`. Reasons:
- The override would have to live in the Deployment forever (or until a second migration). The day someone runs `kubectl apply -f deploy/mostlydone.yaml`, it would quietly switch back to `/data/mostlydone.db`. That is a missing file, so the app would create an **empty database and, using the Secret, a fresh admin**. The site would look healthy (`/healthz` 200, `setup_required:false`) with all the projects gone. Renaming once removes that trap for good.
- The rename is done safely. `mv_db` runs only while no app pod exists. It checkpoints the WAL into the main file (`PRAGMA wal_checkpoint(TRUNCATE)`), runs `quick_check`, refuses to continue if the `-wal` still has data, and refuses to overwrite an existing target. This matters because SQLite finds the WAL by name (`<db>-wal`). Renaming only the `.db` while a non-empty `<old>.db-wal` sits next to it would silently drop the most recent transactions. After a clean shutdown the WAL is already folded in and removed, and the checkpoint covers an unclean one. This was tested locally with the app binary: after a `kill -9` that left every row in a 560 KB `-wal` next to a 4 KB main file, `mv_db` kept all rows and the app started on the renamed file with no `created admin`.
- The helper runs as uid 65532, so the renamed file keeps the app's ownership. A rename never changes ownership or permissions, and no data is copied.

**Why the backup helper mounts the PVC read-write but reads through a read-only connection.** A live WAL database needs its readers to coordinate through the `-shm` file, which requires write access to it. A read-only *mount* can fail or misread while the app is writing. `sqlite3 -readonly` + `.backup` is SQLite's supported way to copy a live database. It never modifies the source, and the copy goes to the helper's own `emptyDir`, not the volume. Both steps were tested locally against the running app binary: the main file held 4 KB and the data sat in a 560 KB WAL, and the backup still contained every row.

**Why pre-bind on both sides (PV `claimRef` + PVC `volumeName`).** This is the documented way to reserve a specific PV for a specific claim. It makes binding immediate, even though `local-path` uses `WaitForFirstConsumer`. It also guarantees local-path cannot provision a new, empty volume for `mostlydone-data`. The PV keeps its `nodeAffinity`, so the pod lands on the node that has the data.

**Why the Secret is copied rather than retyped.** The values never appear on screen, in shell history or in the repo, and they are only used on an empty database anyway.

**Why the old Ingress is deleted inside the window.** Two Ingresses claiming the same host in different namespaces give Traefik an ambiguous route. Deleting the old one at T0 leaves exactly one owner of `mostlydone.jericoagustin.com`, and it is also what drops the old hostname.

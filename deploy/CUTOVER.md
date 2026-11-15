# Cutover to the renamed Kubernetes objects (MD-002)

MD-002 renamed the namespace, Deployment, Service, Ingress, PVC (`mostlydone-data`), admin Secret (`mostlydone-admin`), DB file (`/data/mostlydone.db`), importer hostPath (`~/mostlydone/import`) and image tag. The live homelab install may still run under the **previous names**. Run `git show 99728c0:deploy/mostlydone.yaml` to see them, and set `OLD=<that name>` below.

Applying `mostlydone.yaml` straight over a running install would create a **new namespace with an empty volume**. The pod would then fail to start until the new Secret exists. Nothing is deleted, but the app would come up without its data. Follow these steps instead. One SQLite file means one writer, so the old app must be stopped first.

```sh
OLD=...   # previous name, from git history (see above)

# 1. Stop the old app and take a backup copy of the DB file.
kubectl -n "$OLD" scale deploy/"$OLD" --replicas=0
#    (copy /data/$OLD.db, and any -wal/-shm, off the old PVC with a helper pod)

# 2. Create the new namespace and Secret. Same keys: email, password. Never commit them.
kubectl create namespace mostlydone
kubectl -n mostlydone create secret generic mostlydone-admin --from-literal=email=... --from-literal=password=...

# 3. Apply the manifest with replicas=0 (or scale to 0 right away). Then copy the DB
#    into the new PVC as /data/mostlydone.db with a helper pod that mounts both volumes,
#    or, on local-path storage, copy the directory on the node.

# 4. Start the new app and check it.
kubectl -n mostlydone scale deploy/mostlydone --replicas=1
curl -fsS https://mostlydone.jericoagustin.com/healthz

# 5. Once verified, delete the old namespace. Its PVC goes with it, so keep the backup from step 1.
```

The previous ingress hostname was dropped from the manifest. If it still needs to resolve, add a host rule for it back by hand before deleting the old namespace. The Cloudflare tunnel route (`tunnel-hostname.py`) is separate from this.

Move the importer files on the node from the old `~/<old>/import` directory to `~/mostlydone/import` before running `plane-import-pod.yaml` again.

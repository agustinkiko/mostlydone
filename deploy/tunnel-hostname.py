#!/usr/bin/env python3
"""Add one public hostname to the remotely managed Cloudflare tunnel.

Run on the homelab server. Uses the login that `cloudflared tunnel login`
saved in ~/.cloudflared/cert.pem; the token is never printed.

  python3 tunnel-hostname.py check mostlydone.jericoagustin.com
  python3 tunnel-hostname.py add   mostlydone.jericoagustin.com

`add` saves the current remote config to
~/.cloudflared/remote-before-<hostname>.json first, then inserts the new
rule just before the final catch-all rule. Nothing else is changed.
"""
import base64, json, os, sys, urllib.request

TUNNEL = "c6734a63-ca63-4a3f-9356-ded2d951f2ff"
SERVICE = "http://127.0.0.1:30950"  # Traefik's NodePort on this node
HOME = os.path.expanduser("~/.cloudflared")


def login():
    lines = open(os.path.join(HOME, "cert.pem")).read().splitlines()
    body = "".join(l for l in lines if l and not l.startswith("-----"))
    d = json.loads(base64.b64decode(body))
    return d["accountID"], d["apiToken"]


def call(method, account, token, payload=None):
    url = f"https://api.cloudflare.com/client/v4/accounts/{account}/cfd_tunnel/{TUNNEL}/configurations"
    data = json.dumps(payload).encode() if payload is not None else None
    req = urllib.request.Request(url, data=data, method=method, headers={
        "Authorization": "Bearer " + token, "Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            out = json.load(r)
    except urllib.error.HTTPError as e:
        sys.exit(f"{method} failed: HTTP {e.code} {e.read().decode()[:300]}")
    if not out.get("success"):
        sys.exit(f"{method} failed: {json.dumps(out.get('errors'))[:300]}")
    return out["result"]


def main():
    if len(sys.argv) != 3 or sys.argv[1] not in ("check", "add"):
        sys.exit(__doc__)
    mode, host = sys.argv[1], sys.argv[2]
    account, token = login()
    cur = call("GET", account, token)
    ingress = cur["config"]["ingress"]
    names = [e.get("hostname") for e in ingress]
    print(f"remote config version {cur['version']}, {len(ingress)} rules, "
          f"last rule: {ingress[-1].get('service')}, has {host}: {host in names}")
    if mode == "check" or host in names:
        return
    if "hostname" in ingress[-1]:
        sys.exit("last rule is not a catch-all; refusing to edit")
    backup = os.path.join(HOME, f"remote-before-{host}.json")
    with open(backup, "w") as f:
        json.dump(cur, f, indent=1)
    print("backup saved:", backup)
    new = dict(cur["config"])
    new["ingress"] = ingress[:-1] + [
        {"service": SERVICE, "hostname": host, "originRequest": {"httpHostHeader": host}},
        ingress[-1]]
    after = call("PUT", account, token, {"config": new})
    got = after["config"]["ingress"]
    ok = (len(got) == len(ingress) + 1 and got[:-2] == ingress[:-1] and got[-1] == ingress[-1]
          and got[-2].get("hostname") == host)
    print(f"new version {after['version']}, {len(got)} rules, old rules unchanged and new rule in place: {ok}")
    if not ok:
        sys.exit("unexpected result; restore from the backup file")


main()

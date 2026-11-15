# Deploy notes

See `CUTOVER.md` for the Plane cutover. Agents never apply manifests; Kiko
applies prod by hand.

## Trusted proxies (MD-015)

Prod traffic goes Cloudflare tunnel -> Traefik -> pod, so the pod's
`RemoteAddr` is Traefik's pod IP. The app now reads
`MOSTLYDONE_CLIENT_IP_HEADER` (`CF-Connecting-IP`) only when the peer is in
`MOSTLYDONE_TRUSTED_PROXIES`.

Before deploying an image with MD-015, add `MOSTLYDONE_TRUSTED_PROXIES` to
`mostlydone.yaml`, covering the cluster pod CIDR. The k3s default is
`10.42.0.0/16`; confirm with:

    kubectl get nodes -o jsonpath='{.items[*].spec.podCIDR}'

Without it the header is ignored (the app logs a warning at start) and every
user shares Traefik's login throttle bucket.

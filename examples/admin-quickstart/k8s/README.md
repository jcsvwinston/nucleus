# Kubernetes manifests

Apply order:

```bash
kubectl apply -f 01-namespace.yaml
kubectl apply -f 10-admin-server.yaml      # Deployment + Service + NetworkPolicy + Secret stub
kubectl apply -f 20-oauth2-proxy.yaml      # oauth2-proxy + Ingress
kubectl apply -f 30-app.yaml               # 3-replica sample app with the agent
```

Before applying:

* Replace the `admin-server-token` secret's `token` value with a real
  random string (or empty if you only use mTLS — generate the certs
  with `scripts/gen-dev-certs.sh` and mount them via a separate Secret
  + volume).
* Replace the OIDC `client-id` / `client-secret` / `cookie-secret`
  placeholders in `20-oauth2-proxy.yaml`.
* Edit the `--ui-trusted-cidrs` flag in `10-admin-server.yaml` to your
  cluster's actual pod CIDR. The default `10.0.0.0/8` is just an
  illustrative range.
* Edit the Ingress host (`admin.example.com`) and TLS secret reference.

The `NetworkPolicy` enforces the security boundary: only pods labelled
`nucleus-agent: "true"` can reach the agent listener (:9090), and only
the oauth2-proxy can reach the UI listener (:8080). Adjust the
`podSelector`s to match how you actually label your workloads.

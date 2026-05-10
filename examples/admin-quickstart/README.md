# admin-quickstart — end-to-end Nucleus admin observability

This directory contains everything you need to spin up a representative
production-like topology of the Nucleus admin observability subsystem
on your laptop:

* **3 Nucleus app pods**, each running a small example service with
  the admin agent embedded.
* **1 admin server** with the embedded UI.
* **1 oauth2-proxy** in front of the UI listener with a fixed dev
  identity (no real OIDC provider needed).
* **1 reverse round-robin load balancer** in front of the apps.

Two ways to run it:

* `docker/` — `docker compose up` from this directory. Self-contained.
* `k8s/` — drop-in manifests for a real Kubernetes cluster. Documented
  but not wired automatically.

A minimal **systemd unit set** for on-prem deployments lives in
`systemd/`.

## Tour

```
examples/admin-quickstart/
├── README.md                  (this file)
├── cmd/sample-app/main.go     A trivial Nucleus app with the agent wired
├── docker/
│   ├── docker-compose.yml     The full topology
│   ├── Dockerfile.sample-app  Build the sample app
│   ├── Dockerfile.admin-server Build admin-server
│   └── nucleus.yml            Sample app config
├── oauth2-proxy/
│   └── oauth2-proxy.cfg       oauth2-proxy in static-credentials mode
├── k8s/
│   ├── 01-namespace.yaml
│   ├── 10-admin-server.yaml   Deployment + Service + NetworkPolicy
│   ├── 20-oauth2-proxy.yaml   Deployment + Service + Ingress
│   ├── 30-app.yaml            Sample app Deployment with the agent env
│   └── README.md
└── systemd/
    ├── nucleus-admin-server.service
    └── README.md
```

## Walk-through (docker)

```bash
# 0. Build everything once. Fast (~5 s for the binaries; the docker
#    images are minimal and cache well).
make build
docker compose -f examples/admin-quickstart/docker/docker-compose.yml \
    --project-directory examples/admin-quickstart \
    up --build

# 1. Open the admin UI:
open http://localhost:4180        # oauth2-proxy front door

#    The static-credentials oauth2-proxy is configured with:
#      user: admin
#      pass: admin
#    Replace with your real IdP for anything beyond local development.

# 2. The three app pods (sample-app-{1,2,3}) periodically emit HTTP
#    requests against themselves; the agent ships them to the admin
#    server. You should see them flowing in #/http within seconds.
```

## How the agent finds the admin server

Inside `docker-compose.yml`, the agent container is given:

```yaml
environment:
  NUCLEUS_ADMIN_ENDPOINTS: "http://admin-server:9090"
  NUCLEUS_ADMIN_TOKEN: "${ADMIN_TOKEN:-dev-shared-token}"
```

The agent's failover list is just one URL (the docker service name).
In a real deployment this would be a comma-separated list pointing at
each admin replica, e.g.
`https://admin-a.internal:9090,https://admin-b.internal:9090`.

## What this example does NOT do

* It does not generate real TLS certificates. Use `scripts/gen-dev-certs.sh`
  to produce them, then mount them into the containers and switch the
  agent endpoint scheme to `https://`.
* It does not show real OIDC. oauth2-proxy is configured in
  static-credentials mode for simplicity. The Kubernetes manifest
  refers to a real OIDC `Secret` you fill in.
* It does not show observability of the admin server itself. In a real
  deployment, scrape its `/metrics` endpoint just like any other
  service.

## Next steps

* `admin/README.md` — top-level architecture and configuration reference.
* `admin/BENCHMARKS.md` — overhead numbers and reproducibility recipe.
* `admin/proto/EVOLUTION.md` — how to evolve the wire contract safely.

# MVC + API Example

This example demonstrates how to run a GoFrame project that combines:

- MVC pages
- REST API endpoints
- embedded admin panel

## Run

From repository root:

```bash
go run ./examples/mvc_api
```

Open:

- `http://localhost:8090/`
- `http://localhost:8090/articles`
- `http://localhost:8090/contact`
- `http://localhost:8090/api/health`
- `http://localhost:8090/admin`

## Runtime overrides (env)

`examples/mvc_api` accepts optional environment overrides for local cluster testing:

- `GOFRAME_EXAMPLE_PORT`
- `GOFRAME_EXAMPLE_DB_URL`
- `GOFRAME_EXAMPLE_REDIS_URL`
- `GOFRAME_EXAMPLE_SESSION_STORE` (`memory|sql|redis`)
- `GOFRAME_EXAMPLE_SESSION_REDIS_URL`
- `GOFRAME_EXAMPLE_ADMIN_CLUSTER_ENABLED`
- `GOFRAME_EXAMPLE_ADMIN_CLUSTER_REDIS_URL`
- `GOFRAME_EXAMPLE_ADMIN_CLUSTER_CHANNEL`
- `GOFRAME_EXAMPLE_ADMIN_CLUSTER_NODE_ID`
- `GOFRAME_EXAMPLE_ADMIN_CLUSTER_TOKEN`
- `GOFRAME_EXAMPLE_ADMIN_TRACE_URL_TEMPLATE`
- `GOFRAME_EXAMPLE_OTLP_ENDPOINT`
- `GOFRAME_EXAMPLE_ADMIN_TITLE`

For a full 2-node + LB lab, use:

- `scripts/dev/run_admin_cluster_lab.sh`
- `scripts/dev/run_admin_cluster_lab.ps1`

## Purpose

Use this example as a reference for:

- app bootstrap with `pkg/app`
- model registration and admin exposure
- route composition and practical wiring
- MVC pages that read/write the same business data as the API and `/admin`

## Demo credentials

- App MVC login: `demo / demo123456`
- Admin login: `admin / supersecret123` (or `GOFRAME_EXAMPLE_ADMIN_BOOTSTRAP_PASSWORD`)

## Suggested walkthrough

1. Visit `/articles` to see the public MVC catalog of published content.
2. Submit `/contact` to create a lead from a classic HTML form.
3. Open `/api/leads` to verify the same lead through JSON.
4. Sign in via `/app/login` and check the dashboard summaries.
5. Open `/admin` to edit the same articles and leads from the back office.

## Live Feature Flag Demo

This example registers a runtime flag:

- `articles_preview_mode` (default `false`)

And exposes a demo endpoint:

- `GET /api/articles/live-flag`

Behavior:

- `false` -> mode `published_only` (drafts are hidden)
- `true` -> mode `preview_all` (drafts are included)

How to test manually:

1. Create one draft article (`published=false`) from `/admin` or `POST /api/articles`.
2. Call `GET /api/articles/live-flag` and verify draft is hidden.
3. Go to `/admin/system` -> **Live feature flags** -> enable `articles_preview_mode`.
4. Call `GET /api/articles/live-flag` again and verify draft is now visible.

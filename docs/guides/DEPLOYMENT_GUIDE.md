# Deployment Guide

Reference date: 2026-05-24.
Status: Current.

This guide covers deploying Nucleus applications to production, including containerization, reverse proxy configuration, TLS, horizontal scaling, and operational best practices.

## Table of Contents

- [Overview](#overview)
- [Build and Release](#build-and-release)
- [Docker Deployment](#docker-deployment)
- [Kubernetes Deployment](#kubernetes-deployment)
- [Reverse Proxy Configuration](#reverse-proxy-configuration)
- [TLS/HTTPS Setup](#tlshttps-setup)
- [Horizontal Scaling](#horizontal-scaling)
- [Database Backup and Recovery](#database-backup-and-recovery)
- [Log Aggregation](#log-aggregation)
- [Production Hardening Checklist](#production-hardening-checklist)

---

## Overview

A Nucleus application consists of two runtime processes:

1. **Server** (`cmd/server`): HTTP server handling web requests.
2. **Worker** (`cmd/worker`): Background job processor for Asynq tasks.

Both processes share the same binary but run different entry points.

---

## Build and Release

### Build from source

```bash
# Build server
go build -o bin/server ./cmd/server

# Build worker
go build -o bin/worker ./cmd/worker

# Build CLI (optional, for operations)
go build -o bin/nucleus ./cmd/nucleus
```

### Version-injected build

```bash
VERSION="1.0.0"
COMMIT=$(git rev-parse --short HEAD)
DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)

go build -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
  -o bin/server ./cmd/server
```

---

## Docker Deployment

### Single-stage Dockerfile

```dockerfile
FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server
RUN CGO_ENABLED=0 go build -o /bin/worker ./cmd/worker

FROM alpine:3.20

RUN apk --no-cache add ca-certificates tzdata

RUN adduser -D -u 1000 appuser
USER appuser

COPY --from=builder /bin/server /bin/server
COPY --from=builder /bin/worker /bin/worker
COPY nucleus.yml /etc/app/nucleus.yml
COPY internal/config/ /etc/app/config/

WORKDIR /etc/app

ENTRYPOINT ["/bin/server"]
```

### Multi-stage Dockerfile with migrations

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server
RUN CGO_ENABLED=0 go build -o /bin/worker ./cmd/worker
RUN CGO_ENABLED=0 go build -o /bin/nucleus ./cmd/nucleus

FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata
RUN adduser -D -u 1000 appuser
USER appuser

COPY --from=builder /bin/server /bin/server
COPY --from=builder /bin/worker /bin/worker
COPY --from=builder /bin/nucleus /bin/nucleus
COPY nucleus.yml /etc/app/nucleus.yml
COPY migrations/ /etc/app/migrations/

WORKDIR /etc/app

# Default: run server
CMD ["/bin/server"]
```

### Docker Compose (development)

```yaml
version: "3.9"
services:
  server:
    build: .
    ports:
      - "8080:8080"
    environment:
      - NUCLEUS_ENV=development
      # session_cookie_secure defaults to true. Opt out here so session
      # cookies work over plain HTTP in local development.
      - NUCLEUS_SESSION_COOKIE_SECURE=false
      - NUCLEUS_DATABASE_DEFAULT=default
      - NUCLEUS_DATABASES__DEFAULT__URL=sqlite://nucleus.db
      - NUCLEUS_REDIS_URL=redis://redis:6379/0
    depends_on:
      - redis
    volumes:
      - ./nucleus.yml:/etc/app/nucleus.yml

  worker:
    build: .
    command: ["/bin/worker"]
    environment:
      - NUCLEUS_ENV=development
      - NUCLEUS_REDIS_URL=redis://redis:6379/0
    depends_on:
      - redis

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"

  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: nucleus
      POSTGRES_PASSWORD: nucleus
      POSTGRES_DB: nucleus
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data

volumes:
  pgdata:
```

### Docker Compose (production)

```yaml
version: "3.9"
services:
  server:
    build:
      context: .
      target: builder
    ports:
      - "8080:8080"
    environment:
      - NUCLEUS_ENV=production
      - NUCLEUS_JWT_SECRET=${NUCLEUS_JWT_SECRET}
      - NUCLEUS_DATABASE_DEFAULT=default
      - NUCLEUS_DATABASES__DEFAULT__URL=${DATABASE_URL}
      - NUCLEUS_REDIS_URL=${REDIS_URL}
      - NUCLEUS_SESSION_STORE=redis
      # session_cookie_secure is true by default — no override needed in production.
    deploy:
      replicas: 2
      resources:
        limits:
          memory: 512M
          cpus: "1.0"
    healthcheck:
      test: ["CMD", "/bin/nucleus", "health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 10s
    depends_on:
      - postgres
      - redis

  worker:
    build:
      context: .
      target: builder
    command: ["/bin/worker"]
    environment:
      - NUCLEUS_ENV=production
      - NUCLEUS_REDIS_URL=${REDIS_URL}
    deploy:
      replicas: 1
      resources:
        limits:
          memory: 256M
          cpus: "0.5"
    depends_on:
      - redis

  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: ${POSTGRES_USER}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB: ${POSTGRES_DB}
    volumes:
      - pgdata:/var/lib/postgresql/data
    deploy:
      resources:
        limits:
          memory: 1G

  redis:
    image: redis:7-alpine
    command: redis-server --requirepass ${REDIS_PASSWORD}
    volumes:
      - redisdata:/data

volumes:
  pgdata:
  redisdata:
```

---

## Kubernetes Deployment

### Deployment manifest

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nucleus-server
  labels:
    app: nucleus
    component: server
spec:
  replicas: 3
  selector:
    matchLabels:
      app: nucleus
      component: server
  template:
    metadata:
      labels:
        app: nucleus
        component: server
    spec:
      containers:
      - name: server
        image: your-registry/nucleus:latest
        ports:
        - containerPort: 8080
          name: http
        env:
        - name: NUCLEUS_ENV
          value: production
        - name: NUCLEUS_JWT_SECRET
          valueFrom:
            secretKeyRef:
              name: nucleus-secrets
              key: jwt-secret
        - name: NUCLEUS_DATABASES__DEFAULT__URL
          valueFrom:
            secretKeyRef:
              name: nucleus-secrets
              key: database-url
        - name: NUCLEUS_REDIS_URL
          valueFrom:
            secretKeyRef:
              name: nucleus-secrets
              key: redis-url
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "1000m"
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 30
        readinessProbe:
          httpGet:
            path: /healthz
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 10
---
apiVersion: v1
kind: Service
metadata:
  name: nucleus-server
spec:
  selector:
    app: nucleus
    component: server
  ports:
  - protocol: TCP
    port: 80
    targetPort: 8080
  type: ClusterIP
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nucleus-worker
  labels:
    app: nucleus
    component: worker
spec:
  replicas: 1
  selector:
    matchLabels:
      app: nucleus
      component: worker
  template:
    metadata:
      labels:
        app: nucleus
        component: worker
    spec:
      containers:
      - name: worker
        image: your-registry/nucleus:latest
        command: ["/bin/worker"]
        env:
        - name: NUCLEUS_ENV
          value: production
        - name: NUCLEUS_REDIS_URL
          valueFrom:
            secretKeyRef:
              name: nucleus-secrets
              key: redis-url
        resources:
          requests:
            memory: "128Mi"
            cpu: "100m"
          limits:
            memory: "256Mi"
            cpu: "500m"
```

### ConfigMap for non-sensitive configuration

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: nucleus-config
data:
  NUCLEUS_HOST: "0.0.0.0"
  NUCLEUS_PORT: "8080"
  NUCLEUS_SESSION_STORE: "redis"
  # NUCLEUS_SESSION_COOKIE_SECURE is true by default — only set it to "false" in plain-HTTP dev environments.
  NUCLEUS_SESSION_COOKIE_SAMESITE: "strict"
  NUCLEUS_LOG_LEVEL: "info"
  NUCLEUS_OTLP_ENDPOINT: "http://otel-collector:4318"
```

---

## Reverse Proxy Configuration

### Caddy (recommended for simplicity)

```caddyfile
app.example.com {
    reverse_proxy localhost:8080

    encode gzip
    log {
        output file /var/log/caddy/access.log
    }
}
```

### Nginx

```nginx
upstream nucleus {
    server 127.0.0.1:8080;
    keepalive 32;
}

server {
    listen 80;
    server_name app.example.com;

    # Redirect to HTTPS
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name app.example.com;

    ssl_certificate /etc/ssl/certs/app.pem;
    ssl_certificate_key /etc/ssl/private/app-key.pem;
    ssl_protocols TLSv1.2 TLSv1.3;

    # Security headers
    add_header X-Frame-Options DENY;
    add_header X-Content-Type-Options nosniff;
    add_header X-XSS-Protection "1; mode=block";
    add_header Referrer-Policy strict-origin-when-cross-origin;

    # Proxy settings
    location / {
        proxy_pass http://nucleus;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
    }

    # WebSocket support for admin live inspector
    location /admin/api/live/ws {
        proxy_pass http://nucleus;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # Static assets
    location /static/ {
        expires 1y;
        add_header Cache-Control "public, immutable";
    }

    access_log /var/log/nginx/nucleus-access.log;
    error_log /var/log/nginx/nucleus-error.log;
}
```

### Traefik

```yaml
http:
  routers:
    nucleus:
      rule: "Host(`app.example.com`)"
      service: nucleus
      entryPoints:
        - websecure
      tls:
        certResolver: letsencrypt

  services:
    nucleus:
      loadBalancer:
        servers:
          - url: "http://127.0.0.1:8080"
```

---

## TLS/HTTPS Setup

### Let's Encrypt with Caddy (automatic)

Caddy handles TLS automatically. Just configure the domain and Caddy obtains/renews certificates.

### Let's Encrypt with Certbot + Nginx

```bash
# Install certbot
sudo apt-get install certbot python3-certbot-nginx

# Obtain certificate
sudo certbot --nginx -d app.example.com

# Auto-renewal (certbot sets up cron automatically)
sudo certbot renew --dry-run
```

### Behind load balancer (AWS ALB, Cloudflare)

If TLS is terminated upstream:

```yaml
# nucleus.yml
server:
  host: "0.0.0.0"
  port: 8080

# Nucleus speaks plain HTTP; Nginx terminates TLS.
# Keep the secure default: the browser<->Nginx leg is HTTPS, so the Secure
# session cookie rides correctly even though the Nginx<->Nucleus back-channel
# is plain HTTP. Do NOT set NUCLEUS_SESSION_COOKIE_SECURE=false here — that is
# only for deployments with no TLS anywhere in front of the client.
```

---

## Horizontal Scaling

### Server instances

Nucleus servers are stateless. Scale horizontally freely:

```bash
# Docker Compose
docker compose up --scale server=5

# Kubernetes
kubectl scale deployment nucleus-server --replicas=5
```

### Shared state requirements

When running multiple server replicas:

| Feature | Requirement |
|---------|-------------|
| **Sessions** | Use `sql` or `redis` store (not `memory`) |
| **Background jobs** | Redis required for Asynq |
| **Rate limiting** | Redis required for distributed counters |
| **Caching** | Redis or SQL-backed cache table |

### Worker scaling

```bash
# Run multiple workers consuming from same Redis queue
docker compose up --scale worker=3
```

Asynq workers share queue consumption. Each task is processed by exactly one worker.

---

## Database Backup and Recovery

### PostgreSQL

```bash
# Backup
pg_dump -U nucleus -h localhost nucleus > backup_$(date +%Y%m%d_%H%M%S).sql

# Compressed backup
pg_dump -U nucleus -h localhost nucleus | gzip > backup_$(date +%Y%m%d_%H%M%S).sql.gz

# Restore
psql -U nucleus -h localhost nucleus < backup_20260410_120000.sql

# Continuous backup (WAL archiving) - configure in postgresql.conf
wal_level = replica
archive_mode = on
archive_command = 'cp %p /backup/wal/%f'
```

### MySQL

```bash
# Backup
mysqldump -u nucleus -p nucleus > backup_$(date +%Y%m%d_%H%M%S).sql

# Restore
mysql -u nucleus -p nucleus < backup_20260410_120000.sql
```

### SQLite

```bash
# Backup (while running)
# Use nucleus dumpdata for application data
nucleus dumpdata --config nucleus.yml > fixture.json

# File-level backup (stop server first)
cp nucleus.db nucleus.db.backup

# Or use .backup command in sqlite3
sqlite3 nucleus.db ".backup 'nucleus.db.backup'"
```

### Nucleus fixtures

```bash
# Export all data as JSON fixtures
nucleus dumpdata --config nucleus.yml > fixtures.json

# Import fixtures
nucleus loaddata --config nucleus.yml fixtures.json

# Import with truncate (production requires --force)
nucleus loaddata --config nucleus.yml --truncate --force fixtures.json
```

### Backup schedule

```bash
# Cron: daily backup at 2am
0 2 * * * pg_dump -U nucleus nucleus | gzip > /backup/db_$(date +\%Y\%m\%d).sql.gz

# Cron: weekly full backup, daily incrementals
0 2 * * 0 pg_dump -U nucleus -Fc nucleus > /backup/full_$(date +\%Y\%m\%d).dump
0 2 * * 1-6 pg_dump -U nucleus --data-only nucleus | gzip > /backup/daily_$(date +\%Y\%m\%d).sql.gz
```

---

## Log Aggregation

Nucleus uses structured logging via `log/slog` and exports traces/metrics via OpenTelemetry.

### Log output formats

```yaml
# nucleus.yml
log_level: info
log_format: json   # Options: text, json
```

### File logging (sidecar)

```bash
# Run server, redirect logs to file
./bin/server 2>&1 | tee -a /var/log/nucleus/server.log

# Or use systemd journal
# /etc/systemd/system/nucleus-server.service
[Service]
StandardOutput=journal
StandardError=journal
```

### Loki (Grafana stack)

```yaml
# Promtail configuration
scrape_configs:
  - job_name: nucleus
    static_configs:
      - targets:
          - localhost
        labels:
          job: nucleus
          __path__: /var/log/nucleus/*.log
    pipeline_stages:
      - json:
          expressions:
            level: level
            msg: msg
      - labels:
          level:
```

### ELK Stack (Elasticsearch, Logstash, Kibana)

```ruby
# Logstash configuration
input {
  file {
    path => "/var/log/nucleus/*.log"
    codec => "json"
  }
}

filter {
  mutate {
    add_field => { "service" => "nucleus" }
  }
}

output {
  elasticsearch {
    hosts => ["localhost:9200"]
    index => "nucleus-%{+YYYY.MM.dd}"
  }
}
```

### OpenTelemetry Collector

```yaml
# otel-collector-config.yaml
receivers:
  otlp:
    protocols:
      http:
        endpoint: "0.0.0.0:4318"

exporters:
  logging:
    loglevel: debug
  otlp/jaeger:
    endpoint: "jaeger:4317"
    tls:
      insecure: true
  prometheus:
    endpoint: "0.0.0.0:8889"

service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [otlp/jaeger, logging]
    metrics:
      receivers: [otlp]
      exporters: [prometheus, logging]
```

```yaml
# nucleus.yml
otlp_endpoint: http://otel-collector:4318
```

---

## Production Hardening Checklist

### Security

- [ ] `NUCLEUS_ENV=production` set (also forces `UnknownFieldsStrict` — any typo'd config key that slipped through development with `WithUnknownFields("warn")` will now abort startup)
- [ ] Strong `jwt_secret` (64-byte random hex) — or use the rotation API (`auth.NewJWTManagerFromKeys` + RS256 keypair) and publish `/.well-known/jwks.json`
- [ ] JWT rotation plan documented (RotateKey → grace window → RemoveKey)
- [ ] `session_cookie_secure` is `true` (the default — no action needed unless plain-HTTP, in which case explicitly set `false` and document why)
- [ ] `session_cookie_samesite: strict`
- [ ] CSRF middleware enabled for state-changing endpoints
- [ ] Rate limiting configured (`rate_limit_burst`, `rate_limit_by_route`)
- [ ] CORS origins restricted (no `*` in production)
- [ ] Admin panel secured (`nucleus createuser` run)
- [ ] Database credentials in secrets manager (not env files)
- [ ] TLS enabled (Let's Encrypt or load balancer)
- [ ] Casbin deny-override rules in place for any user/path that must remain blocked even under role expansion

### Reliability

- [ ] Liveness/readiness probes wired to `/healthz` (the unauthenticated core endpoint that probes DB / Redis / storage / mail). `/api/health` is the *admin* healthcheck and requires auth — do not use it for k8s probes.
- [ ] Prometheus scrape configured against `/metrics` (or `metrics_path: ""` to disable)
- [ ] `nucleus migrate drift` wired into the CI/CD predeploy gate so an applied migration with a missing `.up.sql` blocks the rollout
- [ ] Graceful shutdown tested (drain connections)
- [ ] Database connection pooling tuned (`max_open_conns`, `max_idle_conns`)
- [ ] Redis connection validated (`nucleus health`)
- [ ] Session store set to `redis` or `sql` (not `memory`)
- [ ] Background workers running and consuming queues
- [ ] OTel exporter configured and shipping data
- [ ] Circuit breakers (`pkg/circuit`) wrapping calls to external mail / storage / plugin / third-party APIs that can degrade independently

### Operations

- [ ] Log aggregation configured (file, Loki, or ELK)
- [ ] Metrics dashboard created (Grafana/Prometheus)
- [ ] Alert rules configured (error rate, latency, queue depth)
- [ ] Database backups scheduled and tested
- [ ] Migration strategy defined (`nucleus migrate` before deploy)
- [ ] Rollback plan documented
- [ ] `nucleus check --deploy` passes

### Performance

- [ ] Static assets collected (`nucleus collectstatic`)
- [ ] CDN configured for `/static/`
- [ ] Database indexes verified (`nucleus inspectdb`)
- [ ] Query performance monitored (admin live SQL inspector)
- [ ] Worker concurrency tuned for workload
- [ ] Redis memory usage monitored

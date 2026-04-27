# Postgres in Kubernetes — Design Spec

**Date:** 2026-04-27
**Scope:** Dev/local cluster (kind, minikube)

## Goal

Add a Postgres instance to the existing Kubernetes manifests in `config/deployment.yaml` and wire the `custom-api-server` deployment to use it via the already-implemented `STORAGE_BACKEND=postgres` / `POSTGRES_DSN` env vars.

## Approach

Plain `Deployment` + `ClusterIP Service` with `emptyDir` storage (Option A). No PersistentVolume — data is ephemeral, which is acceptable for dev.

## New Resources

### Secret `postgres-credentials`

Holds three keys used by both the postgres pod and the api-server:

| Key | Value |
|-----|-------|
| `POSTGRES_USER` | `apiserver` |
| `POSTGRES_PASSWORD` | `apiserver` |
| `POSTGRES_DB` | `apiserver` |

### Deployment `postgres`

- Image: `postgres:16`
- Single replica
- Env sourced from `postgres-credentials` Secret via `envFrom`
- Data directory (`/var/lib/postgresql/data`) mounted from an `emptyDir` volume
- No liveness/readiness probes (dev simplicity)

### Service `postgres`

- Type: `ClusterIP`
- Port: `5432 → 5432`
- Selector: `app: postgres`

## Changes to Existing Deployment

Add to the `custom-api-server` container's `env`:

```yaml
- name: STORAGE_BACKEND
  value: postgres
- name: POSTGRES_USER
  valueFrom:
    secretKeyRef:
      name: postgres-credentials
      key: POSTGRES_USER
- name: POSTGRES_PASSWORD
  valueFrom:
    secretKeyRef:
      name: postgres-credentials
      key: POSTGRES_PASSWORD
- name: POSTGRES_DB
  valueFrom:
    secretKeyRef:
      name: postgres-credentials
      key: POSTGRES_DB
- name: POSTGRES_DSN
  value: "postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@postgres.default.svc.cluster.local:5432/$(POSTGRES_DB)?sslmode=disable"
```

Kubernetes resolves `$(VAR)` references within the same `env` list before injecting them, so `POSTGRES_DSN` is constructed correctly at pod start.

## Files Changed

- `config/deployment.yaml` — add Secret, Deployment, Service for postgres; add env vars to existing api-server Deployment

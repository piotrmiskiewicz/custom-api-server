# Aggregated API Server — Design Spec
Date: 2026-04-23

## Overview

A Kubernetes Aggregated API Server for the `solution.piotrmiskiewicz.github.com` API group, built with `k8s.io/apiserver`. Serves the `Solution` custom type (namespaced) with an in-memory storage backend. No authentication, no etcd — intended as a local dev/experiment server.

---

## Package Layout

```
cmd/server/
  main.go                         # entrypoint: builds and runs the server

pkg/
  apis/
    solution/
      v1alpha1/
        types.go                  # existing: Solution, SolutionList, SolutionSpec, SolutionStatus
        register.go               # AddToScheme for versioned types + hand-written DeepCopy methods
      register.go                 # internal (unversioned) types scheme registration
      types.go                    # internal Solution type (mirrors v1alpha1, no json tags)
  apiserver/
    server.go                     # builds GenericAPIServer, registers API group
  registry/
    solution/
      storage.go                  # in-memory REST storage (StandardStorage + StatusREST)
```

---

## Scheme & Type Registration

Three sets of types registered in a single `runtime.Scheme`:

1. **Internal types** (`pkg/apis/solution/types.go`) — same struct shape as `v1alpha1`, no JSON tags, registered under group `solution.piotrmiskiewicz.github.com` with empty version `""`.
2. **Versioned types** (`pkg/apis/solution/v1alpha1/types.go`) — existing `Solution`/`SolutionList`, registered under group `solution.piotrmiskiewicz.github.com` + version `v1alpha1`. `DeepCopyObject()` methods hand-written (no code generator).
3. **Kubernetes meta types** — `metav1.Status`, `metav1.WatchEvent`, etc., via `metav1.AddToGroupVersion`.

A single `Scheme` in `pkg/apiserver/server.go` registers all three. A `CodecFactory` built from this scheme handles JSON/YAML serialization.

Conversions between internal and `v1alpha1` are field-for-field and registered with the scheme.

---

## In-Memory Storage

`pkg/registry/solution/storage.go` — a `map[string]runtime.Object` protected by `sync.RWMutex`.

**Interfaces implemented:**
- `rest.StandardStorage`: `Get`, `Create`, `Update`, `Delete`, `List`, `New`, `NewList`, `Destroy`
- `rest.Scoper`: returns `resource.NamespaceScoped = true`
- `rest.StatusUpdater`: separate `StatusREST` struct wrapping the same map, handles `PUT /status`

**Key behaviors:**
- `Create`: generates UID + `resourceVersion: "1"`, sets `creationTimestamp`, stores under `namespace/name` key
- `Update`: increments `resourceVersion` as a monotonic string counter
- `Delete`: removes from map, returns deleted object
- `List`: filters by namespace (from context), returns `SolutionList`
- `Watch`: returns `errors.NewMethodNotSupported` — out of scope
- `StatusREST.Update`: updates only `.status`, increments `resourceVersion`

Map key format: `<namespace>/<name>`

---

## Server Wiring

**`pkg/apiserver/server.go`:**
- Constructs a bare `genericapiserver.Config` directly (no `RecommendedOptions`, no etcd)
- Disables `SecureServing`; listens on plain HTTP `:8080`
- Registers API group `solution.piotrmiskiewicz.github.com/v1alpha1`:
  - `solutions` → `SolutionStorage`
  - `solutions/status` → `StatusREST`
- OpenAPI stub (`GetOpenAPIDefinitions` returns empty map) to prevent panic on `/openapi/v2`

**`cmd/server/main.go`:**
- Calls `apiserver.New()` → `server.Run(stopCh)`
- Blocks until OS signal

---

## Running

```bash
go run ./cmd/server
# In another terminal:
kubectl get solutions --server=http://localhost:8080
kubectl create -f solution.yaml --server=http://localhost:8080
```

---

## Out of Scope

- Authentication / Authorization
- Watch / informer support
- Persistent storage (etcd)
- Admission webhooks
- OpenAPI schema generation (beyond stub)
- TLS / secure serving

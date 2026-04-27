# Watch Implementation (LISTEN/NOTIFY + In-Memory Fan-out) — Design Spec

**Date:** 2026-04-27
**Scope:** Implement `Watch` for both `InMemoryStorage` and `PostgresStorage` with namespace and field selector filtering.

## Goal

Replace the `MethodNotSupported` stub in both storage backends with a working `Watch` implementation. `InMemoryStorage` uses in-process fan-out; `PostgresStorage` uses Postgres `LISTEN/NOTIFY`.

## Approach

Option B — per-backend watch, no shared hub. Each backend manages its own watcher list independently. A shared `watcher` type (new file) provides the `watch.Interface` implementation and the filter+send logic used by both backends.

---

## Section 1: Common `watcher` type

**File:** `pkg/registry/solution/watcher.go` (new)

### Struct

```go
type watcher struct {
    ch       chan watch.Event
    ns       string
    fieldSel fields.Selector
    cancel   context.CancelFunc
    stopped  uint32 // atomic
}
```

### Constructor

```go
func newWatcher(ns string, fieldSel fields.Selector) *watcher
```

Creates a watcher with a buffered channel of size 100.

### `watch.Interface` implementation

- `ResultChan() <-chan watch.Event` — returns `ch`
- `Stop()` — sets `stopped` atomically, calls `cancel()`, closes `ch`

### `send` method

```go
func (w *watcher) send(eventType watch.EventType, obj *internal.Solution) bool
```

- Returns false if `atomic.LoadUint32(&w.stopped) != 0`
- Filters by namespace: if `w.ns != ""` and `obj.Namespace != w.ns`, skip
- Filters by field selector: if `w.fieldSel != nil && !w.fieldSel.Empty()`, evaluate using `GetAttrs`
- Sends to `w.ch` non-blocking (select with default); drops event silently if buffer full
- Returns true if sent or filtered (watcher still alive), false only if stopped

---

## Section 2: `InMemoryStorage` watch

**File:** `pkg/registry/solution/storage.go` (modify)

### Changes

- Add `watchers []*watcher` field to `InMemoryStorage` (protected by existing `mu`)
- Add `broadcast(eventType watch.EventType, obj *internal.Solution)` private method:
  - Iterates `watchers`, calls `send` on each
  - Removes watchers where `atomic.LoadUint32(&w.stopped) != 0`
- Call `broadcast` after every successful `Create`, `Update`, `Delete` (while holding `mu`)
- Implement `Watch(ctx context.Context, opts *metainternalversion.ListOptions) (watch.Interface, error)`:
  - Validates field selector via `validateFieldSelector`
  - Creates a `watcher` with namespace from ctx and field selector from opts
  - Appends to `watchers` under `mu`
  - Starts a goroutine: waits for `ctx.Done()`, calls `w.Stop()`, removes from `watchers`
  - Returns the watcher

---

## Section 3: `PostgresStorage` watch (LISTEN/NOTIFY)

**File:** `pkg/registry/solution/storage_postgres.go` (modify)

### Changes

#### Watcher list

- Add `watchMu sync.Mutex` and `watchers []*watcher` fields to `PostgresStorage`
- Add `broadcast(eventType watch.EventType, obj *internal.Solution)` private method (same logic as in-memory: iterate, send, remove stopped)

#### NOTIFY on writes

Each write operation (`Create`, `Update`, `Delete`) fires a `pg_notify` after the main SQL:

```sql
SELECT pg_notify('solutions', $1)
```

Payload is a JSON object:

```json
{
  "type": "ADDED" | "MODIFIED" | "DELETED",
  "namespace": "...",
  "name": "...",
  "uid": "...",
  "resource_version": 1,
  "creation_timestamp": "...",
  "labels": {},
  "spec_solution_name": "...",
  "status_phase": "...",
  "status_conditions": []
}
```

All fields needed to reconstruct the full `internal.Solution` are included so the listener never needs to re-query the DB.

#### Listener goroutine

Started in `NewPostgresStorage`, runs for the lifetime of the storage:

```go
func (s *PostgresStorage) listenLoop(ctx context.Context)
```

- Acquires a dedicated `pgconn` connection via `pgxpool.Pool.Acquire` then `.Conn().Hijack()` (or `pgconn.Connect` directly with the same DSN)
- Executes `LISTEN solutions`
- Loops on `conn.WaitForNotification(ctx)`
- On notification: unmarshals JSON payload, constructs `internal.Solution`, calls `broadcast`
- On error (connection lost): retries with exponential backoff — 1s, 2s, 4s, 8s, 16s (5 attempts). If all fail, logs and returns (no more events, but no crash)
- Stopped when the context passed to `NewPostgresStorage` is cancelled

#### `Watch` method

Same pattern as in-memory: create watcher, append under `watchMu`, goroutine to remove on `ctx.Done()`.

#### `Destroy` changes

Cancels the listener goroutine context, stops all active watchers (calls `Stop()` on each), then closes the pool.

---

## Section 4: Error handling

| Scenario | Behaviour |
|---|---|
| Slow watcher (buffer full) | Event dropped silently. Buffer size 100. |
| LISTEN connection lost | Retry with backoff (1s×5). If exhausted, listener stops — no new events. |
| Watcher stopped before event | `send` returns false, watcher removed from list on next `broadcast`. |
| `Destroy` called with active watchers | All watchers stopped, listener goroutine cancelled, then pool closed. |

---

## Section 5: Testing

**File:** `pkg/registry/solution/storage_test.go` (modify)

New tests for `InMemoryStorage`:

- `TestWatch_Create` — watch, create a solution, assert `watch.Added` event arrives within 1s
- `TestWatch_Update` — watch, create then update, assert `watch.Modified` event
- `TestWatch_Delete` — watch, create then delete, assert `watch.Deleted` event
- `TestWatch_NamespaceFilter` — two watchers on different namespaces, assert each only receives its own events
- `TestWatch_FieldSelectorFilter` — watcher with `spec.solutionName=alpha`, create two solutions, assert only matching one arrives
- `TestWatch_Stop` — stop watcher, create solution, assert channel receives nothing

No new tests for `PostgresStorage` watch (requires live postgres — consistent with existing test pattern).

---

## Files Changed

| File | Change |
|---|---|
| `pkg/registry/solution/watcher.go` | New — `watcher` struct, `newWatcher`, `Stop`, `ResultChan`, `send` |
| `pkg/registry/solution/storage.go` | Add `watchers`, `broadcast`, implement `Watch`, call `broadcast` in writes |
| `pkg/registry/solution/storage_postgres.go` | Add `watchers`, `broadcast`, `listenLoop`, `pg_notify` in writes, implement `Watch`, update `Destroy` |
| `pkg/registry/solution/storage_test.go` | Add 6 watch tests for `InMemoryStorage` |

# Watch Implementation (LISTEN/NOTIFY + In-Memory Fan-out) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `Watch` on both `InMemoryStorage` (in-process fan-out) and `PostgresStorage` (Postgres LISTEN/NOTIFY) with namespace and field selector filtering.

**Architecture:** A shared `watcher` type in a new file implements `watch.Interface` and the filter+send logic. Each backend manages its own `[]*watcher` list independently, calls `broadcast` after every write, and implements `Watch` to register new watchers. `PostgresStorage` additionally runs a background `listenLoop` goroutine that receives `NOTIFY` payloads and calls `broadcast`.

**Tech Stack:** Go, `k8s.io/apimachinery/pkg/watch`, `github.com/jackc/pgx/v5/pgconn`, `sync/atomic`

---

### Task 1: Create `watcher.go` — shared watcher type

**Files:**
- Create: `pkg/registry/solution/watcher.go`

This task has no test (the watcher is tested via the storage tests in Tasks 2–3). Write the implementation directly.

- [ ] **Step 1: Create `pkg/registry/solution/watcher.go`**

```go
package solution

import (
	"context"
	"sync/atomic"

	internal "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
)

const watchChanSize = 100

// watcher implements watch.Interface for Solution objects.
// It filters events by namespace and field selector before forwarding them.
type watcher struct {
	ch       chan watch.Event
	ns       string
	fieldSel fields.Selector
	cancel   context.CancelFunc
	stopped  uint32 // accessed atomically
}

// newWatcher creates a watcher for the given namespace and field selector.
// ns == "" means all namespaces. fieldSel == nil means no field filtering.
func newWatcher(ns string, fieldSel fields.Selector, cancel context.CancelFunc) *watcher {
	return &watcher{
		ch:       make(chan watch.Event, watchChanSize),
		ns:       ns,
		fieldSel: fieldSel,
		cancel:   cancel,
	}
}

// Stop implements watch.Interface. Safe to call multiple times.
func (w *watcher) Stop() {
	if atomic.CompareAndSwapUint32(&w.stopped, 0, 1) {
		w.cancel()
		close(w.ch)
	}
}

// ResultChan implements watch.Interface.
func (w *watcher) ResultChan() <-chan watch.Event {
	return w.ch
}

// isStopped reports whether Stop has been called.
func (w *watcher) isStopped() bool {
	return atomic.LoadUint32(&w.stopped) != 0
}

// send evaluates namespace and field selector filters and, if the event matches,
// sends it non-blocking to the watcher's channel (drops silently if full).
// Returns false if the watcher has been stopped.
func (w *watcher) send(eventType watch.EventType, obj *internal.Solution) bool {
	if w.isStopped() {
		return false
	}
	// Namespace filter.
	if w.ns != "" && obj.Namespace != w.ns {
		return true // filtered, but watcher is still alive
	}
	// Field selector filter.
	if w.fieldSel != nil && !w.fieldSel.Empty() {
		_, fieldSet, err := GetAttrs(obj)
		if err != nil || !w.fieldSel.Matches(fieldSet) {
			return true // filtered, but watcher is still alive
		}
	}
	ev := watch.Event{Type: eventType, Object: obj.DeepCopyObject()}
	select {
	case w.ch <- ev:
	default:
		// Buffer full — drop silently.
	}
	return true
}
```

- [ ] **Step 2: Verify it compiles**

```bash
cd /Users/i321040/go/src/github.com/piotrmiskiewicz/custom-api-server
/opt/homebrew/bin/go build ./pkg/registry/solution/...
```

Expected: no output (clean build).

- [ ] **Step 3: Commit**

```bash
git add pkg/registry/solution/watcher.go
git commit -m "feat: add shared watcher type for Watch implementation"
```

---

### Task 2: Implement Watch on `InMemoryStorage`

**Files:**
- Modify: `pkg/registry/solution/storage.go`
- Modify: `pkg/registry/solution/storage_test.go`

- [ ] **Step 1: Write failing tests in `storage_test.go`**

Add the following tests at the end of `pkg/registry/solution/storage_test.go`:

```go
func TestWatch_Create(t *testing.T) {
	store := registry.NewSolutionStorage()
	ctx := ctxWithNamespace("default")

	w, err := store.Watch(ctx, nil)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Stop()

	store.Create(ctx, &internal.Solution{
		ObjectMeta: metav1.ObjectMeta{Name: "w1", Namespace: "default"},
		Spec:       internal.SolutionSpec{SolutionName: "foo"},
	}, func(_ context.Context, _ runtime.Object) error { return nil }, &metav1.CreateOptions{})

	select {
	case ev := <-w.ResultChan():
		if ev.Type != watch.Added {
			t.Errorf("expected Added, got %v", ev.Type)
		}
		sol := ev.Object.(*internal.Solution)
		if sol.Name != "w1" {
			t.Errorf("expected name w1, got %s", sol.Name)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for watch event")
	}
}

func TestWatch_Update(t *testing.T) {
	store := registry.NewSolutionStorage()
	ctx := ctxWithNamespace("default")

	store.Create(ctx, &internal.Solution{
		ObjectMeta: metav1.ObjectMeta{Name: "w2", Namespace: "default"},
		Spec:       internal.SolutionSpec{SolutionName: "orig"},
	}, func(_ context.Context, _ runtime.Object) error { return nil }, &metav1.CreateOptions{})

	w, err := store.Watch(ctx, nil)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Stop()

	store.Update(ctx, "w2", registry.UpdateFunc(func(_ context.Context, obj runtime.Object, _ bool) (runtime.Object, bool, error) {
		s := obj.(*internal.Solution)
		s.Spec.SolutionName = "updated"
		return s, false, nil
	}), func(_ context.Context, _ runtime.Object) error { return nil },
		func(_ context.Context, _, _ runtime.Object) error { return nil },
		false, &metav1.UpdateOptions{})

	select {
	case ev := <-w.ResultChan():
		if ev.Type != watch.Modified {
			t.Errorf("expected Modified, got %v", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for watch event")
	}
}

func TestWatch_Delete(t *testing.T) {
	store := registry.NewSolutionStorage()
	ctx := ctxWithNamespace("default")

	store.Create(ctx, &internal.Solution{
		ObjectMeta: metav1.ObjectMeta{Name: "w3", Namespace: "default"},
	}, func(_ context.Context, _ runtime.Object) error { return nil }, &metav1.CreateOptions{})

	w, err := store.Watch(ctx, nil)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Stop()

	store.Delete(ctx, "w3", func(_ context.Context, _ runtime.Object) error { return nil }, &metav1.DeleteOptions{})

	select {
	case ev := <-w.ResultChan():
		if ev.Type != watch.Deleted {
			t.Errorf("expected Deleted, got %v", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for watch event")
	}
}

func TestWatch_NamespaceFilter(t *testing.T) {
	store := registry.NewSolutionStorage()
	ctxA := ctxWithNamespace("ns-a")
	ctxB := ctxWithNamespace("ns-b")

	wA, _ := store.Watch(ctxA, nil)
	wB, _ := store.Watch(ctxB, nil)
	defer wA.Stop()
	defer wB.Stop()

	store.Create(ctxA, &internal.Solution{
		ObjectMeta: metav1.ObjectMeta{Name: "sol-a", Namespace: "ns-a"},
	}, func(_ context.Context, _ runtime.Object) error { return nil }, &metav1.CreateOptions{})

	// wA should receive the event.
	select {
	case ev := <-wA.ResultChan():
		if ev.Type != watch.Added {
			t.Errorf("wA: expected Added, got %v", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("wA: timed out waiting for event")
	}

	// wB should receive nothing.
	select {
	case ev := <-wB.ResultChan():
		t.Errorf("wB: unexpected event %v", ev.Type)
	case <-time.After(100 * time.Millisecond):
		// expected
	}
}

func TestWatch_FieldSelectorFilter(t *testing.T) {
	store := registry.NewSolutionStorage()
	ctx := ctxWithNamespace("default")

	w, err := store.Watch(ctx, &metainternalversion.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("spec.solutionName", "alpha"),
	})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Stop()

	// Create one matching and one non-matching solution.
	store.Create(ctx, &internal.Solution{
		ObjectMeta: metav1.ObjectMeta{Name: "match", Namespace: "default"},
		Spec:       internal.SolutionSpec{SolutionName: "alpha"},
	}, func(_ context.Context, _ runtime.Object) error { return nil }, &metav1.CreateOptions{})

	store.Create(ctx, &internal.Solution{
		ObjectMeta: metav1.ObjectMeta{Name: "nomatch", Namespace: "default"},
		Spec:       internal.SolutionSpec{SolutionName: "beta"},
	}, func(_ context.Context, _ runtime.Object) error { return nil }, &metav1.CreateOptions{})

	// Should receive exactly one event (the matching one).
	select {
	case ev := <-w.ResultChan():
		sol := ev.Object.(*internal.Solution)
		if sol.Name != "match" {
			t.Errorf("expected name 'match', got %q", sol.Name)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for watch event")
	}

	// Should receive no second event.
	select {
	case ev := <-w.ResultChan():
		t.Errorf("unexpected second event: %v %v", ev.Type, ev.Object.(*internal.Solution).Name)
	case <-time.After(100 * time.Millisecond):
		// expected
	}
}

func TestWatch_Stop(t *testing.T) {
	store := registry.NewSolutionStorage()
	ctx := ctxWithNamespace("default")

	w, err := store.Watch(ctx, nil)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	w.Stop()

	store.Create(ctx, &internal.Solution{
		ObjectMeta: metav1.ObjectMeta{Name: "after-stop", Namespace: "default"},
	}, func(_ context.Context, _ runtime.Object) error { return nil }, &metav1.CreateOptions{})

	select {
	case _, ok := <-w.ResultChan():
		if ok {
			t.Error("expected channel to be closed, got event")
		}
		// closed channel — correct
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out — channel neither closed nor received event")
	}
}
```

Also add these imports to the test file's import block (add the missing ones):

```go
import (
	"context"
	"testing"
	"time"

	internal "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution"
	registry "github.com/piotrmiskiewicz/custom-api-server/pkg/registry/solution"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/endpoints/request"
)
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/i321040/go/src/github.com/piotrmiskiewicz/custom-api-server
/opt/homebrew/bin/go test ./pkg/registry/solution/... -run TestWatch -v 2>&1 | head -40
```

Expected: tests fail — `Watch` returns `MethodNotSupported`.

- [ ] **Step 3: Implement Watch on `InMemoryStorage` in `storage.go`**

**3a.** Add `watchers []*watcher` field to `InMemoryStorage`:

```go
type InMemoryStorage struct {
	mu       sync.RWMutex
	objects  map[string]*internal.Solution // key: namespace/name
	watchers []*watcher
}
```

**3b.** Add `broadcast` method (add after the `key` function):

```go
// broadcast sends an event to all active watchers and removes stopped ones.
// Must be called with mu held (write lock).
func (s *InMemoryStorage) broadcast(eventType watch.EventType, obj *internal.Solution) {
	alive := s.watchers[:0]
	for _, w := range s.watchers {
		if w.send(eventType, obj) {
			alive = append(alive, w)
		}
	}
	s.watchers = alive
}
```

**3c.** Replace the `Watch` stub with a real implementation:

```go
func (s *InMemoryStorage) Watch(ctx context.Context, opts *metainternalversion.ListOptions) (watch.Interface, error) {
	ns, _ := request.NamespaceFrom(ctx)

	var fieldSel fields.Selector
	if opts != nil && opts.FieldSelector != nil {
		if err := validateFieldSelector(opts.FieldSelector); err != nil {
			return nil, errors.NewBadRequest(err.Error())
		}
		fieldSel = opts.FieldSelector
	}

	wctx, cancel := context.WithCancel(ctx)
	w := newWatcher(ns, fieldSel, cancel)

	s.mu.Lock()
	s.watchers = append(s.watchers, w)
	s.mu.Unlock()

	go func() {
		<-wctx.Done()
		w.Stop()
		s.mu.Lock()
		alive := s.watchers[:0]
		for _, ww := range s.watchers {
			if !ww.isStopped() {
				alive = append(alive, ww)
			}
		}
		s.watchers = alive
		s.mu.Unlock()
	}()

	return w, nil
}
```

**3d.** Add `broadcast` calls in `Create`, `Update`, and `Delete`. In `Create`, add after storing the object (still inside the `mu.Lock`):

```go
// inside Create, after: s.objects[k] = cp
s.broadcast(watch.Added, cp)
```

In `Update`, add after storing the updated object:

```go
// inside Update, after: s.objects[k] = cp
s.broadcast(watch.Modified, cp)
```

In `Delete`, add after deleting the object:

```go
// inside Delete, after: delete(s.objects, k)
s.broadcast(watch.Deleted, obj)
```

**3e.** Add `"k8s.io/apimachinery/pkg/fields"` to the import block of `storage.go` if not already present (it is already imported).

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/i321040/go/src/github.com/piotrmiskiewicz/custom-api-server
/opt/homebrew/bin/go test ./pkg/registry/solution/... -run TestWatch -v
```

Expected: all 6 `TestWatch_*` tests PASS.

- [ ] **Step 5: Run full test suite to check for regressions**

```bash
/opt/homebrew/bin/go test ./pkg/registry/solution/... -v
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/registry/solution/storage.go pkg/registry/solution/storage_test.go
git commit -m "feat: implement Watch on InMemoryStorage with namespace and field selector filtering"
```

---

### Task 3: Implement Watch on `PostgresStorage` (LISTEN/NOTIFY)

**Files:**
- Modify: `pkg/registry/solution/storage_postgres.go`

No new tests (requires live postgres — consistent with existing pattern).

- [ ] **Step 1: Add `watchMu`, `watchers`, `listenerCancel` fields to `PostgresStorage`**

```go
type PostgresStorage struct {
	db             *pgxpool.Pool
	dsn            string
	watchMu        sync.Mutex
	watchers       []*watcher
	listenerCancel context.CancelFunc
}
```

Also update `NewPostgresStorage` to store the DSN and start the listener:

```go
func NewPostgresStorage(ctx context.Context, dsn string) (*PostgresStorage, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if _, err := pool.Exec(ctx, createTableSQL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("create table: %w", err)
	}

	lctx, cancel := context.WithCancel(context.Background())
	s := &PostgresStorage{
		db:             pool,
		dsn:            dsn,
		listenerCancel: cancel,
	}
	go s.listenLoop(lctx)
	return s, nil
}
```

- [ ] **Step 2: Add `broadcast` method to `PostgresStorage`**

Add after `NewPostgresStorage`:

```go
// broadcast sends an event to all active watchers and removes stopped ones.
func (s *PostgresStorage) broadcast(eventType watch.EventType, obj *internal.Solution) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	alive := s.watchers[:0]
	for _, w := range s.watchers {
		if w.send(eventType, obj) {
			alive = append(alive, w)
		}
	}
	s.watchers = alive
}
```

- [ ] **Step 3: Define the notify payload struct and helpers**

Add after `broadcast`:

```go
type notifyPayload struct {
	Type               string          `json:"type"`
	Namespace          string          `json:"namespace"`
	Name               string          `json:"name"`
	UID                string          `json:"uid"`
	ResourceVersion    int             `json:"resource_version"`
	CreationTimestamp  time.Time       `json:"creation_timestamp"`
	Labels             json.RawMessage `json:"labels"`
	SpecSolutionName   string          `json:"spec_solution_name"`
	StatusPhase        string          `json:"status_phase"`
	StatusConditions   json.RawMessage `json:"status_conditions"`
}

func (s *PostgresStorage) notifyPayloadFor(eventType string, sol *internal.Solution) (string, error) {
	labelsJSON, err := json.Marshal(sol.Labels)
	if err != nil {
		return "", err
	}
	conditionsJSON, err := json.Marshal(sol.Status.Conditions)
	if err != nil {
		return "", err
	}
	rv, _ := strconv.Atoi(sol.ResourceVersion)
	p := notifyPayload{
		Type:              eventType,
		Namespace:         sol.Namespace,
		Name:              sol.Name,
		UID:               string(sol.UID),
		ResourceVersion:   rv,
		CreationTimestamp: sol.CreationTimestamp.Time,
		Labels:            labelsJSON,
		SpecSolutionName:  sol.Spec.SolutionName,
		StatusPhase:       string(sol.Status.Phase),
		StatusConditions:  conditionsJSON,
	}
	b, err := json.Marshal(p)
	return string(b), err
}

func payloadToSolution(p *notifyPayload) (*internal.Solution, error) {
	return buildSolution(
		p.Namespace, p.Name, p.UID, p.ResourceVersion,
		p.CreationTimestamp, p.Labels,
		p.SpecSolutionName, p.StatusPhase, p.StatusConditions,
	)
}
```

- [ ] **Step 4: Add `pg_notify` calls in `Create`, `Update`, `Delete`**

In `Create`, after the `INSERT` succeeds (after `if err != nil { ... }` block, before `return sol.DeepCopyObject(), nil`):

```go
if payload, err := s.notifyPayloadFor("ADDED", sol); err == nil {
	_, _ = s.db.Exec(ctx, `SELECT pg_notify('solutions', $1)`, payload)
}
return sol.DeepCopyObject(), nil
```

In `Update`, replace the final `return sol.DeepCopyObject(), false, nil` with:

```go
if payload, err := s.notifyPayloadFor("MODIFIED", sol); err == nil {
	_, _ = s.db.Exec(ctx, `SELECT pg_notify('solutions', $1)`, payload)
}
return sol.DeepCopyObject(), false, nil
```

In `Delete`, replace the final `return existing, true, nil` with:

```go
if payload, err := s.notifyPayloadFor("DELETED", existing.(*internal.Solution)); err == nil {
	_, _ = s.db.Exec(ctx, `SELECT pg_notify('solutions', $1)`, payload)
}
return existing, true, nil
```

- [ ] **Step 5: Implement `listenLoop`**

Add after `payloadToSolution`:

```go
// listenLoop runs for the lifetime of the storage, listening for NOTIFY on the
// "solutions" channel and broadcasting events to registered watchers.
// It retries on connection loss with exponential backoff.
func (s *PostgresStorage) listenLoop(ctx context.Context) {
	backoff := time.Second
	const maxBackoff = 16 * time.Second
	const maxAttempts = 5

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return
		}
		if err := s.listenOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			// Connection lost — wait before retry.
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}
		// Clean exit (ctx cancelled).
		return
	}
	// All attempts exhausted — log and stop.
	fmt.Fprintf(os.Stderr, "postgres watch: listener failed after %d attempts, no more watch events\n", maxAttempts)
}

// listenOnce opens a dedicated pgconn connection, runs LISTEN, and loops on
// WaitForNotification until the context is cancelled or an error occurs.
func (s *PostgresStorage) listenOnce(ctx context.Context) error {
	conn, err := pgconn.Connect(ctx, s.dsn)
	if err != nil {
		return fmt.Errorf("pgconn.Connect: %w", err)
	}
	defer conn.Close(context.Background())

	if _, err := conn.Exec(ctx, "LISTEN solutions").ReadAll(); err != nil {
		return fmt.Errorf("LISTEN: %w", err)
	}

	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil // clean shutdown
			}
			return err // connection lost
		}

		var p notifyPayload
		if err := json.Unmarshal([]byte(n.Payload), &p); err != nil {
			continue // malformed payload — skip
		}
		sol, err := payloadToSolution(&p)
		if err != nil {
			continue
		}

		var eventType watch.EventType
		switch p.Type {
		case "ADDED":
			eventType = watch.Added
		case "MODIFIED":
			eventType = watch.Modified
		case "DELETED":
			eventType = watch.Deleted
		default:
			continue
		}
		s.broadcast(eventType, sol)
	}
}
```

- [ ] **Step 6: Implement `Watch` on `PostgresStorage`**

Replace the existing `Watch` stub:

```go
func (s *PostgresStorage) Watch(ctx context.Context, opts *metainternalversion.ListOptions) (watch.Interface, error) {
	ns, _ := request.NamespaceFrom(ctx)

	var fieldSel fields.Selector
	if opts != nil && opts.FieldSelector != nil {
		if err := validateFieldSelector(opts.FieldSelector); err != nil {
			return nil, errors.NewBadRequest(err.Error())
		}
		fieldSel = opts.FieldSelector
	}

	wctx, cancel := context.WithCancel(ctx)
	w := newWatcher(ns, fieldSel, cancel)

	s.watchMu.Lock()
	s.watchers = append(s.watchers, w)
	s.watchMu.Unlock()

	go func() {
		<-wctx.Done()
		w.Stop()
		s.watchMu.Lock()
		alive := s.watchers[:0]
		for _, ww := range s.watchers {
			if !ww.isStopped() {
				alive = append(alive, ww)
			}
		}
		s.watchers = alive
		s.watchMu.Unlock()
	}()

	return w, nil
}
```

- [ ] **Step 7: Update `Destroy` to stop all watchers and cancel the listener**

Replace the existing `Destroy`:

```go
func (s *PostgresStorage) Destroy() {
	s.listenerCancel()
	s.watchMu.Lock()
	for _, w := range s.watchers {
		w.Stop()
	}
	s.watchers = nil
	s.watchMu.Unlock()
	s.db.Close()
}
```

- [ ] **Step 8: Add missing imports to `storage_postgres.go`**

Ensure the import block includes:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	internal "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution"
	"k8s.io/apimachinery/pkg/api/errors"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
)
```

> Note: `pgconn` is a sub-package of `pgx/v5` at `github.com/jackc/pgx/v5/pgconn` — already present as an indirect dependency.

- [ ] **Step 9: Verify it compiles**

```bash
cd /Users/i321040/go/src/github.com/piotrmiskiewicz/custom-api-server
/opt/homebrew/bin/go build ./pkg/registry/solution/...
```

Expected: no output (clean build).

- [ ] **Step 10: Run full test suite to check for regressions**

```bash
/opt/homebrew/bin/go test ./pkg/registry/solution/... -v
```

Expected: all tests PASS (the new Watch tests from Task 2 still pass; postgres tests are unaffected).

- [ ] **Step 11: Commit**

```bash
git add pkg/registry/solution/storage_postgres.go
git commit -m "feat: implement Watch on PostgresStorage using LISTEN/NOTIFY"
```

# Aggregated API Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Kubernetes Aggregated API Server for the `solution.piotrmiskiewicz.github.com` group that serves the `Solution` type with in-memory storage and a `/status` subresource.

**Architecture:** Uses `k8s.io/apiserver`'s `GenericAPIServer` with a hand-rolled in-memory `map`-backed REST storage. Internal types mirror versioned types and are registered with a shared `runtime.Scheme`; a `CodecFactory` handles serialization. The server listens on plain HTTP with no auth.

**Tech Stack:** Go 1.26, `k8s.io/apiserver v0.36.0`, `k8s.io/apimachinery v0.36.0`

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `go.mod` / `go.sum` | Modify | Add `k8s.io/apiserver` and `k8s.io/client-go` dependencies |
| `pkg/apis/solution/v1alpha1/types.go` | Modify | Add `DeepCopyObject()` on `Solution` and `SolutionList` |
| `pkg/apis/solution/v1alpha1/register.go` | Create | `AddToScheme` for versioned types, `SchemeGroupVersion` const |
| `pkg/apis/solution/types.go` | Create | Internal (unversioned) `Solution`, `SolutionList`, `SolutionSpec`, `SolutionStatus` types |
| `pkg/apis/solution/register.go` | Create | `AddToScheme` for internal types, `Kind()` helper |
| `pkg/apis/solution/v1alpha1/conversion.go` | Create | `Convert_v1alpha1_Solution_To_solution_Solution` and inverse |
| `pkg/registry/solution/storage.go` | Create | `SolutionStorage` (in-memory map + `StandardStorage`) and `StatusREST` |
| `pkg/apiserver/server.go` | Create | Build `GenericAPIServer`, register API group, export `New()` |
| `cmd/server/main.go` | Create | `main()` entrypoint |

---

### Task 1: Add `k8s.io/apiserver` dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add the dependency**

```bash
cd /Users/i321040/go/src/github.com/piotrmiskiewicz/custom-api-server
go get k8s.io/apiserver@v0.36.0
go get k8s.io/client-go@v0.36.0
go mod tidy
```

Expected: `go.mod` now has `require k8s.io/apiserver v0.36.0` and `require k8s.io/client-go v0.36.0`; `go.sum` updated.

- [ ] **Step 2: Verify the module graph compiles**

```bash
go build ./...
```

Expected: no output (only `pkg/apis/solution/v1alpha1` exists so far, compiles cleanly).

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add k8s.io/apiserver and client-go v0.36.0"
```

---

### Task 2: Add `DeepCopyObject` to versioned types

The `k8s.io/apiserver` framework requires every type registered in the scheme to implement `runtime.Object`, which means it needs a `DeepCopyObject()` method. We add these to the existing `types.go`.

**Files:**
- Modify: `pkg/apis/solution/v1alpha1/types.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/apis/solution/v1alpha1/types_test.go`:

```go
package v1alpha1_test

import (
	"testing"

	"github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSolution_DeepCopyObject(t *testing.T) {
	orig := &v1alpha1.Solution{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec:       v1alpha1.SolutionSpec{SolutionName: "my-solution"},
		Status:     v1alpha1.SolutionStatus{Phase: v1alpha1.PhasePending},
	}
	copy := orig.DeepCopyObject()
	if copy == orig {
		t.Fatal("DeepCopyObject returned same pointer")
	}
	s, ok := copy.(*v1alpha1.Solution)
	if !ok {
		t.Fatalf("expected *Solution, got %T", copy)
	}
	if s.Name != orig.Name {
		t.Errorf("Name mismatch: %s != %s", s.Name, orig.Name)
	}
}

func TestSolutionList_DeepCopyObject(t *testing.T) {
	orig := &v1alpha1.SolutionList{
		Items: []v1alpha1.Solution{
			{ObjectMeta: metav1.ObjectMeta{Name: "a"}},
		},
	}
	copy := orig.DeepCopyObject()
	if copy == orig {
		t.Fatal("DeepCopyObject returned same pointer")
	}
	sl, ok := copy.(*v1alpha1.SolutionList)
	if !ok {
		t.Fatalf("expected *SolutionList, got %T", copy)
	}
	if len(sl.Items) != 1 {
		t.Errorf("expected 1 item, got %d", len(sl.Items))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/apis/solution/v1alpha1/...
```

Expected: compile error — `Solution` has no method `DeepCopyObject`.

- [ ] **Step 3: Add DeepCopy methods to `types.go`**

Append to `pkg/apis/solution/v1alpha1/types.go`:

```go
func (in *Solution) DeepCopyObject() runtime.Object {
	out := new(Solution)
	*out = *in
	out.Conditions = append([]metav1.Condition(nil), in.Status.Conditions...)
	out.Status.Conditions = out.Conditions
	return out
}

func (in *SolutionList) DeepCopyObject() runtime.Object {
	out := new(SolutionList)
	*out = *in
	out.Items = make([]Solution, len(in.Items))
	for i := range in.Items {
		in.Items[i].DeepCopyObject() // reuse
		out.Items[i] = *in.Items[i].DeepCopyObject().(*Solution)
	}
	return out
}
```

Also add the missing import at the top of `types.go`:

```go
import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/apis/solution/v1alpha1/...
```

Expected: `PASS`

- [ ] **Step 5: Commit**

```bash
git add pkg/apis/solution/v1alpha1/types.go pkg/apis/solution/v1alpha1/types_test.go
git commit -m "feat: add DeepCopyObject to versioned Solution types"
```

---

### Task 3: Versioned type scheme registration (`v1alpha1/register.go`)

**Files:**
- Create: `pkg/apis/solution/v1alpha1/register.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/apis/solution/v1alpha1/register_test.go`:

```go
package v1alpha1_test

import (
	"testing"

	"github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestAddToScheme(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme failed: %v", err)
	}
	gvk := schema.GroupVersionKind{
		Group:   "solution.piotrmiskiewicz.github.com",
		Version: "v1alpha1",
		Kind:    "Solution",
	}
	if !scheme.Recognizes(gvk) {
		t.Errorf("scheme does not recognize %v", gvk)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/apis/solution/v1alpha1/...
```

Expected: compile error — `v1alpha1.AddToScheme` undefined.

- [ ] **Step 3: Create `pkg/apis/solution/v1alpha1/register.go`**

```go
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// SchemeGroupVersion is the group/version for this package.
var SchemeGroupVersion = schema.GroupVersion{
	Group:   "solution.piotrmiskiewicz.github.com",
	Version: "v1alpha1",
}

// Resource takes an unqualified resource and returns a GroupResource.
func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

// AddToScheme registers the versioned types into the given scheme.
var AddToScheme = func(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&Solution{},
		&SolutionList{},
	)
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/apis/solution/v1alpha1/...
```

Expected: `PASS`

- [ ] **Step 5: Commit**

```bash
git add pkg/apis/solution/v1alpha1/register.go pkg/apis/solution/v1alpha1/register_test.go
git commit -m "feat: add versioned scheme registration for solution/v1alpha1"
```

---

### Task 4: Internal types and scheme registration

**Files:**
- Create: `pkg/apis/solution/types.go`
- Create: `pkg/apis/solution/register.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/apis/solution/register_test.go`:

```go
package solution_test

import (
	"testing"

	"github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestInternalAddToScheme(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := solution.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme failed: %v", err)
	}
	gvk := schema.GroupVersionKind{
		Group:   "solution.piotrmiskiewicz.github.com",
		Version: runtime.APIVersionInternal,
		Kind:    "Solution",
	}
	if !scheme.Recognizes(gvk) {
		t.Errorf("scheme does not recognize internal GVK %v", gvk)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/apis/solution/...
```

Expected: compile error — package `solution` doesn't exist yet.

- [ ] **Step 3: Create `pkg/apis/solution/types.go`**

```go
package solution

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Solution is the internal (unversioned) representation.
type Solution struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Spec   SolutionSpec
	Status SolutionStatus
}

func (in *Solution) DeepCopyObject() runtime.Object {
	out := new(Solution)
	*out = *in
	out.Status.Conditions = append([]metav1.Condition(nil), in.Status.Conditions...)
	return out
}

type SolutionSpec struct {
	SolutionName string
}

type Phase string

const (
	PhasePending    Phase = "Pending"
	PhaseScheduling Phase = "Scheduling"
	PhaseDeploying  Phase = "Deploying"
	PhaseRunning    Phase = "Running"
	PhaseFailed     Phase = "Failed"
	PhaseDeleting   Phase = "Deleting"
)

type SolutionStatus struct {
	Phase      Phase
	Conditions []metav1.Condition
}

// SolutionList is the internal list type.
type SolutionList struct {
	metav1.TypeMeta
	metav1.ListMeta

	Items []Solution
}

func (in *SolutionList) DeepCopyObject() runtime.Object {
	out := new(SolutionList)
	*out = *in
	out.Items = make([]Solution, len(in.Items))
	for i := range in.Items {
		out.Items[i] = *in.Items[i].DeepCopyObject().(*Solution)
	}
	return out
}
```

- [ ] **Step 4: Create `pkg/apis/solution/register.go`**

```go
package solution

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// SchemeGroupVersion is the internal group version.
var SchemeGroupVersion = schema.GroupVersion{
	Group:   "solution.piotrmiskiewicz.github.com",
	Version: runtime.APIVersionInternal,
}

// Kind takes an unqualified kind and returns a GroupKind.
func Kind(kind string) schema.GroupKind {
	return SchemeGroupVersion.WithKind(kind).GroupKind()
}

// Resource takes an unqualified resource and returns a GroupResource.
func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

// AddToScheme registers the internal types into the given scheme.
var AddToScheme = func(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&Solution{},
		&SolutionList{},
	)
	return nil
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
go test ./pkg/apis/solution/...
```

Expected: `PASS`

- [ ] **Step 6: Commit**

```bash
git add pkg/apis/solution/types.go pkg/apis/solution/register.go pkg/apis/solution/register_test.go
git commit -m "feat: add internal solution types and scheme registration"
```

---

### Task 5: Conversion functions (internal ↔ v1alpha1)

**Files:**
- Create: `pkg/apis/solution/v1alpha1/conversion.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/apis/solution/v1alpha1/conversion_test.go`:

```go
package v1alpha1_test

import (
	"testing"

	internal "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution"
	"github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/conversion"
)

func TestConvertV1alpha1ToInternal(t *testing.T) {
	src := &v1alpha1.Solution{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec:       v1alpha1.SolutionSpec{SolutionName: "my-solution"},
		Status:     v1alpha1.SolutionStatus{Phase: v1alpha1.PhaseRunning},
	}
	dst := &internal.Solution{}
	scope := conversion.Scope(nil)
	if err := v1alpha1.Convert_v1alpha1_Solution_To_solution_Solution(src, dst, scope); err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	if dst.Name != "test" {
		t.Errorf("Name: got %q, want %q", dst.Name, "test")
	}
	if dst.Spec.SolutionName != "my-solution" {
		t.Errorf("SolutionName: got %q, want %q", dst.Spec.SolutionName, "my-solution")
	}
	if dst.Status.Phase != internal.PhaseRunning {
		t.Errorf("Phase: got %q, want %q", dst.Status.Phase, internal.PhaseRunning)
	}
}

func TestConvertInternalToV1alpha1(t *testing.T) {
	src := &internal.Solution{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec:       internal.SolutionSpec{SolutionName: "my-solution"},
		Status:     internal.SolutionStatus{Phase: internal.PhaseFailed},
	}
	dst := &v1alpha1.Solution{}
	scope := conversion.Scope(nil)
	if err := v1alpha1.Convert_solution_Solution_To_v1alpha1_Solution(src, dst, scope); err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	if dst.Status.Phase != v1alpha1.PhaseFailed {
		t.Errorf("Phase: got %q, want %q", dst.Status.Phase, v1alpha1.PhaseFailed)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/apis/solution/v1alpha1/...
```

Expected: compile error — conversion functions undefined.

- [ ] **Step 3: Create `pkg/apis/solution/v1alpha1/conversion.go`**

```go
package v1alpha1

import (
	internal "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution"
	"k8s.io/apimachinery/pkg/conversion"
)

// Convert_v1alpha1_Solution_To_solution_Solution converts versioned → internal.
func Convert_v1alpha1_Solution_To_solution_Solution(in *Solution, out *internal.Solution, _ conversion.Scope) error {
	out.ObjectMeta = in.ObjectMeta
	out.TypeMeta = in.TypeMeta
	out.Spec.SolutionName = in.Spec.SolutionName
	out.Status.Phase = internal.Phase(in.Status.Phase)
	out.Status.Conditions = append(out.Status.Conditions[:0], in.Status.Conditions...)
	return nil
}

// Convert_solution_Solution_To_v1alpha1_Solution converts internal → versioned.
func Convert_solution_Solution_To_v1alpha1_Solution(in *internal.Solution, out *Solution, _ conversion.Scope) error {
	out.ObjectMeta = in.ObjectMeta
	out.TypeMeta = in.TypeMeta
	out.Spec.SolutionName = in.Spec.SolutionName
	out.Status.Phase = Phase(in.Status.Phase)
	out.Status.Conditions = append(out.Status.Conditions[:0], in.Status.Conditions...)
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/apis/solution/v1alpha1/...
```

Expected: `PASS`

- [ ] **Step 5: Commit**

```bash
git add pkg/apis/solution/v1alpha1/conversion.go pkg/apis/solution/v1alpha1/conversion_test.go
git commit -m "feat: add conversion functions between internal and v1alpha1 Solution types"
```

---

### Task 6: In-memory storage (`registry/solution/storage.go`)

**Files:**
- Create: `pkg/registry/solution/storage.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/registry/solution/storage_test.go`:

```go
package solution_test

import (
	"context"
	"testing"

	internal "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution"
	registry "github.com/piotrmiskiewicz/custom-api-server/pkg/registry/solution"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/endpoints/request"
)

func ctxWithNamespace(ns string) context.Context {
	return request.WithNamespace(context.Background(), ns)
}

func TestStorage_CreateAndGet(t *testing.T) {
	store := registry.NewSolutionStorage()
	ctx := ctxWithNamespace("default")

	obj, err := store.Create(ctx, &internal.Solution{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "default"},
		Spec:       internal.SolutionSpec{SolutionName: "my-solution"},
	}, func(ctx context.Context, obj runtime.Object) error { return nil }, &metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	created := obj.(*internal.Solution)
	if created.UID == "" {
		t.Error("UID not set on create")
	}
	if created.ResourceVersion != "1" {
		t.Errorf("ResourceVersion: got %q, want %q", created.ResourceVersion, "1")
	}

	got, err := store.Get(ctx, "s1", &metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.(*internal.Solution).Name != "s1" {
		t.Error("Name mismatch after Get")
	}
}

func TestStorage_List(t *testing.T) {
	store := registry.NewSolutionStorage()
	ctx := ctxWithNamespace("default")

	for _, name := range []string{"a", "b"} {
		_, err := store.Create(ctx, &internal.Solution{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		}, func(ctx context.Context, obj runtime.Object) error { return nil }, &metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}

	list, err := store.List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	sl := list.(*internal.SolutionList)
	if len(sl.Items) != 2 {
		t.Errorf("List: got %d items, want 2", len(sl.Items))
	}
}

func TestStorage_UpdateAndDelete(t *testing.T) {
	store := registry.NewSolutionStorage()
	ctx := ctxWithNamespace("default")

	store.Create(ctx, &internal.Solution{
		ObjectMeta: metav1.ObjectMeta{Name: "s2", Namespace: "default"},
	}, func(ctx context.Context, obj runtime.Object) error { return nil }, &metav1.CreateOptions{})

	updated, _, err := store.Update(ctx, "s2", registry.UpdateFunc(func(ctx context.Context, obj runtime.Object, creating bool) (runtime.Object, bool, error) {
		s := obj.(*internal.Solution)
		s.Spec.SolutionName = "updated"
		return s, false, nil
	}), func(ctx context.Context, obj runtime.Object) error { return nil }, func(ctx context.Context, obj, old runtime.Object) error { return nil }, false, &metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.(*internal.Solution).ResourceVersion == "1" {
		t.Error("ResourceVersion should have been incremented")
	}

	_, _, err = store.Delete(ctx, "s2", func(ctx context.Context, obj runtime.Object) error { return nil }, &metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = store.Get(ctx, "s2", &metav1.GetOptions{})
	if err == nil {
		t.Error("expected not-found error after delete")
	}
}

func TestStatusREST_Update(t *testing.T) {
	store := registry.NewSolutionStorage()
	statusREST := registry.NewStatusREST(store)
	ctx := ctxWithNamespace("default")

	store.Create(ctx, &internal.Solution{
		ObjectMeta: metav1.ObjectMeta{Name: "s3", Namespace: "default"},
		Spec:       internal.SolutionSpec{SolutionName: "orig"},
	}, func(ctx context.Context, obj runtime.Object) error { return nil }, &metav1.CreateOptions{})

	updated, _, err := statusREST.Update(ctx, "s3", registry.UpdateFunc(func(ctx context.Context, obj runtime.Object, creating bool) (runtime.Object, bool, error) {
		s := obj.(*internal.Solution)
		s.Status.Phase = internal.PhaseRunning
		s.Spec.SolutionName = "should-not-change"
		return s, false, nil
	}), func(ctx context.Context, obj runtime.Object) error { return nil }, func(ctx context.Context, obj, old runtime.Object) error { return nil }, false, &metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("StatusREST.Update: %v", err)
	}
	s := updated.(*internal.Solution)
	if s.Status.Phase != internal.PhaseRunning {
		t.Errorf("Phase: got %q, want %q", s.Status.Phase, internal.PhaseRunning)
	}
	if s.Spec.SolutionName != "orig" {
		t.Errorf("Spec must not change via status update, got %q", s.Spec.SolutionName)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/registry/solution/...
```

Expected: compile error — package `registry/solution` doesn't exist.

- [ ] **Step 3: Create `pkg/registry/solution/storage.go`**

```go
package solution

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	internal "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
)

var solutionGR = schema.GroupResource{Group: "solution.piotrmiskiewicz.github.com", Resource: "solutions"}

// UpdateFunc is a helper type so tests can pass an inline updater.
type UpdateFunc rest.UpdatedObjectInfo

// SolutionStorage is an in-memory REST storage for Solution objects.
type SolutionStorage struct {
	mu      sync.RWMutex
	objects map[string]*internal.Solution // key: namespace/name
}

// NewSolutionStorage creates an empty SolutionStorage.
func NewSolutionStorage() *SolutionStorage {
	return &SolutionStorage{objects: make(map[string]*internal.Solution)}
}

func key(ns, name string) string { return ns + "/" + name }

// --- rest.Scoper ---

func (s *SolutionStorage) NamespaceScoped() bool { return true }

// --- rest.StandardStorage ---

func (s *SolutionStorage) New() runtime.Object { return &internal.Solution{} }

func (s *SolutionStorage) NewList() runtime.Object { return &internal.SolutionList{} }

func (s *SolutionStorage) Destroy() {}

func (s *SolutionStorage) Get(ctx context.Context, name string, _ *metav1.GetOptions) (runtime.Object, error) {
	ns, _ := request.NamespaceFrom(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	obj, ok := s.objects[key(ns, name)]
	if !ok {
		return nil, errors.NewNotFound(solutionGR, name)
	}
	return obj.DeepCopyObject(), nil
}

func (s *SolutionStorage) List(ctx context.Context, _ *metainternalversion.ListOptions) (runtime.Object, error) {
	ns, _ := request.NamespaceFrom(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := &internal.SolutionList{}
	for k, v := range s.objects {
		if ns == "" || k[:len(ns)] == ns {
			list.Items = append(list.Items, *v.DeepCopyObject().(*internal.Solution))
		}
	}
	return list, nil
}

func (s *SolutionStorage) Create(ctx context.Context, obj runtime.Object, createValidation rest.ValidateObjectFunc, _ *metav1.CreateOptions) (runtime.Object, error) {
	sol := obj.(*internal.Solution)
	ns, _ := request.NamespaceFrom(ctx)
	if sol.Namespace == "" {
		sol.Namespace = ns
	}
	if err := createValidation(ctx, obj); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(ns, sol.Name)
	if _, exists := s.objects[k]; exists {
		return nil, errors.NewAlreadyExists(solutionGR, sol.Name)
	}
	sol.UID = types.UID(uuid.NewUUID())
	sol.ResourceVersion = "1"
	now := metav1.NewTime(time.Now())
	sol.CreationTimestamp = now
	cp := sol.DeepCopyObject().(*internal.Solution)
	s.objects[k] = cp
	return cp.DeepCopyObject(), nil
}

func (s *SolutionStorage) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo, createValidation rest.ValidateObjectFunc, updateValidation rest.ValidateObjectUpdateFunc, _ bool, _ *metav1.UpdateOptions) (runtime.Object, bool, error) {
	ns, _ := request.NamespaceFrom(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(ns, name)
	existing, ok := s.objects[k]
	if !ok {
		return nil, false, errors.NewNotFound(solutionGR, name)
	}
	updated, err := objInfo.UpdatedObject(ctx, existing)
	if err != nil {
		return nil, false, err
	}
	if err := updateValidation(ctx, updated, existing); err != nil {
		return nil, false, err
	}
	sol := updated.(*internal.Solution)
	rv, _ := strconv.Atoi(existing.ResourceVersion)
	sol.ResourceVersion = strconv.Itoa(rv + 1)
	cp := sol.DeepCopyObject().(*internal.Solution)
	s.objects[k] = cp
	return cp.DeepCopyObject(), false, nil
}

func (s *SolutionStorage) Delete(ctx context.Context, name string, deleteValidation rest.ValidateObjectFunc, _ *metav1.DeleteOptions) (runtime.Object, bool, error) {
	ns, _ := request.NamespaceFrom(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(ns, name)
	obj, ok := s.objects[k]
	if !ok {
		return nil, false, errors.NewNotFound(solutionGR, name)
	}
	if err := deleteValidation(ctx, obj); err != nil {
		return nil, false, err
	}
	delete(s.objects, k)
	return obj, true, nil
}

func (s *SolutionStorage) Watch(_ context.Context, _ *metainternalversion.ListOptions) (watch.Interface, error) {
	return nil, errors.NewMethodNotSupported(solutionGR, "watch")
}

func (s *SolutionStorage) ConvertToTable(ctx context.Context, obj runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	return rest.NewDefaultTableConvertor(solutionGR).ConvertToTable(ctx, obj, tableOptions)
}

// --- StatusREST ---

// StatusREST handles updates to the /status subresource.
type StatusREST struct {
	store *SolutionStorage
}

// NewStatusREST creates a StatusREST that shares the given SolutionStorage.
func NewStatusREST(store *SolutionStorage) *StatusREST {
	return &StatusREST{store: store}
}

func (r *StatusREST) New() runtime.Object { return &internal.Solution{} }

func (r *StatusREST) Destroy() {}

func (r *StatusREST) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo, createValidation rest.ValidateObjectFunc, updateValidation rest.ValidateObjectUpdateFunc, forceAllowCreate bool, options *metav1.UpdateOptions) (runtime.Object, bool, error) {
	ns, _ := request.NamespaceFrom(ctx)
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	k := key(ns, name)
	existing, ok := r.store.objects[k]
	if !ok {
		return nil, false, errors.NewNotFound(solutionGR, name)
	}
	updated, err := objInfo.UpdatedObject(ctx, existing)
	if err != nil {
		return nil, false, err
	}
	updatedSol := updated.(*internal.Solution)
	// Only copy status — spec and metadata remain from existing.
	existing.Status = updatedSol.Status
	rv, _ := strconv.Atoi(existing.ResourceVersion)
	existing.ResourceVersion = strconv.Itoa(rv + 1)
	cp := existing.DeepCopyObject().(*internal.Solution)
	r.store.objects[k] = cp
	return cp.DeepCopyObject(), false, nil
}

// UpdateFunc adapts a plain function to rest.UpdatedObjectInfo for use in tests.
type UpdateFunc func(ctx context.Context, obj runtime.Object, creating bool) (runtime.Object, bool, error)

func (f UpdateFunc) Preconditions() *metav1.Preconditions { return nil }

func (f UpdateFunc) UpdatedObject(ctx context.Context, oldObj runtime.Object) (runtime.Object, error) {
	obj, _, err := f(ctx, oldObj.DeepCopyObject(), false)
	return obj, err
}
```

> **Note on `metainternalversion`**: The `List` and `Watch` signatures use `*metainternalversion.ListOptions` from `k8s.io/apimachinery/pkg/apis/meta/internalversion`. Add this import where needed.

- [ ] **Step 4: Fix imports in storage.go**

The full import block for `storage.go`:

```go
import (
	"context"
	"strconv"
	"sync"
	"time"

	internal "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution"
	"k8s.io/apimachinery/pkg/api/errors"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
)
```

Remove the unused `fmt` import and the `_ = fmt.Sprintf` placeholder if present.

- [ ] **Step 5: Run test to verify it passes**

```bash
go test ./pkg/registry/solution/...
```

Expected: `PASS`

- [ ] **Step 6: Commit**

```bash
git add pkg/registry/solution/storage.go pkg/registry/solution/storage_test.go
git commit -m "feat: add in-memory SolutionStorage and StatusREST"
```

---

### Task 7: Server wiring (`pkg/apiserver/server.go`)

**Files:**
- Create: `pkg/apiserver/server.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/apiserver/server_test.go`:

```go
package apiserver_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/piotrmiskiewicz/custom-api-server/pkg/apiserver"
)

func TestNew_ReturnsServer(t *testing.T) {
	srv, err := apiserver.New()
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if srv == nil {
		t.Fatal("New() returned nil")
	}
}

func TestHealthz(t *testing.T) {
	srv, err := apiserver.New()
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	handler := srv.Handler
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("/healthz: got %d, want 200", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/apiserver/...
```

Expected: compile error — package `apiserver` doesn't exist.

- [ ] **Step 3: Create `pkg/apiserver/server.go`**

```go
package apiserver

import (
	internal "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution"
	"github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution/v1alpha1"
	registry "github.com/piotrmiskiewicz/custom-api-server/pkg/registry/solution"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/server/options"
)

var (
	// Scheme is the runtime.Scheme that holds all registered types.
	Scheme = runtime.NewScheme()
	// Codecs is the CodecFactory built from Scheme.
	Codecs = serializer.NewCodecFactory(Scheme)
)

func init() {
	// Register internal types.
	internal.AddToScheme(Scheme)
	// Register versioned types.
	v1alpha1.AddToScheme(Scheme)
	// Register conversions.
	registerConversions(Scheme)
	// Register Kubernetes meta types needed by the framework.
	metav1.AddToGroupVersion(Scheme, schema.GroupVersion{Version: "v1"})
	// Unversioned types (Status, etc).
	unversioned := schema.GroupVersion{Group: "", Version: "v1"}
	Scheme.AddUnversionedTypes(unversioned,
		&metav1.Status{},
		&metav1.APIVersions{},
		&metav1.APIGroupList{},
		&metav1.APIGroup{},
		&metav1.APIResourceList{},
	)
}

func registerConversions(scheme *runtime.Scheme) {
	scheme.AddConversionFunc((*v1alpha1.Solution)(nil), (*internal.Solution)(nil), func(a, b interface{}, scope conversion.Scope) error {
		return v1alpha1.Convert_v1alpha1_Solution_To_solution_Solution(a.(*v1alpha1.Solution), b.(*internal.Solution), scope)
	})
	scheme.AddConversionFunc((*internal.Solution)(nil), (*v1alpha1.Solution)(nil), func(a, b interface{}, scope conversion.Scope) error {
		return v1alpha1.Convert_solution_Solution_To_v1alpha1_Solution(a.(*internal.Solution), b.(*v1alpha1.Solution), scope)
	})
}

// New builds and returns a configured GenericAPIServer.
func New() (*genericapiserver.GenericAPIServer, error) {
	// Minimal server config — no etcd, no auth.
	recommendedConfig := genericapiserver.NewRecommendedConfig(Codecs)
	// Disable secure serving (plain HTTP on :8080).
	recommendedConfig.SecureServing = nil
	insecureOpts := options.NewInsecureServingOptions().WithLoopback()
	insecureOpts.BindPort = 8080
	if err := insecureOpts.ApplyTo(&recommendedConfig.Config.LoopbackClientConfig); err != nil {
		return nil, err
	}
	// Stub OpenAPI so the framework doesn't panic.
	recommendedConfig.Config.OpenAPIConfig = genericapiserver.DefaultOpenAPIConfig(
		func(ref openapi.ReferenceCallback) map[string]openapi.OpenAPIDefinition { return nil },
		openapi.NewDefinitionNamer(Scheme),
	)
	recommendedConfig.Config.OpenAPIConfig.Info.Title = "custom-api-server"
	recommendedConfig.Config.OpenAPIConfig.Info.Version = "0.1.0"

	genericServer, err := recommendedConfig.Complete().New("custom-api-server", genericapiserver.NewEmptyDelegate())
	if err != nil {
		return nil, err
	}

	store := registry.NewSolutionStorage()
	statusREST := registry.NewStatusREST(store)

	apiGroupInfo := genericapiserver.NewDefaultAPIGroupInfo(
		"solution.piotrmiskiewicz.github.com",
		Scheme,
		metav1.ParameterCodec,
		Codecs,
	)
	apiGroupInfo.VersionedResourcesStorageMap["v1alpha1"] = map[string]rest.Storage{
		"solutions":        store,
		"solutions/status": statusREST,
	}

	if err := genericServer.InstallAPIGroup(&apiGroupInfo); err != nil {
		return nil, err
	}
	return genericServer, nil
}
```

> **Full import block for server.go**:
> ```go
> import (
>     internal "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution"
>     "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution/v1alpha1"
>     registry "github.com/piotrmiskiewicz/custom-api-server/pkg/registry/solution"
>     "k8s.io/apimachinery/pkg/conversion"
>     metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
>     "k8s.io/apimachinery/pkg/runtime"
>     "k8s.io/apimachinery/pkg/runtime/schema"
>     "k8s.io/apimachinery/pkg/runtime/serializer"
>     "k8s.io/apimachinery/pkg/version"
>     "k8s.io/apiserver/pkg/registry/rest"
>     genericapiserver "k8s.io/apiserver/pkg/server"
>     "k8s.io/apiserver/pkg/server/options"
>     openapi "k8s.io/kube-openapi/pkg/common"
> )
> ```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/apiserver/...
```

Expected: `PASS`

- [ ] **Step 5: Commit**

```bash
git add pkg/apiserver/server.go pkg/apiserver/server_test.go
git commit -m "feat: add GenericAPIServer wiring in pkg/apiserver"
```

---

### Task 8: Entrypoint (`cmd/server/main.go`)

**Files:**
- Create: `cmd/server/main.go`

- [ ] **Step 1: Create `cmd/server/main.go`**

```go
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/piotrmiskiewicz/custom-api-server/pkg/apiserver"
)

func main() {
	srv, err := apiserver.New()
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}

	stopCh := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		close(stopCh)
	}()

	log.Println("Starting custom-api-server on :8080")
	if err := srv.PrepareRun().Run(stopCh); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
```

- [ ] **Step 2: Build to verify it compiles**

```bash
go build ./cmd/server
```

Expected: produces a `server` binary with no errors.

- [ ] **Step 3: Run smoke test**

```bash
./server &
sleep 1
curl -s http://localhost:8080/healthz
kill %1
```

Expected: `ok`

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat: add cmd/server/main.go entrypoint"
```

---

### Task 9: End-to-end smoke test

- [ ] **Step 1: Start the server**

```bash
go run ./cmd/server &
sleep 1
```

- [ ] **Step 2: List solutions (expect empty list)**

```bash
curl -s http://localhost:8080/apis/solution.piotrmiskiewicz.github.com/v1alpha1/namespaces/default/solutions
```

Expected:
```json
{"apiVersion":"solution.piotrmiskiewicz.github.com/v1alpha1","items":[],"kind":"SolutionList","metadata":{"resourceVersion":""}}
```

- [ ] **Step 3: Create a solution**

```bash
curl -s -X POST http://localhost:8080/apis/solution.piotrmiskiewicz.github.com/v1alpha1/namespaces/default/solutions \
  -H "Content-Type: application/json" \
  -d '{"apiVersion":"solution.piotrmiskiewicz.github.com/v1alpha1","kind":"Solution","metadata":{"name":"test-solution","namespace":"default"},"spec":{"solutionName":"my-app"}}'
```

Expected: JSON response with `"name":"test-solution"` and `"resourceVersion":"1"`.

- [ ] **Step 4: Get the solution**

```bash
curl -s http://localhost:8080/apis/solution.piotrmiskiewicz.github.com/v1alpha1/namespaces/default/solutions/test-solution
```

Expected: the created object.

- [ ] **Step 5: Update status**

```bash
curl -s -X PUT http://localhost:8080/apis/solution.piotrmiskiewicz.github.com/v1alpha1/namespaces/default/solutions/test-solution/status \
  -H "Content-Type: application/json" \
  -d '{"apiVersion":"solution.piotrmiskiewicz.github.com/v1alpha1","kind":"Solution","metadata":{"name":"test-solution","namespace":"default","resourceVersion":"1"},"spec":{"solutionName":"my-app"},"status":{"phase":"Running"}}'
```

Expected: response with `"phase":"Running"` and `"resourceVersion":"2"`.

- [ ] **Step 6: Delete the solution**

```bash
curl -s -X DELETE http://localhost:8080/apis/solution.piotrmiskiewicz.github.com/v1alpha1/namespaces/default/solutions/test-solution
```

Expected: `{"kind":"Status","apiVersion":"v1","status":"Success",...}`

- [ ] **Step 7: Stop server and commit**

```bash
kill %1
git add .
git commit -m "test: end-to-end smoke test verified"
```

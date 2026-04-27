package solution_test

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

func TestStorage_ListFieldSelector(t *testing.T) {
	store := registry.NewSolutionStorage()
	ctx := ctxWithNamespace("default")
	noopValidate := func(ctx context.Context, obj runtime.Object) error { return nil }

	for _, tc := range []struct{ name, solutionName string }{
		{"s1", "alpha"},
		{"s2", "beta"},
		{"s3", "alpha"},
	} {
		_, err := store.Create(ctx, &internal.Solution{
			ObjectMeta: metav1.ObjectMeta{Name: tc.name, Namespace: "default"},
			Spec:       internal.SolutionSpec{SolutionName: tc.solutionName},
		}, noopValidate, &metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Create %s: %v", tc.name, err)
		}
	}

	list, err := store.List(ctx, &metainternalversion.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("spec.solutionName", "alpha"),
	})
	if err != nil {
		t.Fatalf("List with field selector: %v", err)
	}
	items := list.(*internal.SolutionList).Items
	if len(items) != 2 {
		t.Errorf("expected 2 items with solutionName=alpha, got %d", len(items))
	}
	for _, item := range items {
		if item.Spec.SolutionName != "alpha" {
			t.Errorf("unexpected solutionName %q in result", item.Spec.SolutionName)
		}
	}
}

func TestStorage_ListFieldSelectorUnknownField(t *testing.T) {
	store := registry.NewSolutionStorage()
	ctx := ctxWithNamespace("default")

	_, err := store.List(ctx, &metainternalversion.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("spec.unknown", "x"),
	})
	if err == nil {
		t.Fatal("expected error for unknown field selector, got nil")
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

	select {
	case ev := <-wA.ResultChan():
		if ev.Type != watch.Added {
			t.Errorf("wA: expected Added, got %v", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("wA: timed out waiting for event")
	}

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

	store.Create(ctx, &internal.Solution{
		ObjectMeta: metav1.ObjectMeta{Name: "match", Namespace: "default"},
		Spec:       internal.SolutionSpec{SolutionName: "alpha"},
	}, func(_ context.Context, _ runtime.Object) error { return nil }, &metav1.CreateOptions{})

	store.Create(ctx, &internal.Solution{
		ObjectMeta: metav1.ObjectMeta{Name: "nomatch", Namespace: "default"},
		Spec:       internal.SolutionSpec{SolutionName: "beta"},
	}, func(_ context.Context, _ runtime.Object) error { return nil }, &metav1.CreateOptions{})

	select {
	case ev := <-w.ResultChan():
		sol := ev.Object.(*internal.Solution)
		if sol.Name != "match" {
			t.Errorf("expected name 'match', got %q", sol.Name)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for watch event")
	}

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

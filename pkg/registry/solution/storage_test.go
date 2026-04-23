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

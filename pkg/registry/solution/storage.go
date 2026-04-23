package solution

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

var solutionGR = schema.GroupResource{Group: "solution.piotrmiskiewicz.github.com", Resource: "solutions"}

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

// --- rest.SingularNameProvider ---

func (s *SolutionStorage) GetSingularName() string { return "solution" }

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
		// key format is "namespace/name"; match prefix "namespace/"
		if ns == "" || len(k) > len(ns) && k[:len(ns)+1] == ns+"/" {
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
	sol.CreationTimestamp = metav1.NewTime(time.Now())
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

func (r *StatusREST) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo, _ rest.ValidateObjectFunc, _ rest.ValidateObjectUpdateFunc, _ bool, _ *metav1.UpdateOptions) (runtime.Object, bool, error) {
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

package solution

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	internal "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution"
	"k8s.io/apimachinery/pkg/api/errors"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
)

var solutionGR = schema.GroupResource{Group: "solution.piotrmiskiewicz.github.com", Resource: "solutions"}

// Storage is the interface both InMemoryStorage and PostgresStorage implement.
type Storage interface {
	rest.StandardStorage
	rest.SingularNameProvider
}

// StatusUpdater is an optional interface for storage backends that can update
// only the status subresource atomically, preserving spec.
type StatusUpdater interface {
	UpdateStatus(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo) (runtime.Object, error)
}

// GetAttrs returns the label set, field set, and error for a Solution object.
// Supported field selectors: metadata.name, metadata.namespace, spec.solutionName.
func GetAttrs(obj runtime.Object) (labels.Set, fields.Set, error) {
	sol, ok := obj.(*internal.Solution)
	if !ok {
		return nil, nil, fmt.Errorf("not a Solution")
	}
	return labels.Set(sol.Labels), fields.Set{
		"metadata.name":      sol.Name,
		"metadata.namespace": sol.Namespace,
		"spec.solutionName":  sol.Spec.SolutionName,
	}, nil
}

// InMemoryStorage is an in-memory REST storage for Solution objects.
type InMemoryStorage struct {
	mu      sync.RWMutex
	objects map[string]*internal.Solution // key: namespace/name
}

// NewInMemoryStorage creates an empty InMemoryStorage.
func NewInMemoryStorage() *InMemoryStorage {
	return &InMemoryStorage{objects: make(map[string]*internal.Solution)}
}

// NewSolutionStorage is an alias for NewInMemoryStorage for backwards compatibility.
func NewSolutionStorage() *InMemoryStorage {
	return NewInMemoryStorage()
}

func key(ns, name string) string { return ns + "/" + name }

// --- rest.Scoper ---

func (s *InMemoryStorage) NamespaceScoped() bool { return true }

// --- rest.SingularNameProvider ---

func (s *InMemoryStorage) GetSingularName() string { return "solution" }

// --- rest.StandardStorage ---

func (s *InMemoryStorage) New() runtime.Object { return &internal.Solution{} }

func (s *InMemoryStorage) NewList() runtime.Object { return &internal.SolutionList{} }

func (s *InMemoryStorage) Destroy() {}

func (s *InMemoryStorage) Get(ctx context.Context, name string, _ *metav1.GetOptions) (runtime.Object, error) {
	ns, _ := request.NamespaceFrom(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	obj, ok := s.objects[key(ns, name)]
	if !ok {
		return nil, errors.NewNotFound(solutionGR, name)
	}
	return obj.DeepCopyObject(), nil
}

func (s *InMemoryStorage) List(ctx context.Context, opts *metainternalversion.ListOptions) (runtime.Object, error) {
	ns, _ := request.NamespaceFrom(ctx)

	var fieldSel fields.Selector
	if opts != nil && opts.FieldSelector != nil {
		if err := validateFieldSelector(opts.FieldSelector); err != nil {
			return nil, errors.NewBadRequest(err.Error())
		}
		fieldSel = opts.FieldSelector
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	list := &internal.SolutionList{}
	for k, v := range s.objects {
		// key format is "namespace/name"; match prefix "namespace/"
		if ns != "" && !(len(k) > len(ns) && k[:len(ns)+1] == ns+"/") {
			continue
		}
		if fieldSel != nil && !fieldSel.Empty() {
			_, fieldSet, _ := GetAttrs(v)
			if !fieldSel.Matches(fieldSet) {
				continue
			}
		}
		list.Items = append(list.Items, *v.DeepCopyObject().(*internal.Solution))
	}
	return list, nil
}

// validateFieldSelector rejects field names that are not supported.
func validateFieldSelector(sel fields.Selector) error {
	for _, req := range sel.Requirements() {
		switch req.Field {
		case "metadata.name", "metadata.namespace", "spec.solutionName":
			// supported
		default:
			return fmt.Errorf("field selector %q is not supported; supported fields: metadata.name, metadata.namespace, spec.solutionName", req.Field)
		}
	}
	return nil
}

func (s *InMemoryStorage) Create(ctx context.Context, obj runtime.Object, createValidation rest.ValidateObjectFunc, _ *metav1.CreateOptions) (runtime.Object, error) {
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

func (s *InMemoryStorage) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo, createValidation rest.ValidateObjectFunc, updateValidation rest.ValidateObjectUpdateFunc, _ bool, _ *metav1.UpdateOptions) (runtime.Object, bool, error) {
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

func (s *InMemoryStorage) Delete(ctx context.Context, name string, deleteValidation rest.ValidateObjectFunc, _ *metav1.DeleteOptions) (runtime.Object, bool, error) {
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

func (s *InMemoryStorage) Watch(_ context.Context, _ *metainternalversion.ListOptions) (watch.Interface, error) {
	return nil, errors.NewMethodNotSupported(solutionGR, "watch")
}

func (s *InMemoryStorage) DeleteCollection(ctx context.Context, deleteValidation rest.ValidateObjectFunc, _ *metav1.DeleteOptions, listOpts *metainternalversion.ListOptions) (runtime.Object, error) {
	listed, err := s.List(ctx, listOpts)
	if err != nil {
		return nil, err
	}
	sl := listed.(*internal.SolutionList)
	ns, _ := request.NamespaceFrom(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range sl.Items {
		obj := &sl.Items[i]
		if err := deleteValidation(ctx, obj); err != nil {
			return nil, err
		}
		delete(s.objects, key(ns, obj.Name))
	}
	return sl, nil
}

func (s *InMemoryStorage) ConvertToTable(ctx context.Context, obj runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	return rest.NewDefaultTableConvertor(solutionGR).ConvertToTable(ctx, obj, tableOptions)
}

// UpdateStatus updates only the status field, preserving spec and metadata.
// It implements the StatusUpdater interface.
func (s *InMemoryStorage) UpdateStatus(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo) (runtime.Object, error) {
	ns, _ := request.NamespaceFrom(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(ns, name)
	existing, ok := s.objects[k]
	if !ok {
		return nil, errors.NewNotFound(solutionGR, name)
	}
	updated, err := objInfo.UpdatedObject(ctx, existing)
	if err != nil {
		return nil, err
	}
	updatedSol := updated.(*internal.Solution)
	// Only copy status — spec and metadata remain from existing.
	existing.Status = updatedSol.Status
	rv, _ := strconv.Atoi(existing.ResourceVersion)
	existing.ResourceVersion = strconv.Itoa(rv + 1)
	cp := existing.DeepCopyObject().(*internal.Solution)
	s.objects[k] = cp
	return cp.DeepCopyObject(), nil
}

// --- StatusREST ---

// StatusREST handles updates to the /status subresource.
type StatusREST struct {
	store Storage
}

// NewStatusREST creates a StatusREST that shares the given Storage.
func NewStatusREST(store Storage) *StatusREST {
	return &StatusREST{store: store}
}

func (r *StatusREST) New() runtime.Object { return &internal.Solution{} }

func (r *StatusREST) Destroy() {}

func (r *StatusREST) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo, _ rest.ValidateObjectFunc, _ rest.ValidateObjectUpdateFunc, _ bool, _ *metav1.UpdateOptions) (runtime.Object, bool, error) {
	if su, ok := r.store.(StatusUpdater); ok {
		obj, err := su.UpdateStatus(ctx, name, objInfo)
		return obj, false, err
	}
	// Fallback: use generic Update (spec changes won't be filtered).
	return r.store.Update(ctx, name, objInfo, func(_ context.Context, _ runtime.Object) error { return nil }, func(_ context.Context, _, _ runtime.Object) error { return nil }, false, &metav1.UpdateOptions{})
}

// UpdateFunc adapts a plain function to rest.UpdatedObjectInfo for use in tests.
type UpdateFunc func(ctx context.Context, obj runtime.Object, creating bool) (runtime.Object, bool, error)

func (f UpdateFunc) Preconditions() *metav1.Preconditions { return nil }

func (f UpdateFunc) UpdatedObject(ctx context.Context, oldObj runtime.Object) (runtime.Object, error) {
	obj, _, err := f(ctx, oldObj.DeepCopyObject(), false)
	return obj, err
}

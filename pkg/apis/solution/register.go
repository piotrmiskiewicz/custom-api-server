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

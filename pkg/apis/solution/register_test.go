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

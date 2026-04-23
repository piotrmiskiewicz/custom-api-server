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

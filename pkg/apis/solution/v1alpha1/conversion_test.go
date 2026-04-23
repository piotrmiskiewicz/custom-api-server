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
	var scope conversion.Scope
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
	var scope conversion.Scope
	if err := v1alpha1.Convert_solution_Solution_To_v1alpha1_Solution(src, dst, scope); err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	if dst.Status.Phase != v1alpha1.PhaseFailed {
		t.Errorf("Phase: got %q, want %q", dst.Status.Phase, v1alpha1.PhaseFailed)
	}
}

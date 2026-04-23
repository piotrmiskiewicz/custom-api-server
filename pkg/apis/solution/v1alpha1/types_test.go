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
	cp := orig.DeepCopyObject()
	if cp == orig {
		t.Fatal("DeepCopyObject returned same pointer")
	}
	s, ok := cp.(*v1alpha1.Solution)
	if !ok {
		t.Fatalf("expected *Solution, got %T", cp)
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
	cp := orig.DeepCopyObject()
	if cp == orig {
		t.Fatal("DeepCopyObject returned same pointer")
	}
	sl, ok := cp.(*v1alpha1.SolutionList)
	if !ok {
		t.Fatalf("expected *SolutionList, got %T", cp)
	}
	if len(sl.Items) != 1 {
		t.Errorf("expected 1 item, got %d", len(sl.Items))
	}
}

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

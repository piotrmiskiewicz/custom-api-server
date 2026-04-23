package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type Solution struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SolutionSpec   `json:"spec"`
	Status SolutionStatus `json:"status,omitempty"`
}

type SolutionSpec struct {
	// +kubebuilder:validation:Required
	SolutionName string `json:"solutionName"`
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
	// +kubebuilder:default=Pending
	Phase Phase `json:"phase,omitempty"`

	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
type SolutionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Solution `json:"items"`
}

func (in *Solution) DeepCopyObject() runtime.Object {
	out := new(Solution)
	*out = *in
	out.Status.Conditions = append([]metav1.Condition(nil), in.Status.Conditions...)
	return out
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

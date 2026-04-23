package v1alpha1

import (
	internal "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution"
	"k8s.io/apimachinery/pkg/conversion"
)

// Convert_v1alpha1_Solution_To_solution_Solution converts versioned → internal.
func Convert_v1alpha1_Solution_To_solution_Solution(in *Solution, out *internal.Solution, _ conversion.Scope) error {
	out.ObjectMeta = in.ObjectMeta
	out.TypeMeta = in.TypeMeta
	out.Spec.SolutionName = in.Spec.SolutionName
	out.Status.Phase = internal.Phase(in.Status.Phase)
	out.Status.Conditions = append(out.Status.Conditions[:0], in.Status.Conditions...)
	return nil
}

// Convert_solution_Solution_To_v1alpha1_Solution converts internal → versioned.
func Convert_solution_Solution_To_v1alpha1_Solution(in *internal.Solution, out *Solution, _ conversion.Scope) error {
	out.ObjectMeta = in.ObjectMeta
	out.TypeMeta = in.TypeMeta
	out.Spec.SolutionName = in.Spec.SolutionName
	out.Status.Phase = Phase(in.Status.Phase)
	out.Status.Conditions = append(out.Status.Conditions[:0], in.Status.Conditions...)
	return nil
}

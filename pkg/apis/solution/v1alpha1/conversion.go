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

// Convert_v1alpha1_SolutionList_To_solution_SolutionList converts versioned list → internal list.
func Convert_v1alpha1_SolutionList_To_solution_SolutionList(in *SolutionList, out *internal.SolutionList, scope conversion.Scope) error {
	out.ListMeta = in.ListMeta
	out.Items = make([]internal.Solution, len(in.Items))
	for i := range in.Items {
		if err := Convert_v1alpha1_Solution_To_solution_Solution(&in.Items[i], &out.Items[i], scope); err != nil {
			return err
		}
	}
	return nil
}

// Convert_solution_SolutionList_To_v1alpha1_SolutionList converts internal list → versioned list.
func Convert_solution_SolutionList_To_v1alpha1_SolutionList(in *internal.SolutionList, out *SolutionList, scope conversion.Scope) error {
	out.ListMeta = in.ListMeta
	out.Items = make([]Solution, len(in.Items))
	for i := range in.Items {
		if err := Convert_solution_Solution_To_v1alpha1_Solution(&in.Items[i], &out.Items[i], scope); err != nil {
			return err
		}
	}
	return nil
}

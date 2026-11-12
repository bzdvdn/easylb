package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=vippool
// +kubebuilder:printcolumn:name="Addresses",type=string,JSONPath=`.spec.addresses`
// +kubebuilder:printcolumn:name="AutoAssign",type=boolean,JSONPath=`.spec.autoAssign`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// VIPPool is a cluster-scoped pool of external VIP addresses.
type VIPPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec VIPPoolSpec `json:"spec,omitempty"`
}

// VIPPoolSpec defines the addresses available for automatic allocation.
type VIPPoolSpec struct {
	// Addresses is a list of IPv4/IPv6 addresses, CIDRs, or ranges (a-b).
	// +kubebuilder:validation:MinItems=1
	Addresses []string `json:"addresses"`

	// AutoAssign controls automatic allocation from this pool. Defaults to true.
	// +optional
	AutoAssign *bool `json:"autoAssign,omitempty"`
}

// AutoAssignEnabled reports whether automatic allocation is enabled.
func (in *VIPPool) AutoAssignEnabled() bool {
	return in.Spec.AutoAssign == nil || *in.Spec.AutoAssign
}

// +kubebuilder:object:root=true

// VIPPoolList contains a list of VIPPool.
type VIPPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []VIPPool `json:"items"`
}

// DeepCopyInto copies the receiver into out.
func (in *VIPPool) DeepCopyInto(out *VIPPool) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
}

// DeepCopy returns a deep copy of the receiver.
func (in *VIPPool) DeepCopy() *VIPPool {
	if in == nil {
		return nil
	}
	out := new(VIPPool)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject returns a deep copy as a runtime.Object.
func (in *VIPPool) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	return in.DeepCopy()
}

// DeepCopyInto copies the receiver into out.
func (in *VIPPoolSpec) DeepCopyInto(out *VIPPoolSpec) {
	*out = *in
	if in.Addresses != nil {
		out.Addresses = make([]string, len(in.Addresses))
		copy(out.Addresses, in.Addresses)
	}
	if in.AutoAssign != nil {
		out.AutoAssign = new(bool)
		*out.AutoAssign = *in.AutoAssign
	}
}

// DeepCopyInto copies the receiver into out.
func (in *VIPPoolList) DeepCopyInto(out *VIPPoolList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]VIPPool, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopy returns a deep copy of the receiver.
func (in *VIPPoolList) DeepCopy() *VIPPoolList {
	if in == nil {
		return nil
	}
	out := new(VIPPoolList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject returns a deep copy as a runtime.Object.
func (in *VIPPoolList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	return in.DeepCopy()
}

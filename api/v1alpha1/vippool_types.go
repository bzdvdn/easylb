package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=vippool
// +kubebuilder:printcolumn:name="Addresses",type=string,JSONPath=`.spec.addresses`
// +kubebuilder:printcolumn:name="AutoAssign",type=boolean,JSONPath=`.spec.autoAssign`
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.spec.mode`
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

	// AvoidBuggyIPs skips IPv4 addresses ending in .0 or .255.
	// +optional
	AvoidBuggyIPs bool `json:"avoidBuggyIPs,omitempty"`

	// Mode is the default ownership mode for bindings created automatically
	// from this pool. Defaults to l2.
	// +kubebuilder:validation:Enum=external;l2;bgp
	// +optional
	Mode VIPMode `json:"mode,omitempty"`

	// ServiceAllocation restricts which Services are auto-assigned from this
	// pool. When unset, the pool is available to all Services. An empty
	// selector matches everything.
	// +optional
	ServiceAllocation *ServiceAllocation `json:"serviceAllocation,omitempty"`
}

// ServiceAllocation selects Services that may receive addresses from a pool.
type ServiceAllocation struct {
	// NamespaceSelector selects namespaces by labels.
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`

	// ServiceSelector selects Services by labels.
	// +optional
	ServiceSelector *metav1.LabelSelector `json:"serviceSelector,omitempty"`
}

// AutoAssignEnabled reports whether automatic allocation is enabled.
func (in *VIPPool) AutoAssignEnabled() bool {
	return in.Spec.AutoAssign == nil || *in.Spec.AutoAssign
}

// ModeOrDefault returns the pool ownership mode, defaulting to l2.
func (in *VIPPool) ModeOrDefault() VIPMode {
	if in.Spec.Mode == "" {
		return VIPModeL2
	}
	return in.Spec.Mode
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
	if in.ServiceAllocation != nil {
		out.ServiceAllocation = in.ServiceAllocation.DeepCopy()
	}
}

// DeepCopy returns a deep copy of the receiver.
func (in *ServiceAllocation) DeepCopy() *ServiceAllocation {
	if in == nil {
		return nil
	}
	out := new(ServiceAllocation)
	*out = *in
	if in.NamespaceSelector != nil {
		out.NamespaceSelector = in.NamespaceSelector.DeepCopy()
	}
	if in.ServiceSelector != nil {
		out.ServiceSelector = in.ServiceSelector.DeepCopy()
	}
	return out
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

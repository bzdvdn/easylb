package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// VIPMode describes who owns a VIP.
type VIPMode string

const (
	// VIPModeExternal means the VIP is managed outside easyLB, for example by
	// keepalived configured by a cluster administrator.
	VIPModeExternal VIPMode = "external"
	// VIPModeL2 means the easyLB speaker owns the VIP and advertises it via
	// ARP/NDP.
	VIPModeL2 VIPMode = "l2"
	// VIPModeBGP is reserved for a future easyLB BGP speaker.
	VIPModeBGP VIPMode = "bgp"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=vipb
// +kubebuilder:printcolumn:name="Service",type=string,JSONPath=`.spec.serviceRef.name`
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.spec.mode`
// +kubebuilder:printcolumn:name="VIPs",type=string,JSONPath=`.status.allocatedIPs`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// VIPBinding binds VIPs to a LoadBalancer Service.
type VIPBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VIPBindingSpec   `json:"spec,omitempty"`
	Status VIPBindingStatus `json:"status,omitempty"`
}

// VIPBindingSpec defines the desired VIPs for a Service.
type VIPBindingSpec struct {
	// ServiceRef references a Service in the same namespace.
	ServiceRef ServiceReference `json:"serviceRef"`

	// Mode is the VIP ownership mode. Defaults to "external".
	// +kubebuilder:validation:Enum=external;l2;bgp
	// +optional
	Mode VIPMode `json:"mode,omitempty"`

	// VIPs is an explicit list of VIPs. Mutually exclusive with PoolRef.
	// +optional
	VIPs []string `json:"vips,omitempty"`

	// PoolRef requests an address from a VIPPool. Mutually exclusive with VIPs.
	// +optional
	PoolRef *PoolReference `json:"poolRef,omitempty"`

	// RequestedIP requests a specific address from the pool.
	// +optional
	RequestedIP string `json:"requestedIP,omitempty"`
}

// ServiceReference references a Service in the same namespace.
type ServiceReference struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// PoolReference references a cluster-scoped VIPPool.
type PoolReference struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// VIPBindingStatus is the observed state of a VIPBinding.
type VIPBindingStatus struct {
	// AllocatedIPs is the list of VIPs published for the Service.
	// +optional
	AllocatedIPs []string `json:"allocatedIPs,omitempty"`

	// ObservedGeneration is the last generation reconciled by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions contains the current state of the binding.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true

// VIPBindingList contains a list of VIPBinding.
type VIPBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []VIPBinding `json:"items"`
}

// DeepCopyInto copies the receiver into out.
func (in *VIPBinding) DeepCopyInto(out *VIPBinding) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy returns a deep copy of the receiver.
func (in *VIPBinding) DeepCopy() *VIPBinding {
	if in == nil {
		return nil
	}
	out := new(VIPBinding)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject returns a deep copy as a runtime.Object.
func (in *VIPBinding) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	return in.DeepCopy()
}

// DeepCopyInto copies the receiver into out.
func (in *VIPBindingSpec) DeepCopyInto(out *VIPBindingSpec) {
	*out = *in
	out.ServiceRef = in.ServiceRef
	if in.VIPs != nil {
		out.VIPs = make([]string, len(in.VIPs))
		copy(out.VIPs, in.VIPs)
	}
	if in.PoolRef != nil {
		out.PoolRef = new(PoolReference)
		*out.PoolRef = *in.PoolRef
	}
}

// DeepCopyInto copies the receiver into out.
func (in *VIPBindingStatus) DeepCopyInto(out *VIPBindingStatus) {
	*out = *in
	if in.AllocatedIPs != nil {
		out.AllocatedIPs = make([]string, len(in.AllocatedIPs))
		copy(out.AllocatedIPs, in.AllocatedIPs)
	}
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		for i := range in.Conditions {
			in.Conditions[i].DeepCopyInto(&out.Conditions[i])
		}
	}
}

// DeepCopyInto copies the receiver into out.
func (in *VIPBindingList) DeepCopyInto(out *VIPBindingList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]VIPBinding, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopy returns a deep copy of the receiver.
func (in *VIPBindingList) DeepCopy() *VIPBindingList {
	if in == nil {
		return nil
	}
	out := new(VIPBindingList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject returns a deep copy as a runtime.Object.
func (in *VIPBindingList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	return in.DeepCopy()
}

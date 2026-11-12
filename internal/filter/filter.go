// Package filter contains the shared Service selection logic used by the
// controller and the speaker.
package filter

import (
	corev1 "k8s.io/api/core/v1"
)

// EnabledAnnotation can be set to "false" to opt a Service out.
const EnabledAnnotation = "easylb.io/enabled"

// Options controls Service selection.
type Options struct {
	LBClass      string
	RequireClass bool
}

// Manages reports whether svc belongs to easyLB.
func Manages(svc *corev1.Service, opts Options) bool {
	if svc == nil || svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		return false
	}
	if svc.Annotations[EnabledAnnotation] == "false" {
		return false
	}
	if svc.Spec.LoadBalancerClass != nil {
		return *svc.Spec.LoadBalancerClass == opts.LBClass
	}
	return !opts.RequireClass
}

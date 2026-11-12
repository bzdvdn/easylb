package speaker

import (
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
)

// Eligible reports whether nodeName may advertise the Service VIP.
//
// A node is eligible when it is Ready, the Service has at least one Ready
// endpoint, and - for externalTrafficPolicy=Local - the Ready endpoint is local
// to nodeName.
func Eligible(nodeName string, svc *corev1.Service, node *corev1.Node, slices []*discoveryv1.EndpointSlice) bool {
	if node == nil || !nodeReady(node) {
		return false
	}

	localReady := false
	anyReady := false
	for _, slice := range slices {
		if slice == nil {
			continue
		}
		for _, endpoint := range slice.Endpoints {
			if endpoint.Conditions.Ready == nil || !*endpoint.Conditions.Ready {
				continue
			}
			anyReady = true
			if endpoint.NodeName != nil && *endpoint.NodeName == nodeName {
				localReady = true
			}
		}
	}
	if !anyReady {
		return false
	}
	if svc.Spec.ExternalTrafficPolicy == corev1.ServiceExternalTrafficPolicyTypeLocal {
		return localReady
	}
	return true
}

func nodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

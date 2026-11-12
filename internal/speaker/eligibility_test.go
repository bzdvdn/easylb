package speaker

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestEligibleClusterPolicy(t *testing.T) {
	svc := &corev1.Service{Spec: corev1.ServiceSpec{
		ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
	}}
	node := speakerNode("master", true)
	slices := make([]*discoveryv1.EndpointSlice, 0, 2)
	slices = append(slices, speakerSlice(endpointOnNode("worker1", true)))

	if !Eligible("master", svc, node, slices) {
		t.Fatal("expected cluster-policy node to be eligible")
	}
}

func TestEligibleLocalPolicyRequiresLocalEndpoint(t *testing.T) {
	svc := &corev1.Service{Spec: corev1.ServiceSpec{
		ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
	}}
	node := speakerNode("master", true)
	slices := make([]*discoveryv1.EndpointSlice, 0, 2)
	slices = append(slices, speakerSlice(endpointOnNode("worker1", true)))

	if Eligible("master", svc, node, slices) {
		t.Fatal("expected node without a local endpoint to be ineligible")
	}

	slices = append(slices, speakerSlice(endpointOnNode("master", true)))
	if !Eligible("master", svc, node, slices) {
		t.Fatal("expected node with a local endpoint to be eligible")
	}
}

func TestEligibleRequiresReadyEndpointAndReadyNode(t *testing.T) {
	svc := &corev1.Service{Spec: corev1.ServiceSpec{
		ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
	}}

	if Eligible("master", svc, speakerNode("master", true), []*discoveryv1.EndpointSlice{
		speakerSlice(endpointOnNode("worker1", false)),
	}) {
		t.Fatal("expected no eligible nodes without a ready endpoint")
	}

	if Eligible("master", svc, speakerNode("master", false), []*discoveryv1.EndpointSlice{
		speakerSlice(endpointOnNode("worker1", true)),
	}) {
		t.Fatal("expected a not-ready node to be ineligible")
	}
}

func speakerNode(name string, ready bool) *corev1.Node {
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{
				Type:   corev1.NodeReady,
				Status: status,
			}},
		},
	}
}

func speakerSlice(endpoints ...discoveryv1.Endpoint) *discoveryv1.EndpointSlice {
	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-abc"},
		Endpoints:  endpoints,
	}
}

func endpointOnNode(name string, ready bool) discoveryv1.Endpoint {
	return discoveryv1.Endpoint{
		NodeName:   ptr.To(name),
		Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(ready)},
	}
}

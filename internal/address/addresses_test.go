package address

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestDesiredClusterPublishesReadyNodeIPs(t *testing.T) {
	svc := &corev1.Service{Spec: corev1.ServiceSpec{
		IPFamilies:            []corev1.IPFamily{corev1.IPv4Protocol},
		ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
	}}
	nodes := []*corev1.Node{
		readyNode("node-1",
			corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
		),
		readyNode("node-2",
			corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "10.0.0.2"},
			corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: "1.2.3.4"},
		),
		notReadyNode("node-3",
			corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "10.0.0.3"},
		),
	}

	got, invalid := Desired(svc, nodes, nil, "", Options{
		NodeIPTypes:    []corev1.NodeAddressType{corev1.NodeInternalIP},
		PublishNodeIPs: true,
	})
	want := []string{"10.0.0.1", "10.0.0.2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected addresses: got %v, want %v", got, want)
	}
	if len(invalid) != 0 {
		t.Fatalf("unexpected invalid VIPs: %v", invalid)
	}
}

func TestDesiredLocalOnlyPublishesNodesWithReadyEndpoints(t *testing.T) {
	svc := &corev1.Service{Spec: corev1.ServiceSpec{
		IPFamilies:            []corev1.IPFamily{corev1.IPv4Protocol},
		ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
	}}
	nodes := []*corev1.Node{
		readyNode("node-1", corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "10.0.0.1"}),
		readyNode("node-2", corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "10.0.0.2"}),
		readyNode("node-3", corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "10.0.0.3"}),
	}
	slices := []*discoveryv1.EndpointSlice{
		endpointSlice("ingress",
			endpoint("node-1", true),
			endpoint("node-2", false),
		),
	}

	got, _ := Desired(svc, nodes, slices, "", Options{
		NodeIPTypes:    []corev1.NodeAddressType{corev1.NodeInternalIP},
		PublishNodeIPs: true,
	})
	want := []string{"10.0.0.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected addresses: got %v, want %v", got, want)
	}
}

func TestDesiredIncludesVIPsAndReportsInvalidValues(t *testing.T) {
	svc := &corev1.Service{Spec: corev1.ServiceSpec{
		IPFamilies:            []corev1.IPFamily{corev1.IPv4Protocol},
		ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
	}}
	nodes := []*corev1.Node{
		readyNode("node-1", corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "10.0.0.1"}),
	}

	got, invalid := Desired(svc, nodes, nil, " 10.99.50.100 , not-an-ip, 2001:db8::1 ", Options{
		NodeIPTypes:    []corev1.NodeAddressType{corev1.NodeInternalIP},
		PublishNodeIPs: true,
	})
	want := []string{"10.0.0.1", "10.99.50.100"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected addresses: got %v, want %v", got, want)
	}
	wantInvalid := []string{"not-an-ip"}
	if !reflect.DeepEqual(invalid, wantInvalid) {
		t.Fatalf("unexpected invalid VIPs: got %v, want %v", invalid, wantInvalid)
	}
}

func TestDesiredDeduplicatesVIPEqualToNodeIP(t *testing.T) {
	svc := &corev1.Service{Spec: corev1.ServiceSpec{
		IPFamilies:            []corev1.IPFamily{corev1.IPv4Protocol},
		ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
	}}
	nodes := []*corev1.Node{
		readyNode("node-1", corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "10.0.0.1"}),
	}

	got, _ := Desired(svc, nodes, nil, "10.0.0.1", Options{
		NodeIPTypes:    []corev1.NodeAddressType{corev1.NodeInternalIP},
		PublishNodeIPs: true,
	})
	want := []string{"10.0.0.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected addresses: got %v, want %v", got, want)
	}
}

func TestDesiredFiltersByServiceIPFamily(t *testing.T) {
	svc := &corev1.Service{Spec: corev1.ServiceSpec{
		IPFamilies:            []corev1.IPFamily{corev1.IPv6Protocol},
		ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
	}}
	nodes := []*corev1.Node{
		readyNode("node-1",
			corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
			corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "2001:db8::10"},
		),
	}

	got, _ := Desired(svc, nodes, nil, "10.99.50.100,2001:db8::100", Options{
		NodeIPTypes:    []corev1.NodeAddressType{corev1.NodeInternalIP},
		PublishNodeIPs: true,
	})
	want := []string{"2001:db8::10", "2001:db8::100"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected addresses: got %v, want %v", got, want)
	}
}

func TestDesiredCanDisableNodeIPs(t *testing.T) {
	svc := &corev1.Service{Spec: corev1.ServiceSpec{
		IPFamilies:            []corev1.IPFamily{corev1.IPv4Protocol},
		ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
	}}
	nodes := []*corev1.Node{
		readyNode("node-1", corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "10.0.0.1"}),
	}

	got, _ := Desired(svc, nodes, nil, "10.99.50.100", Options{
		NodeIPTypes:    []corev1.NodeAddressType{corev1.NodeInternalIP},
		PublishNodeIPs: false,
	})
	want := []string{"10.99.50.100"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected addresses: got %v, want %v", got, want)
	}
}

func readyNode(name string, addrs ...corev1.NodeAddress) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
			}},
			Addresses: addrs,
		},
	}
}

func notReadyNode(name string, addrs ...corev1.NodeAddress) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionFalse,
			}},
			Addresses: addrs,
		},
	}
}

func endpointSlice(serviceName string, endpoints ...discoveryv1.Endpoint) *discoveryv1.EndpointSlice {
	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName + "-slice",
			Namespace: "default",
			Labels:    map[string]string{discoveryv1.LabelServiceName: serviceName},
		},
		Endpoints: endpoints,
	}
}

func endpoint(node string, ready bool) discoveryv1.Endpoint {
	return discoveryv1.Endpoint{
		NodeName:   ptr.To(node),
		Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(ready)},
	}
}

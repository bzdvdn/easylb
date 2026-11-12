package controller

import (
	"context"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	easylv1alpha1 "github.com/bzdvdn/easylb/api/v1alpha1"
	"github.com/bzdvdn/easylb/internal/address"
	"github.com/bzdvdn/easylb/internal/vip"
)

const testClass = "easylb.io/easylb"

func TestReconcilePublishesNodeIPsAndVIPs(t *testing.T) {
	ctx := context.Background()
	svc := testService("ingress", corev1.ServiceExternalTrafficPolicyTypeLocal)
	objects := []client.Object{
		testNode("node-1", true, "10.0.0.1"),
		testNode("node-2", true, "10.0.0.2"),
		svc,
		testVIPBinding("vip", "ingress", easylv1alpha1.VIPModeExternal, "10.99.50.100"),
		testSlice("ingress", testEndpoint("node-1", true), testEndpoint("node-2", false)),
	}
	r, c := newReconciler(t, objects...)

	key := types.NamespacedName{Namespace: "default", Name: "ingress"}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	got := getService(t, c, key)
	want := []corev1.LoadBalancerIngress{{IP: "10.0.0.1"}, {IP: "10.99.50.100"}}
	if !reflect.DeepEqual(got.Status.LoadBalancer.Ingress, want) {
		t.Fatalf("unexpected ingress: got %#v, want %#v", got.Status.LoadBalancer.Ingress, want)
	}
}

func TestReconcilePublishesConfiguredVIPModes(t *testing.T) {
	ctx := context.Background()
	svc := testService("ingress", corev1.ServiceExternalTrafficPolicyTypeCluster)
	objects := []client.Object{
		testNode("node-1", true, "10.0.0.1"),
		svc,
		testVIPBinding("external", "ingress", easylv1alpha1.VIPModeExternal, "10.99.50.100"),
		testVIPBinding("l2", "ingress", easylv1alpha1.VIPModeL2, "10.99.50.101"),
	}
	r, c := newReconciler(t, objects...)

	key := types.NamespacedName{Namespace: "default", Name: "ingress"}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	got := getService(t, c, key)
	want := []corev1.LoadBalancerIngress{
		{IP: "10.0.0.1"},
		{IP: "10.99.50.100"},
		{IP: "10.99.50.101"},
	}
	if !reflect.DeepEqual(got.Status.LoadBalancer.Ingress, want) {
		t.Fatalf("unexpected ingress: got %#v, want %#v", got.Status.LoadBalancer.Ingress, want)
	}
}

func TestReconcileClusterPolicyPublishesAllReadyNodes(t *testing.T) {
	ctx := context.Background()
	svc := testService("ingress", corev1.ServiceExternalTrafficPolicyTypeCluster)
	objects := []client.Object{
		testNode("node-1", true, "10.0.0.1"),
		testNode("node-2", true, "10.0.0.2"),
		testNode("node-3", false, "10.0.0.3"),
		svc,
	}
	r, c := newReconciler(t, objects...)

	key := types.NamespacedName{Namespace: "default", Name: "ingress"}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	got := getService(t, c, key)
	want := []corev1.LoadBalancerIngress{{IP: "10.0.0.1"}, {IP: "10.0.0.2"}}
	if !reflect.DeepEqual(got.Status.LoadBalancer.Ingress, want) {
		t.Fatalf("unexpected ingress: got %#v, want %#v", got.Status.LoadBalancer.Ingress, want)
	}
}

func TestReconcileIsIdempotent(t *testing.T) {
	ctx := context.Background()
	svc := testService("ingress", corev1.ServiceExternalTrafficPolicyTypeLocal)
	objects := []client.Object{
		testNode("node-1", true, "10.0.0.1"),
		svc,
		testVIPBinding("vip", "ingress", easylv1alpha1.VIPModeExternal, "10.99.50.100"),
		testSlice("ingress", testEndpoint("node-1", true)),
	}
	r, c := newReconciler(t, objects...)
	key := types.NamespacedName{Namespace: "default", Name: "ingress"}

	for i := 0; i < 2; i++ {
		if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
			t.Fatalf("reconcile %d failed: %v", i, err)
		}
	}

	got := getService(t, c, key)
	want := []corev1.LoadBalancerIngress{{IP: "10.0.0.1"}, {IP: "10.99.50.100"}}
	if !reflect.DeepEqual(got.Status.LoadBalancer.Ingress, want) {
		t.Fatalf("unexpected ingress: got %#v, want %#v", got.Status.LoadBalancer.Ingress, want)
	}
}

func TestReconcileIgnoresForeignLoadBalancerClass(t *testing.T) {
	ctx := context.Background()
	svc := testService("ingress", corev1.ServiceExternalTrafficPolicyTypeLocal)
	svc.Spec.LoadBalancerClass = ptr.To("example.com/other")
	objects := []client.Object{
		testNode("node-1", true, "10.0.0.1"),
		svc,
		testSlice("ingress", testEndpoint("node-1", true)),
	}
	r, c := newReconciler(t, objects...)

	key := types.NamespacedName{Namespace: "default", Name: "ingress"}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	got := getService(t, c, key)
	if len(got.Status.LoadBalancer.Ingress) != 0 {
		t.Fatalf("expected empty ingress for foreign class, got %#v", got.Status.LoadBalancer.Ingress)
	}
}

func newReconciler(t *testing.T, objects ...client.Object) (*ServiceReconciler, client.Client) {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to build scheme: %v", err)
	}
	if err := easylv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add easyLB scheme: %v", err)
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&corev1.Service{}).
		WithObjects(objects...).
		Build()

	return &ServiceReconciler{
		Client:       c,
		LBClass:      testClass,
		VIPSource:    vip.BindingSource{Client: c},
		RequireClass: true,
		AddressOptions: address.Options{
			NodeIPTypes:    []corev1.NodeAddressType{corev1.NodeInternalIP},
			PublishNodeIPs: true,
		},
	}, c
}

func getService(t *testing.T, c client.Client, key types.NamespacedName) *corev1.Service {
	t.Helper()
	var svc corev1.Service
	if err := c.Get(context.Background(), key, &svc); err != nil {
		t.Fatalf("failed to get service: %v", err)
	}
	return &svc
}

func testService(name string, policy corev1.ServiceExternalTrafficPolicyType) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Type:                  corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass:     ptr.To(testClass),
			ExternalTrafficPolicy: policy,
			IPFamilies:            []corev1.IPFamily{corev1.IPv4Protocol},
			Ports: []corev1.ServicePort{{
				Name:     "http",
				Port:     80,
				Protocol: corev1.ProtocolTCP,
			}},
		},
	}
}

func testVIPBinding(name, serviceName string, mode easylv1alpha1.VIPMode, vips ...string) *easylv1alpha1.VIPBinding {
	return &easylv1alpha1.VIPBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: easylv1alpha1.VIPBindingSpec{
			ServiceRef: easylv1alpha1.ServiceReference{Name: serviceName},
			Mode:       mode,
			VIPs:       vips,
		},
	}
}

func testNode(name string, ready bool, ip string) *corev1.Node {
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
			Addresses: []corev1.NodeAddress{{
				Type:    corev1.NodeInternalIP,
				Address: ip,
			}},
		},
	}
}

func testSlice(serviceName string, endpoints ...discoveryv1.Endpoint) *discoveryv1.EndpointSlice {
	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName + "-abc",
			Namespace: "default",
			Labels:    map[string]string{discoveryv1.LabelServiceName: serviceName},
		},
		Endpoints: endpoints,
	}
}

func testEndpoint(node string, ready bool) discoveryv1.Endpoint {
	return discoveryv1.Endpoint{
		NodeName:   ptr.To(node),
		Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(ready)},
	}
}

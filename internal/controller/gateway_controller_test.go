package controller

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	easylv1alpha1 "github.com/bzdvdn/easylb/api/v1alpha1"
)

func TestGatewayReconcilerCreatesBinding(t *testing.T) {
	gateway := testGateway("gw", "gateway-system", map[string]string{
		gatewayServiceAnnotation: "ingress",
		gatewayModeAnnotation:    "l2",
	}, []interface{}{
		map[string]interface{}{"type": "IPAddress", "value": "10.0.0.100"},
	})
	r, c := newGatewayReconciler(t, gateway)

	reconcileGateway(t, r, "gateway-system", "gw")

	binding := getGatewayBinding(t, c, "gateway-system", "gateway-gw")
	if binding.Spec.ServiceRef.Name != "ingress" {
		t.Fatalf("unexpected serviceRef: %q", binding.Spec.ServiceRef.Name)
	}
	if binding.Spec.Mode != easylv1alpha1.VIPModeL2 {
		t.Fatalf("unexpected mode: %q", binding.Spec.Mode)
	}
	if len(binding.Spec.VIPs) != 1 || binding.Spec.VIPs[0] != "10.0.0.100" {
		t.Fatalf("unexpected vips: %v", binding.Spec.VIPs)
	}
	if len(binding.OwnerReferences) != 1 || binding.OwnerReferences[0].UID != gateway.GetUID() {
		t.Fatalf("unexpected ownerReferences: %#v", binding.OwnerReferences)
	}
}

func TestGatewayReconcilerUsesPool(t *testing.T) {
	gateway := testGateway("gw", "gateway-system", map[string]string{
		gatewayServiceAnnotation: "ingress",
		gatewayPoolAnnotation:    "public-vips",
	}, nil)
	r, c := newGatewayReconciler(t, gateway)

	reconcileGateway(t, r, "gateway-system", "gw")

	binding := getGatewayBinding(t, c, "gateway-system", "gateway-gw")
	if binding.Spec.PoolRef == nil || binding.Spec.PoolRef.Name != "public-vips" {
		t.Fatalf("unexpected poolRef: %#v", binding.Spec.PoolRef)
	}
	if binding.Spec.Mode != easylv1alpha1.VIPModeExternal {
		t.Fatalf("unexpected mode: %q", binding.Spec.Mode)
	}
}

func TestGatewayReconcilerIgnoresMissingService(t *testing.T) {
	gateway := testGateway("gw", "gateway-system", nil, []interface{}{
		map[string]interface{}{"type": "IPAddress", "value": "10.0.0.100"},
	})
	r, c := newGatewayReconciler(t, gateway)

	reconcileGateway(t, r, "gateway-system", "gw")

	var binding easylv1alpha1.VIPBinding
	err := c.Get(context.Background(), types.NamespacedName{Namespace: "gateway-system", Name: "gateway-gw"}, &binding)
	if err == nil {
		t.Fatalf("expected no VIPBinding, got %#v", binding.Spec)
	}
}

func TestGatewayReconcilerUpdatesAndDeletesBinding(t *testing.T) {
	gateway := testGateway("gw", "gateway-system", map[string]string{
		gatewayServiceAnnotation: "ingress",
	}, []interface{}{
		map[string]interface{}{"type": "IPAddress", "value": "10.0.0.100"},
	})
	r, c := newGatewayReconciler(t, gateway)
	reconcileGateway(t, r, "gateway-system", "gw")

	gateway.Object["spec"] = map[string]interface{}{
		"addresses": []interface{}{
			map[string]interface{}{"type": "IPAddress", "value": "10.0.0.101"},
		},
	}
	if err := c.Update(context.Background(), gateway); err != nil {
		t.Fatalf("failed to update gateway: %v", err)
	}
	reconcileGateway(t, r, "gateway-system", "gw")

	binding := getGatewayBinding(t, c, "gateway-system", "gateway-gw")
	if len(binding.Spec.VIPs) != 1 || binding.Spec.VIPs[0] != "10.0.0.101" {
		t.Fatalf("unexpected vips after update: %v", binding.Spec.VIPs)
	}

	gateway.SetAnnotations(nil)
	if err := c.Update(context.Background(), gateway); err != nil {
		t.Fatalf("failed to update gateway: %v", err)
	}
	reconcileGateway(t, r, "gateway-system", "gw")

	var deleted easylv1alpha1.VIPBinding
	err := c.Get(context.Background(), types.NamespacedName{Namespace: "gateway-system", Name: "gateway-gw"}, &deleted)
	if err == nil {
		t.Fatal("expected VIPBinding to be deleted")
	}
}

func newGatewayReconciler(t *testing.T, objects ...client.Object) (*GatewayReconciler, client.Client) {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to build scheme: %v", err)
	}
	if err := easylv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add easyLB scheme: %v", err)
	}
	scheme.AddKnownTypeWithName(gatewayGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(gatewayGVK.GroupVersion().WithKind("GatewayList"), &unstructured.UnstructuredList{})

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&easylv1alpha1.VIPBinding{}).
		WithObjects(objects...).
		Build()
	return &GatewayReconciler{Client: c}, c
}

func testGateway(name, namespace string, annotations map[string]string, addresses []interface{}) *unstructured.Unstructured {
	gateway := &unstructured.Unstructured{}
	gateway.SetGroupVersionKind(gatewayGVK)
	gateway.SetName(name)
	gateway.SetNamespace(namespace)
	gateway.SetUID(types.UID(name + "-uid"))
	gateway.SetAnnotations(annotations)
	if addresses != nil {
		gateway.Object["spec"] = map[string]interface{}{"addresses": addresses}
	}
	return gateway
}

func reconcileGateway(t *testing.T, r *GatewayReconciler, namespace, name string) {
	t.Helper()
	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: namespace, Name: name},
	}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
}

func getGatewayBinding(t *testing.T, c client.Client, namespace, name string) *easylv1alpha1.VIPBinding {
	t.Helper()
	var binding easylv1alpha1.VIPBinding
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, &binding); err != nil {
		t.Fatalf("failed to get VIPBinding: %v", err)
	}
	return &binding
}

package controller

import (
	"context"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	easylv1alpha1 "github.com/bzdvdn/easylb/api/v1alpha1"
)

func TestBindingReconcilerExplicitVIPs(t *testing.T) {
	binding := testBinding("explicit", easylv1alpha1.VIPBindingSpec{
		ServiceRef: easylv1alpha1.ServiceReference{Name: "ingress"},
		VIPs:       []string{"10.0.0.100"},
	})
	r, c := newBindingReconciler(t, binding)

	reconcileBinding(t, r, client.ObjectKeyFromObject(binding))

	got := getBinding(t, c, client.ObjectKeyFromObject(binding))
	if !reflect.DeepEqual(got.Status.AllocatedIPs, []string{"10.0.0.100"}) {
		t.Fatalf("unexpected allocated IPs: %v", got.Status.AllocatedIPs)
	}
	condition := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
	if condition == nil || condition.Status != metav1.ConditionTrue {
		t.Fatalf("expected Ready=True, got %#v", condition)
	}
}

func TestBindingReconcilerAllocatesFromPool(t *testing.T) {
	pool := &easylv1alpha1.VIPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool"},
		Spec: easylv1alpha1.VIPPoolSpec{
			Addresses: []string{"192.168.56.240-192.168.56.242"},
		},
	}
	first := testBinding("first", easylv1alpha1.VIPBindingSpec{
		ServiceRef: easylv1alpha1.ServiceReference{Name: "ingress"},
		Mode:       easylv1alpha1.VIPModeL2,
		PoolRef:    &easylv1alpha1.PoolReference{Name: "pool"},
	})
	second := testBinding("second", easylv1alpha1.VIPBindingSpec{
		ServiceRef: easylv1alpha1.ServiceReference{Name: "ingress-b"},
		Mode:       easylv1alpha1.VIPModeL2,
		PoolRef:    &easylv1alpha1.PoolReference{Name: "pool"},
	})
	r, c := newBindingReconciler(t, pool, first, second)

	reconcileBinding(t, r, client.ObjectKeyFromObject(first))
	reconcileBinding(t, r, client.ObjectKeyFromObject(second))

	gotFirst := getBinding(t, c, client.ObjectKeyFromObject(first))
	gotSecond := getBinding(t, c, client.ObjectKeyFromObject(second))
	if !reflect.DeepEqual(gotFirst.Status.AllocatedIPs, []string{"192.168.56.240"}) {
		t.Fatalf("unexpected first allocation: %v", gotFirst.Status.AllocatedIPs)
	}
	if !reflect.DeepEqual(gotSecond.Status.AllocatedIPs, []string{"192.168.56.241"}) {
		t.Fatalf("unexpected second allocation: %v", gotSecond.Status.AllocatedIPs)
	}
}

func TestBindingReconcilerPoolExhausted(t *testing.T) {
	pool := &easylv1alpha1.VIPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool"},
		Spec: easylv1alpha1.VIPPoolSpec{
			Addresses: []string{"192.168.56.240"},
		},
	}
	first := testBinding("first", easylv1alpha1.VIPBindingSpec{
		ServiceRef: easylv1alpha1.ServiceReference{Name: "ingress"},
		Mode:       easylv1alpha1.VIPModeL2,
		PoolRef:    &easylv1alpha1.PoolReference{Name: "pool"},
	})
	second := testBinding("second", easylv1alpha1.VIPBindingSpec{
		ServiceRef: easylv1alpha1.ServiceReference{Name: "ingress-b"},
		Mode:       easylv1alpha1.VIPModeL2,
		PoolRef:    &easylv1alpha1.PoolReference{Name: "pool"},
	})
	r, c := newBindingReconciler(t, pool, first, second)

	reconcileBinding(t, r, client.ObjectKeyFromObject(first))
	reconcileBinding(t, r, client.ObjectKeyFromObject(second))

	gotSecond := getBinding(t, c, client.ObjectKeyFromObject(second))
	condition := meta.FindStatusCondition(gotSecond.Status.Conditions, conditionReady)
	if condition == nil || condition.Reason != "PoolExhausted" {
		t.Fatalf("expected PoolExhausted condition, got %#v", condition)
	}
	if len(gotSecond.Status.AllocatedIPs) != 0 {
		t.Fatalf("expected no allocation, got %v", gotSecond.Status.AllocatedIPs)
	}
}

func TestBindingReconcilerRequestedIP(t *testing.T) {
	pool := &easylv1alpha1.VIPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool"},
		Spec: easylv1alpha1.VIPPoolSpec{
			Addresses: []string{"192.168.56.240-192.168.56.242"},
		},
	}
	binding := testBinding("requested", easylv1alpha1.VIPBindingSpec{
		ServiceRef:  easylv1alpha1.ServiceReference{Name: "ingress"},
		Mode:        easylv1alpha1.VIPModeL2,
		PoolRef:     &easylv1alpha1.PoolReference{Name: "pool"},
		RequestedIP: "192.168.56.241",
	})
	r, c := newBindingReconciler(t, pool, binding)

	reconcileBinding(t, r, client.ObjectKeyFromObject(binding))

	got := getBinding(t, c, client.ObjectKeyFromObject(binding))
	if !reflect.DeepEqual(got.Status.AllocatedIPs, []string{"192.168.56.241"}) {
		t.Fatalf("unexpected allocation: %v", got.Status.AllocatedIPs)
	}
}

func TestBindingReconcilerDetectsConflict(t *testing.T) {
	first := testBinding("first", easylv1alpha1.VIPBindingSpec{
		ServiceRef: easylv1alpha1.ServiceReference{Name: "ingress"},
		VIPs:       []string{"10.0.0.100"},
	})
	second := testBinding("second", easylv1alpha1.VIPBindingSpec{
		ServiceRef: easylv1alpha1.ServiceReference{Name: "ingress-b"},
		VIPs:       []string{"10.0.0.100"},
	})
	r, c := newBindingReconciler(t, first, second)

	reconcileBinding(t, r, client.ObjectKeyFromObject(first))

	got := getBinding(t, c, client.ObjectKeyFromObject(first))
	condition := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
	if condition == nil || condition.Reason != "Conflict" {
		t.Fatalf("expected Conflict condition, got %#v", condition)
	}
	if len(got.Status.AllocatedIPs) != 0 {
		t.Fatalf("expected no allocation on conflict, got %v", got.Status.AllocatedIPs)
	}
}

func newBindingReconciler(t *testing.T, objects ...client.Object) (*VIPBindingReconciler, client.Client) {
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
		WithStatusSubresource(&easylv1alpha1.VIPBinding{}).
		WithObjects(objects...).
		Build()
	return &VIPBindingReconciler{Client: c}, c
}

func reconcileBinding(t *testing.T, r *VIPBindingReconciler, key types.NamespacedName) {
	t.Helper()
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
}

func getBinding(t *testing.T, c client.Client, key types.NamespacedName) *easylv1alpha1.VIPBinding {
	t.Helper()
	var binding easylv1alpha1.VIPBinding
	if err := c.Get(context.Background(), key, &binding); err != nil {
		t.Fatalf("failed to get binding: %v", err)
	}
	return &binding
}

func testBinding(name string, spec easylv1alpha1.VIPBindingSpec) *easylv1alpha1.VIPBinding {
	return &easylv1alpha1.VIPBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec:       spec,
	}
}

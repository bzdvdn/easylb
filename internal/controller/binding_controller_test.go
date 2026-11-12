package controller

import (
	"context"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
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
	reconcileBinding(t, r, client.ObjectKeyFromObject(second))

	gotFirst := getBinding(t, c, client.ObjectKeyFromObject(first))
	if !reflect.DeepEqual(gotFirst.Status.AllocatedIPs, []string{"10.0.0.100"}) {
		t.Fatalf("expected the first binding to win, got %v", gotFirst.Status.AllocatedIPs)
	}
	gotSecond := getBinding(t, c, client.ObjectKeyFromObject(second))
	condition := meta.FindStatusCondition(gotSecond.Status.Conditions, conditionReady)
	if condition == nil || condition.Reason != "Conflict" {
		t.Fatalf("expected Conflict condition on the second binding, got %#v", condition)
	}
	if len(gotSecond.Status.AllocatedIPs) != 0 {
		t.Fatalf("expected no allocation on conflict, got %v", gotSecond.Status.AllocatedIPs)
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

func TestBindingReconcilerAvoidBuggyIPs(t *testing.T) {
	tests := []struct {
		name          string
		avoid         bool
		wantAllocated string
	}{
		{name: "enabled", avoid: true, wantAllocated: "192.168.56.1"},
		{name: "disabled", avoid: false, wantAllocated: "192.168.56.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := &easylv1alpha1.VIPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool"},
				Spec: easylv1alpha1.VIPPoolSpec{
					Addresses:     []string{"192.168.56.0/24"},
					AvoidBuggyIPs: tt.avoid,
				},
			}
			binding := testBinding("first", easylv1alpha1.VIPBindingSpec{
				ServiceRef: easylv1alpha1.ServiceReference{Name: "ingress"},
				Mode:       easylv1alpha1.VIPModeL2,
				PoolRef:    &easylv1alpha1.PoolReference{Name: "pool"},
			})
			r, c := newBindingReconciler(t, pool, binding)

			reconcileBinding(t, r, client.ObjectKeyFromObject(binding))

			got := getBinding(t, c, client.ObjectKeyFromObject(binding))
			if !reflect.DeepEqual(got.Status.AllocatedIPs, []string{tt.wantAllocated}) {
				t.Fatalf("unexpected allocation: %v", got.Status.AllocatedIPs)
			}
		})
	}
}

func TestBindingReconcilerAvoidBuggyRequestedIP(t *testing.T) {
	pool := &easylv1alpha1.VIPPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool"},
		Spec: easylv1alpha1.VIPPoolSpec{
			Addresses:     []string{"192.168.56.0/24"},
			AvoidBuggyIPs: true,
		},
	}
	binding := testBinding("requested", easylv1alpha1.VIPBindingSpec{
		ServiceRef:  easylv1alpha1.ServiceReference{Name: "ingress"},
		Mode:        easylv1alpha1.VIPModeL2,
		PoolRef:     &easylv1alpha1.PoolReference{Name: "pool"},
		RequestedIP: "192.168.56.255",
	})
	r, c := newBindingReconciler(t, pool, binding)

	reconcileBinding(t, r, client.ObjectKeyFromObject(binding))

	got := getBinding(t, c, client.ObjectKeyFromObject(binding))
	condition := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
	if condition == nil || condition.Reason != "RequestedIPUnavailable" {
		t.Fatalf("expected RequestedIPUnavailable, got %#v", condition)
	}
}

func TestBindingReconcilerSharedIP(t *testing.T) {
	tests := []struct {
		name          string
		keyA          string
		keyB          string
		portA         int32
		portB         int32
		wantAllocated bool
	}{
		{
			name:          "same key disjoint ports",
			keyA:          "shared",
			keyB:          "shared",
			portA:         80,
			portB:         81,
			wantAllocated: true,
		},
		{
			name:          "same key overlapping ports",
			keyA:          "shared",
			keyB:          "shared",
			portA:         80,
			portB:         80,
			wantAllocated: false,
		},
		{
			name:          "different keys",
			keyA:          "shared-a",
			keyB:          "shared-b",
			portA:         80,
			portB:         81,
			wantAllocated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svcA := autoTestService("a", "default", nil, nil)
			svcA.Spec.Ports = []corev1.ServicePort{{Port: tt.portA}}
			svcB := autoTestService("b", "default", nil, nil)
			svcB.Spec.Ports = []corev1.ServicePort{{Port: tt.portB}}
			first := testBinding("first", easylv1alpha1.VIPBindingSpec{
				ServiceRef: easylv1alpha1.ServiceReference{Name: "a"},
				Mode:       easylv1alpha1.VIPModeL2,
				VIPs:       []string{"10.0.0.50"},
				SharingKey: tt.keyA,
			})
			second := testBinding("second", easylv1alpha1.VIPBindingSpec{
				ServiceRef: easylv1alpha1.ServiceReference{Name: "b"},
				Mode:       easylv1alpha1.VIPModeL2,
				VIPs:       []string{"10.0.0.50"},
				SharingKey: tt.keyB,
			})
			r, c := newBindingReconciler(t, svcA, svcB, first, second)

			reconcileBinding(t, r, client.ObjectKeyFromObject(first))
			reconcileBinding(t, r, client.ObjectKeyFromObject(second))

			gotFirst := getBinding(t, c, client.ObjectKeyFromObject(first))
			gotSecond := getBinding(t, c, client.ObjectKeyFromObject(second))
			if !reflect.DeepEqual(gotFirst.Status.AllocatedIPs, []string{"10.0.0.50"}) {
				t.Fatalf("unexpected first allocation: %v", gotFirst.Status.AllocatedIPs)
			}
			if tt.wantAllocated {
				if !reflect.DeepEqual(gotSecond.Status.AllocatedIPs, []string{"10.0.0.50"}) {
					t.Fatalf("unexpected second allocation: %v", gotSecond.Status.AllocatedIPs)
				}
				return
			}
			condition := meta.FindStatusCondition(gotSecond.Status.Conditions, conditionReady)
			if condition == nil || condition.Reason != "Conflict" {
				t.Fatalf("expected Conflict, got %#v", condition)
			}
		})
	}
}

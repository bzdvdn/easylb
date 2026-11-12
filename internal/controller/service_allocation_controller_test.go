package controller

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	easylv1alpha1 "github.com/bzdvdn/easylb/api/v1alpha1"
)

func TestAutoBindingCreatesFromPool(t *testing.T) {
	pool := autoTestPool("pool-a", []string{"10.0.0.10", "10.0.0.11"}, nil, "", nil)
	svc := autoTestService("web", "default", nil, nil)
	r, c := newAutoReconciler(t, pool, svc)

	reconcileAuto(t, r, "default", "web")

	binding := getAutoBinding(t, c, "default", "auto-web")
	if binding.Spec.PoolRef == nil || binding.Spec.PoolRef.Name != "pool-a" {
		t.Fatalf("unexpected poolRef: %#v", binding.Spec.PoolRef)
	}
	if binding.Spec.Mode != easylv1alpha1.VIPModeL2 {
		t.Fatalf("unexpected mode: %q", binding.Spec.Mode)
	}
	if binding.Labels[autoBindingLabel] != "true" || binding.Labels[autoBindingPool] != "pool-a" {
		t.Fatalf("unexpected labels: %#v", binding.Labels)
	}
	if len(binding.OwnerReferences) != 1 || binding.OwnerReferences[0].Kind != "Service" ||
		binding.OwnerReferences[0].Name != "web" || binding.OwnerReferences[0].UID != svc.UID {
		t.Fatalf("unexpected ownerReferences: %#v", binding.OwnerReferences)
	}
}

func TestAutoBindingHonorsMetalLBPoolAnnotation(t *testing.T) {
	poolA := autoTestPool("pool-a", []string{"10.0.0.10"}, nil, "", nil)
	poolB := autoTestPool("pool-b", []string{"10.0.0.20"}, nil, "", nil)
	tests := []struct {
		name       string
		annotation string
		wantPool   string
	}{
		{name: "new annotation", annotation: metalLBPoolAnnotation, wantPool: "pool-b"},
		{name: "legacy annotation", annotation: metalLBPoolAnnotationLegacy, wantPool: "pool-b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := autoTestService("web", "default", nil, map[string]string{tt.annotation: "pool-b"})
			r, c := newAutoReconciler(t, poolA, poolB, svc)

			reconcileAuto(t, r, "default", "web")

			binding := getAutoBinding(t, c, "default", "auto-web")
			if binding.Spec.PoolRef == nil || binding.Spec.PoolRef.Name != tt.wantPool {
				t.Fatalf("unexpected poolRef: %#v", binding.Spec.PoolRef)
			}
		})
	}
}

func TestAutoBindingHonorsLoadBalancerIP(t *testing.T) {
	pool := autoTestPool("pool-a", []string{"10.0.0.10-10.0.0.30"}, nil, "", nil)

	t.Run("spec.loadBalancerIP", func(t *testing.T) {
		svc := autoTestService("web", "default", nil, nil)
		svc.Spec.LoadBalancerIP = "10.0.0.20"
		r, c := newAutoReconciler(t, pool, svc)

		reconcileAuto(t, r, "default", "web")

		binding := getAutoBinding(t, c, "default", "auto-web")
		if binding.Spec.RequestedIP != "10.0.0.20" {
			t.Fatalf("unexpected requestedIP: %q", binding.Spec.RequestedIP)
		}
		if binding.Spec.PoolRef == nil || binding.Spec.PoolRef.Name != "pool-a" {
			t.Fatalf("unexpected poolRef: %#v", binding.Spec.PoolRef)
		}
	})

	t.Run("multiple requested IPs use explicit vips", func(t *testing.T) {
		svc := autoTestService("web", "default", nil, map[string]string{
			metalLBIPsAnnotation: "10.0.0.20,10.0.0.21",
		})
		r, c := newAutoReconciler(t, pool, svc)

		reconcileAuto(t, r, "default", "web")

		binding := getAutoBinding(t, c, "default", "auto-web")
		if binding.Spec.PoolRef != nil {
			t.Fatalf("unexpected poolRef: %#v", binding.Spec.PoolRef)
		}
		assertStrings(t, "vips", binding.Spec.VIPs, []string{"10.0.0.20", "10.0.0.21"})
	})

	t.Run("explicit IP works without a pool", func(t *testing.T) {
		svc := autoTestService("web", "default", nil, nil)
		svc.Spec.LoadBalancerIP = "10.0.0.99"
		r, c := newAutoReconciler(t, svc)

		reconcileAuto(t, r, "default", "web")

		binding := getAutoBinding(t, c, "default", "auto-web")
		if binding.Spec.PoolRef != nil {
			t.Fatalf("unexpected poolRef: %#v", binding.Spec.PoolRef)
		}
		assertStrings(t, "vips", binding.Spec.VIPs, []string{"10.0.0.99"})
	})
}

func TestAutoBindingRespectsSelectors(t *testing.T) {
	prod := autoTestNamespace("prod", map[string]string{"env": "prod"})
	dev := autoTestNamespace("dev", map[string]string{"env": "dev"})
	pool := autoTestPool("pool-a", []string{"10.0.0.10"}, nil, "", &easylv1alpha1.ServiceAllocation{
		NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}},
		ServiceSelector:   &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
	})
	webProd := autoTestService("web", "prod", map[string]string{"app": "web"}, nil)
	apiProd := autoTestService("api", "prod", map[string]string{"app": "api"}, nil)
	webDev := autoTestService("web", "dev", map[string]string{"app": "web"}, nil)
	r, c := newAutoReconciler(t, prod, dev, pool, webProd, apiProd, webDev)

	for _, key := range []types.NamespacedName{
		{Namespace: "prod", Name: "web"},
		{Namespace: "prod", Name: "api"},
		{Namespace: "dev", Name: "web"},
	} {
		reconcileAuto(t, r, key.Namespace, key.Name)
	}

	if _, err := getAutoBindingResult(c, "prod", "auto-web"); err != nil {
		t.Fatalf("expected auto-web in prod: %v", err)
	}
	if _, err := getAutoBindingResult(c, "prod", "auto-api"); err == nil {
		t.Fatal("did not expect auto-api in prod")
	}
	if _, err := getAutoBindingResult(c, "dev", "auto-web"); err == nil {
		t.Fatal("did not expect auto-web in dev")
	}
}

func TestAutoBindingSkipsManualBinding(t *testing.T) {
	pool := autoTestPool("pool-a", []string{"10.0.0.10"}, nil, "", nil)
	svc := autoTestService("web", "default", nil, nil)
	manual := autoTestBinding("web-bind", "default", "web", []string{"10.0.0.50"})
	r, c := newAutoReconciler(t, pool, svc, manual)

	reconcileAuto(t, r, "default", "web")

	if _, err := getAutoBindingResult(c, "default", "auto-web"); err == nil {
		t.Fatal("did not expect an auto binding when a manual binding exists")
	}
}

func TestAutoBindingDeletedWhenManualBindingAppears(t *testing.T) {
	pool := autoTestPool("pool-a", []string{"10.0.0.10"}, nil, "", nil)
	svc := autoTestService("web", "default", nil, nil)
	r, c := newAutoReconciler(t, pool, svc)
	reconcileAuto(t, r, "default", "web")
	getAutoBinding(t, c, "default", "auto-web")

	manual := autoTestBinding("web-bind", "default", "web", []string{"10.0.0.50"})
	if err := c.Create(context.Background(), manual); err != nil {
		t.Fatalf("failed to create manual binding: %v", err)
	}
	reconcileAuto(t, r, "default", "web")

	if _, err := getAutoBindingResult(c, "default", "auto-web"); err == nil {
		t.Fatal("expected the auto binding to be deleted")
	}
	if _, err := getAutoBindingResult(c, "default", "web-bind"); err != nil {
		t.Fatalf("manual binding must stay: %v", err)
	}
}

func TestAutoBindingDeletedWhenServiceUnmanaged(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*corev1.Service)
	}{
		{
			name: "not LoadBalancer",
			mutate: func(svc *corev1.Service) {
				svc.Spec.Type = corev1.ServiceTypeClusterIP
			},
		},
		{
			name: "auto-assign disabled",
			mutate: func(svc *corev1.Service) {
				svc.Annotations = map[string]string{autoAssignAnnotation: "false"}
			},
		},
		{
			name: "wrong loadBalancerClass",
			mutate: func(svc *corev1.Service) {
				svc.Spec.LoadBalancerClass = ptr.To("example.com/other")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := autoTestPool("pool-a", []string{"10.0.0.10"}, nil, "", nil)
			svc := autoTestService("web", "default", nil, nil)
			r, c := newAutoReconciler(t, pool, svc)
			reconcileAuto(t, r, "default", "web")
			getAutoBinding(t, c, "default", "auto-web")

			tt.mutate(svc)
			if err := c.Update(context.Background(), svc); err != nil {
				t.Fatalf("failed to update service: %v", err)
			}
			reconcileAuto(t, r, "default", "web")

			if _, err := getAutoBindingResult(c, "default", "auto-web"); err == nil {
				t.Fatal("expected the auto binding to be deleted")
			}
		})
	}
}

func TestAutoBindingKeepsExistingWhenNoPoolMatches(t *testing.T) {
	pool := autoTestPool("pool-a", []string{"10.0.0.10"}, nil, "", nil)
	svc := autoTestService("web", "default", nil, nil)
	r, c := newAutoReconciler(t, pool, svc)
	reconcileAuto(t, r, "default", "web")
	getAutoBinding(t, c, "default", "auto-web")

	if err := c.Delete(context.Background(), pool); err != nil {
		t.Fatalf("failed to delete pool: %v", err)
	}
	reconcileAuto(t, r, "default", "web")

	getAutoBinding(t, c, "default", "auto-web")
}

func TestAutoBindingUpdatesPoolFromAnnotation(t *testing.T) {
	poolA := autoTestPool("pool-a", []string{"10.0.0.10"}, nil, "", nil)
	poolB := autoTestPool("pool-b", []string{"10.0.0.20"}, nil, "", nil)
	svc := autoTestService("web", "default", nil, nil)
	r, c := newAutoReconciler(t, poolA, poolB, svc)
	reconcileAuto(t, r, "default", "web")

	svc.Annotations = map[string]string{metalLBPoolAnnotation: "pool-b"}
	if err := c.Update(context.Background(), svc); err != nil {
		t.Fatalf("failed to update service: %v", err)
	}
	reconcileAuto(t, r, "default", "web")

	binding := getAutoBinding(t, c, "default", "auto-web")
	if binding.Spec.PoolRef == nil || binding.Spec.PoolRef.Name != "pool-b" {
		t.Fatalf("unexpected poolRef: %#v", binding.Spec.PoolRef)
	}
}

func TestAutoBindingPoolMode(t *testing.T) {
	tests := []struct {
		name string
		mode easylv1alpha1.VIPMode
		want easylv1alpha1.VIPMode
	}{
		{name: "default", want: easylv1alpha1.VIPModeL2},
		{name: "external", mode: easylv1alpha1.VIPModeExternal, want: easylv1alpha1.VIPModeExternal},
		{name: "l2", mode: easylv1alpha1.VIPModeL2, want: easylv1alpha1.VIPModeL2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := autoTestPool("pool-a", []string{"10.0.0.10"}, nil, tt.mode, nil)
			svc := autoTestService("web", "default", nil, nil)
			r, c := newAutoReconciler(t, pool, svc)

			reconcileAuto(t, r, "default", "web")

			binding := getAutoBinding(t, c, "default", "auto-web")
			if binding.Spec.Mode != tt.want {
				t.Fatalf("got mode %q, want %q", binding.Spec.Mode, tt.want)
			}
		})
	}
}

func TestRequestedIPs(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		lbIP        string
		want        []string
		wantErr     bool
	}{
		{name: "empty"},
		{
			name:        "annotation",
			annotations: map[string]string{metalLBIPsAnnotation: "10.0.0.20, 10.0.0.21"},
			want:        []string{"10.0.0.20", "10.0.0.21"},
		},
		{
			name:        "annotation wins over spec",
			annotations: map[string]string{metalLBIPsAnnotation: "10.0.0.20"},
			lbIP:        "10.0.0.30",
			want:        []string{"10.0.0.20"},
		},
		{
			name:        "legacy annotation",
			annotations: map[string]string{metalLBIPsAnnotationLegacy: "10.0.0.21"},
			want:        []string{"10.0.0.21"},
		},
		{
			name:        "dedupe",
			annotations: map[string]string{metalLBIPsAnnotation: "10.0.0.20,10.0.0.20"},
			want:        []string{"10.0.0.20"},
		},
		{
			name:    "invalid",
			lbIP:    "not-an-ip",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := autoTestService("web", "default", nil, tt.annotations)
			svc.Spec.LoadBalancerIP = tt.lbIP

			got, err := requestedIPs(svc)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertStrings(t, "requested IPs", got, tt.want)
		})
	}
}

func TestAutoBindingName(t *testing.T) {
	if got := autoBindingName("web"); got != "auto-web" {
		t.Fatalf("unexpected name: %q", got)
	}
	long := autoBindingName(string(make([]byte, 300)))
	if len(long) != 253 {
		t.Fatalf("unexpected length: %d", len(long))
	}
}

func newAutoReconciler(t *testing.T, objects ...client.Object) (*ServiceAllocationReconciler, client.Client) {
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
		WithObjects(objects...).
		Build()
	return &ServiceAllocationReconciler{
		Client:       c,
		LBClass:      "easylb.io/easylb",
		RequireClass: false,
		RequeueAfter: time.Minute,
	}, c
}

func autoTestService(name, namespace string, serviceLabels, annotations map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			UID:         types.UID(namespace + "-" + name + "-uid"),
			Labels:      serviceLabels,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
	}
}

func autoTestNamespace(name string, namespaceLabels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: namespaceLabels,
		},
	}
}

func autoTestPool(name string, addresses []string, autoAssign *bool, mode easylv1alpha1.VIPMode, allocation *easylv1alpha1.ServiceAllocation) *easylv1alpha1.VIPPool {
	return &easylv1alpha1.VIPPool{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: easylv1alpha1.VIPPoolSpec{
			Addresses:         addresses,
			AutoAssign:        autoAssign,
			Mode:              mode,
			ServiceAllocation: allocation,
		},
	}
}

func autoTestBinding(name, namespace, service string, vips []string) *easylv1alpha1.VIPBinding {
	return &easylv1alpha1.VIPBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: easylv1alpha1.VIPBindingSpec{
			ServiceRef: easylv1alpha1.ServiceReference{Name: service},
			Mode:       easylv1alpha1.VIPModeL2,
			VIPs:       vips,
		},
		Status: easylv1alpha1.VIPBindingStatus{AllocatedIPs: append([]string(nil), vips...)},
	}
}

func reconcileAuto(t *testing.T, r *ServiceAllocationReconciler, namespace, name string) {
	t.Helper()
	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: namespace, Name: name},
	}); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
}

func getAutoBinding(t *testing.T, c client.Client, namespace, name string) *easylv1alpha1.VIPBinding {
	t.Helper()
	binding, err := getAutoBindingResult(c, namespace, name)
	if err != nil {
		t.Fatalf("failed to get VIPBinding: %v", err)
	}
	return binding
}

func getAutoBindingResult(c client.Client, namespace, name string) (*easylv1alpha1.VIPBinding, error) {
	var binding easylv1alpha1.VIPBinding
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, &binding); err != nil {
		return nil, err
	}
	return &binding, nil
}

func TestAutoBindingFallsBackToNextPool(t *testing.T) {
	poolA := autoTestPool("pool-a", []string{"10.0.0.10"}, nil, "", nil)
	poolB := autoTestPool("pool-b", []string{"10.0.0.20"}, nil, "", nil)
	other := autoTestBinding("other-bind", "default", "other", []string{"10.0.0.10"})
	svc := autoTestService("web", "default", nil, nil)
	r, c := newAutoReconciler(t, poolA, poolB, other, svc)

	reconcileAuto(t, r, "default", "web")

	binding := getAutoBinding(t, c, "default", "auto-web")
	if binding.Spec.PoolRef == nil || binding.Spec.PoolRef.Name != "pool-b" {
		t.Fatalf("unexpected poolRef: %#v", binding.Spec.PoolRef)
	}
}

func TestAutoBindingMovesFromExhaustedPool(t *testing.T) {
	poolA := autoTestPool("pool-a", []string{"10.0.0.10"}, nil, "", nil)
	svc := autoTestService("web", "default", nil, nil)
	r, c := newAutoReconciler(t, poolA, svc)
	reconcileAuto(t, r, "default", "web")

	binding := getAutoBinding(t, c, "default", "auto-web")
	if binding.Spec.PoolRef == nil || binding.Spec.PoolRef.Name != "pool-a" {
		t.Fatalf("unexpected initial poolRef: %#v", binding.Spec.PoolRef)
	}

	other := autoTestBinding("other-bind", "default", "other", []string{"10.0.0.10"})
	poolB := autoTestPool("pool-b", []string{"10.0.0.20"}, nil, "", nil)
	if err := c.Create(context.Background(), other); err != nil {
		t.Fatalf("failed to create other binding: %v", err)
	}
	if err := c.Create(context.Background(), poolB); err != nil {
		t.Fatalf("failed to create pool-b: %v", err)
	}
	reconcileAuto(t, r, "default", "web")

	binding = getAutoBinding(t, c, "default", "auto-web")
	if binding.Spec.PoolRef == nil || binding.Spec.PoolRef.Name != "pool-b" {
		t.Fatalf("unexpected poolRef after exhaustion: %#v", binding.Spec.PoolRef)
	}
}

func TestAutoBindingSharesIP(t *testing.T) {
	tests := []struct {
		name       string
		apiPort    int32
		webPort    int32
		wantShared bool
	}{
		{name: "disjoint ports", apiPort: 80, webPort: 81, wantShared: true},
		{name: "overlapping ports", apiPort: 80, webPort: 80, wantShared: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := autoTestService("api", "default", nil, map[string]string{
				metalLBSharedIPAnnotation: "shared",
			})
			api.Spec.Ports = []corev1.ServicePort{{Port: tt.apiPort}}
			web := autoTestService("web", "default", nil, map[string]string{
				metalLBSharedIPAnnotation: "shared",
			})
			web.Spec.Ports = []corev1.ServicePort{{Port: tt.webPort}}

			existing := autoTestBinding("auto-api", "default", "api", []string{"10.0.0.20"})
			existing.Spec.SharingKey = "shared"
			pool := autoTestPool("pool-a", []string{"10.0.0.10-10.0.0.30"}, nil, "", nil)
			r, c := newAutoReconciler(t, pool, api, web, existing)

			reconcileAuto(t, r, "default", "web")

			binding := getAutoBinding(t, c, "default", "auto-web")
			if binding.Spec.SharingKey != "shared" {
				t.Fatalf("unexpected sharingKey: %q", binding.Spec.SharingKey)
			}
			if tt.wantShared {
				if binding.Spec.PoolRef != nil {
					t.Fatalf("unexpected poolRef: %#v", binding.Spec.PoolRef)
				}
				assertStrings(t, "vips", binding.Spec.VIPs, []string{"10.0.0.20"})
				return
			}
			if binding.Spec.PoolRef == nil || binding.Spec.PoolRef.Name != "pool-a" {
				t.Fatalf("expected a separate pool allocation, got %#v", binding.Spec.PoolRef)
			}
		})
	}
}

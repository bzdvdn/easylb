package controller

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	easylv1alpha1 "github.com/bzdvdn/easylb/api/v1alpha1"
)

// These tests do not need a cluster or Gateway API CRDs. They build the same
// unstructured Gateway objects that different Gateway API releases store and
// check that easyLB only depends on the stable v1 surface it actually uses.

func TestGatewayGVKIsV1(t *testing.T) {
	if got := gatewayGVK.GroupVersion().String(); got != "gateway.networking.k8s.io/v1" {
		t.Fatalf("unexpected Gateway GVK: %s", got)
	}
}

func TestGatewayAddressesSyntheticVersions(t *testing.T) {
	ipAddress := func(ip string) map[string]interface{} {
		return map[string]interface{}{"type": "IPAddress", "value": ip}
	}

	tests := []struct {
		name        string
		spec        map[string]interface{}
		status      map[string]interface{}
		annotations map[string]string
		wantVIPs    []string
		wantInvalid []string
	}{
		{
			name: "v1.0 basic addresses",
			spec: map[string]interface{}{
				"addresses": []interface{}{ipAddress("10.0.0.100")},
			},
			wantVIPs: []string{"10.0.0.100"},
		},
		{
			name: "v1.2 infrastructure and listeners",
			spec: map[string]interface{}{
				"gatewayClassName": "eg",
				"infrastructure": map[string]interface{}{
					"labels": map[string]interface{}{"app": "gw"},
				},
				"listeners": []interface{}{
					map[string]interface{}{
						"name":     "http",
						"protocol": "HTTP",
						"port":     int64(80),
						"allowedRoutes": map[string]interface{}{
							"namespaces": map[string]interface{}{"from": "Same"},
						},
					},
				},
				"addresses": []interface{}{
					ipAddress("10.0.0.100"),
					ipAddress("10.0.0.100"),
					ipAddress("10.0.0.101"),
				},
			},
			wantVIPs: []string{"10.0.0.100", "10.0.0.101"},
		},
		{
			name: "v1.4 hostname is skipped",
			spec: map[string]interface{}{
				"addresses": []interface{}{
					map[string]interface{}{"type": "Hostname", "value": "gw.example.com"},
					ipAddress("10.0.0.101"),
				},
			},
			wantVIPs: []string{"10.0.0.101"},
		},
		{
			name: "v1.5 address routability is ignored",
			spec: map[string]interface{}{
				"addresses": []interface{}{
					map[string]interface{}{
						"type":        "IPAddress",
						"value":       "10.0.0.102",
						"routability": "Routable",
					},
				},
			},
			wantVIPs: []string{"10.0.0.102"},
		},
		{
			name: "v1.7 unknown listener and address fields are ignored",
			spec: map[string]interface{}{
				"addresses": []interface{}{
					map[string]interface{}{
						"type":        "IPAddress",
						"value":       "2001:db8::10",
						"routability": "Routable",
						"futureField": map[string]interface{}{"enabled": true},
					},
				},
				"listeners": []interface{}{
					map[string]interface{}{
						"name":     "https",
						"protocol": "HTTPS",
						"port":     int64(443),
						"tls": map[string]interface{}{
							"mode": "Terminate",
						},
						"filters": []interface{}{
							map[string]interface{}{"type": "FutureFilter"},
						},
					},
				},
				"someFutureField": map[string]interface{}{"enabled": true},
			},
			wantVIPs: []string{"2001:db8::10"},
		},
		{
			name: "status addresses are not a source",
			spec: map[string]interface{}{
				"listeners": []interface{}{
					map[string]interface{}{"name": "http", "protocol": "HTTP", "port": int64(80)},
				},
			},
			status: map[string]interface{}{
				"addresses": []interface{}{
					map[string]interface{}{"type": "IPAddress", "value": "10.0.0.200"},
				},
			},
			wantVIPs: nil,
		},
		{
			name: "annotation fallback when spec has no addresses",
			spec: map[string]interface{}{
				"listeners": []interface{}{
					map[string]interface{}{"name": "http", "protocol": "HTTP", "port": int64(80)},
				},
			},
			annotations: map[string]string{gatewayVIPsAnnotation: "10.0.0.103, 10.0.0.104"},
			wantVIPs:    []string{"10.0.0.103", "10.0.0.104"},
		},
		{
			name: "annotation fallback after hostname-only spec",
			spec: map[string]interface{}{
				"addresses": []interface{}{
					map[string]interface{}{"type": "Hostname", "value": "gw.example.com"},
				},
			},
			annotations: map[string]string{gatewayVIPsAnnotation: "10.0.0.105"},
			wantVIPs:    []string{"10.0.0.105"},
		},
		{
			name: "invalid spec addresses are reported and annotation fallback is used",
			spec: map[string]interface{}{
				"addresses": []interface{}{
					ipAddress("not-an-ip"),
				},
			},
			annotations: map[string]string{gatewayVIPsAnnotation: "10.0.0.106"},
			wantVIPs:    []string{"10.0.0.106"},
			wantInvalid: []string{"not-an-ip"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := gatewayWithSpec("gw", "gateway-system", tt.annotations, tt.spec)
			if tt.status != nil {
				gateway.Object["status"] = tt.status
			}

			vips, invalid := gatewayAddresses(gateway, gateway.GetAnnotations())
			assertStrings(t, "vips", vips, tt.wantVIPs)
			assertStrings(t, "invalid", invalid, tt.wantInvalid)
		})
	}
}

func TestGatewayReconcilerSyntheticNewerAPIShapes(t *testing.T) {
	tests := []struct {
		name        string
		spec        map[string]interface{}
		status      map[string]interface{}
		annotations map[string]string
		wantVIPs    []string
		wantPool    string
	}{
		{
			name: "new address fields",
			spec: map[string]interface{}{
				"addresses": []interface{}{
					map[string]interface{}{
						"type":        "IPAddress",
						"value":       "10.0.0.110",
						"routability": "Routable",
					},
				},
				"someFutureField": map[string]interface{}{"enabled": true},
			},
			annotations: map[string]string{
				gatewayServiceAnnotation: "envoy-gw-abc123",
				gatewayModeAnnotation:    "l2",
				gatewayPoolAnnotation:    "must-be-ignored",
			},
			wantVIPs: []string{"10.0.0.110"},
		},
		{
			name: "status only falls back to pool",
			spec: map[string]interface{}{
				"listeners": []interface{}{
					map[string]interface{}{"name": "http", "protocol": "HTTP", "port": int64(80)},
				},
			},
			status: map[string]interface{}{
				"addresses": []interface{}{
					map[string]interface{}{"type": "IPAddress", "value": "10.0.0.200"},
				},
			},
			annotations: map[string]string{
				gatewayServiceAnnotation: "ngf-gw-nginx",
				gatewayModeAnnotation:    "external",
				gatewayPoolAnnotation:    "gateway-vips",
			},
			wantPool: "gateway-vips",
		},
		{
			name: "annotation vips win over pool",
			spec: map[string]interface{}{
				"listeners": []interface{}{
					map[string]interface{}{"name": "http", "protocol": "HTTP", "port": int64(80)},
				},
			},
			annotations: map[string]string{
				gatewayServiceAnnotation: "gw-nginx",
				gatewayVIPsAnnotation:    "10.0.0.111,10.0.0.112",
				gatewayPoolAnnotation:    "must-be-ignored",
			},
			wantVIPs: []string{"10.0.0.111", "10.0.0.112"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := gatewayWithSpec("gw", "gateway-system", tt.annotations, tt.spec)
			if tt.status != nil {
				gateway.Object["status"] = tt.status
			}
			r, c := newGatewayReconciler(t, gateway)

			reconcileGateway(t, r, "gateway-system", "gw")

			binding := getGatewayBinding(t, c, "gateway-system", "gateway-gw")
			assertStrings(t, "vips", binding.Spec.VIPs, tt.wantVIPs)
			if tt.wantPool == "" {
				if binding.Spec.PoolRef != nil {
					t.Fatalf("unexpected poolRef: %#v", binding.Spec.PoolRef)
				}
			} else if binding.Spec.PoolRef == nil || binding.Spec.PoolRef.Name != tt.wantPool {
				t.Fatalf("unexpected poolRef: %#v", binding.Spec.PoolRef)
			}
			if binding.Spec.ServiceRef.Name != tt.annotations[gatewayServiceAnnotation] {
				t.Fatalf("unexpected serviceRef: %q", binding.Spec.ServiceRef.Name)
			}
		})
	}
}

func TestGatewayReconcilerDoesNotTouchGatewayStatus(t *testing.T) {
	status := map[string]interface{}{
		"addresses": []interface{}{
			map[string]interface{}{"type": "IPAddress", "value": "10.0.0.200"},
		},
	}
	gateway := gatewayWithSpec("gw", "gateway-system", map[string]string{
		gatewayServiceAnnotation: "ingress",
	}, map[string]interface{}{
		"addresses": []interface{}{
			map[string]interface{}{"type": "IPAddress", "value": "10.0.0.100"},
		},
	})
	gateway.Object["status"] = status
	r, c := newGatewayReconciler(t, gateway)

	reconcileGateway(t, r, "gateway-system", "gw")

	var stored unstructured.Unstructured
	stored.SetGroupVersionKind(gatewayGVK)
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), &stored); err != nil {
		t.Fatalf("failed to get Gateway: %v", err)
	}
	gotStatus, found, err := unstructured.NestedMap(stored.Object, "status")
	if err != nil || !found {
		t.Fatalf("status disappeared: found=%v err=%v", found, err)
	}
	assertStrings(t, "status addresses",
		unstructuredAddressValues(gotStatus),
		[]string{"10.0.0.200"})
}

func TestGatewayReconcilerPreservesForeignKeyBinding(t *testing.T) {
	gateway := testGateway("gw", "gateway-system", map[string]string{
		gatewayServiceAnnotation: "ingress",
	}, []interface{}{
		map[string]interface{}{"type": "IPAddress", "value": "10.0.0.100"},
	})
	foreign := &easylv1alpha1.VIPBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gateway-gw",
			Namespace: "gateway-system",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "gateway.networking.k8s.io/v1",
				Kind:       "Gateway",
				Name:       "other",
				UID:        types.UID("other-uid"),
			}},
		},
		Spec: easylv1alpha1.VIPBindingSpec{
			ServiceRef: easylv1alpha1.ServiceReference{Name: "other-service"},
			Mode:       easylv1alpha1.VIPModeExternal,
			VIPs:       []string{"10.0.0.99"},
		},
	}
	r, c := newGatewayReconciler(t, gateway, foreign)

	reconcileGateway(t, r, "gateway-system", "gw")

	binding := getGatewayBinding(t, c, "gateway-system", "gateway-gw")
	if binding.Spec.ServiceRef.Name != "other-service" {
		t.Fatalf("foreign binding was overwritten: %#v", binding.Spec)
	}
	assertStrings(t, "vips", binding.Spec.VIPs, []string{"10.0.0.99"})
}

func TestGatewayModeParsing(t *testing.T) {
	tests := []struct {
		value   string
		want    easylv1alpha1.VIPMode
		wantErr bool
	}{
		{value: "", want: easylv1alpha1.VIPModeExternal},
		{value: "external", want: easylv1alpha1.VIPModeExternal},
		{value: "L2", want: easylv1alpha1.VIPModeL2},
		{value: "bgp", want: easylv1alpha1.VIPModeBGP},
		{value: "bogus", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, err := gatewayMode(map[string]string{gatewayModeAnnotation: tt.value})
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func gatewayWithSpec(name, namespace string, annotations map[string]string, spec map[string]interface{}) *unstructured.Unstructured {
	gateway := testGateway(name, namespace, annotations, nil)
	gateway.Object["spec"] = spec
	return gateway
}

func unstructuredAddressValues(status map[string]interface{}) []string {
	addresses, _, _ := unstructured.NestedSlice(status, "addresses")
	out := make([]string, 0, len(addresses))
	for _, item := range addresses {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		value, _, _ := unstructured.NestedString(entry, "value")
		out = append(out, value)
	}
	return out
}

func assertStrings(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v, want %v", label, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: got %v, want %v", label, got, want)
		}
	}
}

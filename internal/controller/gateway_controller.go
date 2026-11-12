package controller

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	easylv1alpha1 "github.com/bzdvdn/easylb/api/v1alpha1"
)

var gatewayGVK = schema.GroupVersionKind{
	Group:   "gateway.networking.k8s.io",
	Version: "v1",
	Kind:    "Gateway",
}

const (
	gatewayServiceAnnotation = "easylb.io/service"
	gatewayModeAnnotation    = "easylb.io/vip-mode"
	gatewayPoolAnnotation    = "easylb.io/vip-pool"
	gatewayVIPsAnnotation    = "easylb.io/vips"
	gatewayBindingPrefix     = "gateway-"
)

// GatewayReconciler is an optional integration for Gateway API. It does not
// implement Gateway API; it only translates a Gateway into a VIPBinding for the
// data-plane Service created by the Gateway implementation.
type GatewayReconciler struct {
	client.Client
	Recorder     record.EventRecorder
	RequeueAfter time.Duration
}

// Reconcile implements reconcile.Reconciler.
func (r *GatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	gateway := &unstructured.Unstructured{}
	gateway.SetGroupVersionKind(gatewayGVK)
	if err := r.Get(ctx, req.NamespacedName, gateway); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	bindingKey := types.NamespacedName{
		Namespace: gateway.GetNamespace(),
		Name:      gatewayBindingName(gateway.GetName()),
	}

	annotations := gateway.GetAnnotations()
	serviceName := strings.TrimSpace(annotations[gatewayServiceAnnotation])
	if serviceName == "" {
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, r.deleteOwnedBinding(ctx, gateway, bindingKey)
	}

	mode, err := gatewayMode(annotations)
	if err != nil {
		r.event(gateway, corev1.EventTypeWarning, "InvalidConfiguration", err.Error())
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	vips, invalid := gatewayAddresses(gateway, annotations)
	if len(invalid) > 0 {
		r.event(gateway, corev1.EventTypeWarning, "InvalidVIP",
			fmt.Sprintf("ignoring invalid VIP values: %v", invalid))
	}
	poolRef := strings.TrimSpace(annotations[gatewayPoolAnnotation])
	if len(vips) > 0 {
		poolRef = ""
	}
	if len(vips) == 0 && poolRef == "" {
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, r.deleteOwnedBinding(ctx, gateway, bindingKey)
	}

	desired := desiredBinding(gateway, serviceName, mode, vips, poolRef)

	var existing easylv1alpha1.VIPBinding
	if err := r.Get(ctx, bindingKey, &existing); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, desired); err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("created VIPBinding from Gateway", "gateway", gateway.GetName(), "binding", bindingKey.Name)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	if !ownedByGateway(&existing, gateway) {
		r.event(gateway, corev1.EventTypeWarning, "BindingConflict",
			fmt.Sprintf("VIPBinding %s already exists and is not owned by this Gateway", bindingKey.Name))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	labels := make(map[string]string, len(existing.Labels)+len(desired.Labels))
	for key, value := range existing.Labels {
		labels[key] = value
	}
	for key, value := range desired.Labels {
		labels[key] = value
	}

	if equality.Semantic.DeepEqual(existing.Spec, desired.Spec) &&
		equality.Semantic.DeepEqual(existing.Labels, labels) &&
		equality.Semantic.DeepEqual(existing.OwnerReferences, desired.OwnerReferences) {
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	existing.Spec = desired.Spec
	existing.Labels = labels
	existing.OwnerReferences = desired.OwnerReferences
	if err := r.Update(ctx, &existing); err != nil {
		return ctrl.Result{}, err
	}
	logger.Info("updated VIPBinding from Gateway", "gateway", gateway.GetName(), "binding", bindingKey.Name)
	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

func desiredBinding(gateway *unstructured.Unstructured, serviceName string, mode easylv1alpha1.VIPMode, vips []string, poolRef string) *easylv1alpha1.VIPBinding {
	binding := &easylv1alpha1.VIPBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gatewayBindingName(gateway.GetName()),
			Namespace: gateway.GetNamespace(),
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "easylb",
				"easylb.io/gateway":            gateway.GetName(),
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         gatewayGVK.GroupVersion().String(),
				Kind:               gatewayGVK.Kind,
				Name:               gateway.GetName(),
				UID:                gateway.GetUID(),
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(false),
			}},
		},
		Spec: easylv1alpha1.VIPBindingSpec{
			ServiceRef: easylv1alpha1.ServiceReference{Name: serviceName},
			Mode:       mode,
			VIPs:       vips,
		},
	}
	if poolRef != "" {
		binding.Spec.PoolRef = &easylv1alpha1.PoolReference{Name: poolRef}
	}
	return binding
}

func (r *GatewayReconciler) deleteOwnedBinding(ctx context.Context, gateway *unstructured.Unstructured, key types.NamespacedName) error {
	var binding easylv1alpha1.VIPBinding
	if err := r.Get(ctx, key, &binding); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !ownedByGateway(&binding, gateway) {
		return nil
	}
	return client.IgnoreNotFound(r.Delete(ctx, &binding))
}

func ownedByGateway(binding *easylv1alpha1.VIPBinding, gateway *unstructured.Unstructured) bool {
	for _, ref := range binding.OwnerReferences {
		if ref.UID != "" && gateway.GetUID() != "" {
			if ref.UID == gateway.GetUID() {
				return true
			}
			continue
		}
		if ref.APIVersion == gatewayGVK.GroupVersion().String() &&
			ref.Kind == gatewayGVK.Kind &&
			ref.Name == gateway.GetName() {
			return true
		}
	}
	return false
}

func gatewayMode(annotations map[string]string) (easylv1alpha1.VIPMode, error) {
	switch strings.ToLower(strings.TrimSpace(annotations[gatewayModeAnnotation])) {
	case "", string(easylv1alpha1.VIPModeExternal):
		return easylv1alpha1.VIPModeExternal, nil
	case string(easylv1alpha1.VIPModeL2):
		return easylv1alpha1.VIPModeL2, nil
	case string(easylv1alpha1.VIPModeBGP):
		return easylv1alpha1.VIPModeBGP, nil
	default:
		return "", fmt.Errorf("unsupported %s value %q (supported: external, l2, bgp)",
			gatewayModeAnnotation, annotations[gatewayModeAnnotation])
	}
}

func gatewayAddresses(gateway *unstructured.Unstructured, annotations map[string]string) (vips, invalid []string) {
	if addresses, found, _ := unstructured.NestedSlice(gateway.Object, "spec", "addresses"); found {
		for _, item := range addresses {
			entry, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			addressType, _, _ := unstructured.NestedString(entry, "type")
			if addressType != "" && addressType != "IPAddress" {
				continue
			}
			value, _, _ := unstructured.NestedString(entry, "value")
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, err := netip.ParseAddr(value); err != nil {
				invalid = append(invalid, value)
				continue
			}
			vips = append(vips, value)
		}
	}

	if len(vips) == 0 {
		for _, raw := range strings.Split(annotations[gatewayVIPsAnnotation], ",") {
			value := strings.TrimSpace(raw)
			if value == "" {
				continue
			}
			if _, err := netip.ParseAddr(value); err != nil {
				invalid = append(invalid, value)
				continue
			}
			vips = append(vips, value)
		}
	}
	return dedupeStrings(vips), invalid
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func gatewayBindingName(name string) string {
	prefix := gatewayBindingPrefix
	maxLen := 253 - len(prefix)
	if len(name) > maxLen {
		name = strings.TrimRight(name[:maxLen], "-")
	}
	return prefix + name
}

func (r *GatewayReconciler) event(obj runtime.Object, eventType, reason, message string) {
	if r.Recorder != nil {
		r.Recorder.Event(obj, eventType, reason, message)
	}
}

// SetupWithManager registers the optional Gateway integration with the manager.
func (r *GatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	gateway := &unstructured.Unstructured{}
	gateway.SetGroupVersionKind(gatewayGVK)
	return ctrl.NewControllerManagedBy(mgr).
		For(gateway).
		Owns(&easylv1alpha1.VIPBinding{}).
		Complete(r)
}

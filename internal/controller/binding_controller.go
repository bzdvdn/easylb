package controller

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	easylv1alpha1 "github.com/bzdvdn/easylb/api/v1alpha1"
	"github.com/bzdvdn/easylb/internal/ipam"
	"github.com/bzdvdn/easylb/internal/metrics"
)

const conditionReady = "Ready"

// VIPBindingReconciler allocates VIPs for VIPBinding objects.
type VIPBindingReconciler struct {
	client.Client
	RequeueAfter time.Duration
}

// Reconcile implements reconcile.Reconciler.
func (r *VIPBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var binding easylv1alpha1.VIPBinding
	if err := r.Get(ctx, req.NamespacedName, &binding); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	original := binding.DeepCopy()
	allocated, condition := r.resolve(ctx, &binding)
	binding.Status.AllocatedIPs = allocated
	binding.Status.ObservedGeneration = binding.Generation
	apimeta.SetStatusCondition(&binding.Status.Conditions, condition)

	if equality.Semantic.DeepEqual(original.Status, binding.Status) {
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}
	if err := r.Status().Patch(ctx, &binding, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
		return ctrl.Result{}, err
	}
	result := "success"
	if condition.Status != metav1.ConditionTrue {
		result = "failure"
	}
	metrics.VIPAllocations.WithLabelValues(result, condition.Reason).Inc()
	logger.Info("updated VIPBinding status",
		"allocated", allocated,
		"ready", condition.Status == metav1.ConditionTrue,
		"reason", condition.Reason,
	)
	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

func (r *VIPBindingReconciler) resolve(ctx context.Context, binding *easylv1alpha1.VIPBinding) ([]string, metav1.Condition) {
	if binding.Spec.ServiceRef.Name == "" {
		return nil, readyCondition(binding, metav1.ConditionFalse, "InvalidSpec", "spec.serviceRef.name is required")
	}
	if len(binding.Spec.VIPs) > 0 && binding.Spec.PoolRef != nil {
		return nil, readyCondition(binding, metav1.ConditionFalse, "InvalidSpec", "spec.vips and spec.poolRef are mutually exclusive")
	}
	if len(binding.Spec.VIPs) == 0 && binding.Spec.PoolRef == nil {
		return nil, readyCondition(binding, metav1.ConditionFalse, "InvalidSpec", "either spec.vips or spec.poolRef is required")
	}
	if binding.Spec.RequestedIP != "" && binding.Spec.PoolRef == nil {
		return nil, readyCondition(binding, metav1.ConditionFalse, "InvalidSpec", "spec.requestedIP requires spec.poolRef")
	}

	if len(binding.Spec.VIPs) > 0 {
		return r.resolveExplicit(ctx, binding)
	}
	return r.resolvePool(ctx, binding)
}

func (r *VIPBindingReconciler) resolveExplicit(ctx context.Context, binding *easylv1alpha1.VIPBinding) ([]string, metav1.Condition) {
	ips, err := canonicalIPs(binding.Spec.VIPs)
	if err != nil {
		return nil, readyCondition(binding, metav1.ConditionFalse, "InvalidVIP", err.Error())
	}

	used, err := r.usedAddresses(ctx, binding)
	if err != nil {
		return nil, readyCondition(binding, metav1.ConditionFalse, "AllocationError", err.Error())
	}
	for _, ip := range ips {
		if _, ok := used[ip]; ok {
			return nil, readyCondition(binding, metav1.ConditionFalse, "Conflict",
				fmt.Sprintf("VIP %s is already claimed by another VIPBinding", ip))
		}
	}

	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out, readyCondition(binding, metav1.ConditionTrue, "Explicit", "explicit VIPs are configured")
}

func (r *VIPBindingReconciler) resolvePool(ctx context.Context, binding *easylv1alpha1.VIPBinding) ([]string, metav1.Condition) {
	var pool easylv1alpha1.VIPPool
	if err := r.Get(ctx, types.NamespacedName{Name: binding.Spec.PoolRef.Name}, &pool); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Keep the previous allocation during a transient pool outage.
			return binding.Status.AllocatedIPs, readyCondition(binding, metav1.ConditionFalse, "PoolNotFound",
				fmt.Sprintf("VIPPool %q not found", binding.Spec.PoolRef.Name))
		}
		return nil, readyCondition(binding, metav1.ConditionFalse, "AllocationError", err.Error())
	}

	poolAddresses, err := ipam.Expand(pool.Spec.Addresses)
	if err != nil {
		return binding.Status.AllocatedIPs, readyCondition(binding, metav1.ConditionFalse, "InvalidPool", err.Error())
	}
	poolAddresses = usablePoolAddresses(&pool, poolAddresses)

	used, err := r.usedAddresses(ctx, binding)
	if err != nil {
		return nil, readyCondition(binding, metav1.ConditionFalse, "AllocationError", err.Error())
	}

	// Keep the current allocation when it is still valid. When requestedIP is
	// set, only keep it if it already matches the request.
	if binding.Spec.RequestedIP == "" {
		if current, ok := currentAllocation(binding, poolAddresses, used); ok {
			return []string{current.String()}, readyCondition(binding, metav1.ConditionTrue, "Allocated", "address allocated from pool")
		}
	}

	if binding.Spec.RequestedIP != "" {
		requested, err := parseIP(binding.Spec.RequestedIP)
		if err != nil {
			return nil, readyCondition(binding, metav1.ConditionFalse, "InvalidVIP", err.Error())
		}
		if !containsAddress(poolAddresses, requested) {
			return nil, readyCondition(binding, metav1.ConditionFalse, "RequestedIPUnavailable",
				fmt.Sprintf("requested IP %s is not in pool %q", requested, pool.Name))
		}
		if _, ok := used[requested]; ok {
			return nil, readyCondition(binding, metav1.ConditionFalse, "RequestedIPUnavailable",
				fmt.Sprintf("requested IP %s is already in use", requested))
		}
		return []string{requested.String()}, readyCondition(binding, metav1.ConditionTrue, "Allocated", "requested address allocated from pool")
	}

	if !pool.AutoAssignEnabled() {
		return nil, readyCondition(binding, metav1.ConditionFalse, "PoolAutoAssignDisabled",
			fmt.Sprintf("VIPPool %q has autoAssign disabled and no requestedIP was set", pool.Name))
	}

	for _, addr := range poolAddresses {
		if _, ok := used[addr]; ok {
			continue
		}
		return []string{addr.String()}, readyCondition(binding, metav1.ConditionTrue, "Allocated", "address allocated from pool")
	}
	return nil, readyCondition(binding, metav1.ConditionFalse, "PoolExhausted",
		fmt.Sprintf("VIPPool %q has no free addresses", pool.Name))
}

func (r *VIPBindingReconciler) usedAddresses(ctx context.Context, current *easylv1alpha1.VIPBinding) (map[netip.Addr]struct{}, error) {
	var list easylv1alpha1.VIPBindingList
	if err := r.List(ctx, &list); err != nil {
		return nil, err
	}
	used := make(map[netip.Addr]struct{})
	for i := range list.Items {
		binding := &list.Items[i]
		if binding.Namespace == current.Namespace && binding.Name == current.Name {
			continue
		}
		ips := binding.Spec.VIPs
		if len(ips) == 0 {
			ips = binding.Status.AllocatedIPs
		}
		if len(ips) == 0 {
			continue
		}
		shareable := r.canShare(ctx, current, binding)
		otherHasPriority := bindingBefore(binding, types.NamespacedName{
			Namespace: current.Namespace,
			Name:      current.Name,
		})
		for _, raw := range ips {
			if addr, err := parseIP(raw); err == nil && !shareable && otherHasPriority {
				used[addr] = struct{}{}
			}
		}
	}
	return used, nil
}

func (r *VIPBindingReconciler) canShare(ctx context.Context, a, b *easylv1alpha1.VIPBinding) bool {
	if a.Spec.SharingKey == "" || a.Spec.SharingKey != b.Spec.SharingKey {
		return false
	}
	aSvc, err := r.bindingService(ctx, a)
	if err != nil || aSvc == nil {
		return false
	}
	bSvc, err := r.bindingService(ctx, b)
	if err != nil || bSvc == nil {
		return false
	}
	return !portsConflict(aSvc, bSvc)
}

func (r *VIPBindingReconciler) bindingService(ctx context.Context, binding *easylv1alpha1.VIPBinding) (*corev1.Service, error) {
	if binding.Spec.ServiceRef.Name == "" {
		return nil, nil
	}
	var svc corev1.Service
	if err := r.Get(ctx, types.NamespacedName{Namespace: binding.Namespace, Name: binding.Spec.ServiceRef.Name}, &svc); err != nil {
		return nil, err
	}
	return &svc, nil
}

func currentAllocation(binding *easylv1alpha1.VIPBinding, pool []netip.Addr, used map[netip.Addr]struct{}) (netip.Addr, bool) {
	if len(binding.Status.AllocatedIPs) != 1 {
		return netip.Addr{}, false
	}
	addr, err := parseIP(binding.Status.AllocatedIPs[0])
	if err != nil || !containsAddress(pool, addr) {
		return netip.Addr{}, false
	}
	if _, ok := used[addr]; ok {
		return netip.Addr{}, false
	}
	return addr, true
}

func canonicalIPs(raw []string) ([]netip.Addr, error) {
	out := make([]netip.Addr, 0, len(raw))
	seen := make(map[netip.Addr]struct{})
	for _, value := range raw {
		addr, err := parseIP(value)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		out = append(out, addr)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid VIPs configured")
	}
	return out, nil
}

func parseIP(value string) (netip.Addr, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("invalid IP %q: %w", value, err)
	}
	if addr.Zone() != "" {
		return netip.Addr{}, fmt.Errorf("invalid IP %q: zones are not supported", value)
	}
	return addr.Unmap(), nil
}

func containsAddress(addresses []netip.Addr, target netip.Addr) bool {
	for _, addr := range addresses {
		if addr == target {
			return true
		}
	}
	return false
}

func usablePoolAddresses(pool *easylv1alpha1.VIPPool, addresses []netip.Addr) []netip.Addr {
	if !pool.Spec.AvoidBuggyIPs {
		return addresses
	}
	return ipam.FilterBuggyIPv4(addresses)
}

func readyCondition(binding *easylv1alpha1.VIPBinding, status metav1.ConditionStatus, reason, message string) metav1.Condition {
	return metav1.Condition{
		Type:               conditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: binding.Generation,
	}
}

// SetupWithManager registers the controller with the manager.
func (r *VIPBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&easylv1alpha1.VIPBinding{}).
		Watches(&easylv1alpha1.VIPPool{}, handler.EnqueueRequestsFromMapFunc(r.bindingsForPool)).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(r)
}

func (r *VIPBindingReconciler) bindingsForPool(ctx context.Context, obj client.Object) []reconcile.Request {
	pool, ok := obj.(*easylv1alpha1.VIPPool)
	if !ok {
		return nil
	}
	var list easylv1alpha1.VIPBindingList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	var requests []reconcile.Request
	for i := range list.Items {
		binding := &list.Items[i]
		if binding.Spec.PoolRef != nil && binding.Spec.PoolRef.Name == pool.Name {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(binding),
			})
		}
	}
	return requests
}

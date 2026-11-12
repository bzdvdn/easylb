package controller

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	easylv1alpha1 "github.com/bzdvdn/easylb/api/v1alpha1"
	"github.com/bzdvdn/easylb/internal/filter"
	"github.com/bzdvdn/easylb/internal/ipam"
	"github.com/bzdvdn/easylb/internal/metrics"
)

const (
	autoBindingPrefix = "auto-"
	autoBindingLabel  = "easylb.io/auto-binding"
	autoBindingPool   = "easylb.io/pool"

	autoAssignAnnotation = "easylb.io/auto-assign"

	metalLBPoolAnnotation           = "metallb.io/address-pool"
	metalLBPoolAnnotationLegacy     = "metallb.universe.tf/address-pool"
	metalLBIPsAnnotation            = "metallb.io/loadBalancerIPs"
	metalLBIPsAnnotationLegacy      = "metallb.universe.tf/loadBalancerIPs"
	metalLBSharedIPAnnotation       = "metallb.io/allow-shared-ip"
	metalLBSharedIPAnnotationLegacy = "metallb.universe.tf/allow-shared-ip"
)

// ServiceAllocationReconciler creates VIPBinding objects for LoadBalancer
// Services that do not have one yet. This is the MetalLB-style allocation path:
// a matching VIPPool allocates an address automatically.
type ServiceAllocationReconciler struct {
	client.Client
	Recorder     record.EventRecorder
	LBClass      string
	RequireClass bool
	RequeueAfter time.Duration
}

// Reconcile implements reconcile.Reconciler.
func (r *ServiceAllocationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var svc corev1.Service
	if err := r.Get(ctx, req.NamespacedName, &svc); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return ctrl.Result{}, r.deleteAutoBinding(ctx, req.Namespace, autoBindingName(req.Name))
		}
		return ctrl.Result{}, err
	}

	autoKey := types.NamespacedName{
		Namespace: svc.Namespace,
		Name:      autoBindingName(svc.Name),
	}

	var existing easylv1alpha1.VIPBinding
	existingFound := true
	if err := r.Get(ctx, autoKey, &existing); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		existingFound = false
	}

	if !svc.DeletionTimestamp.IsZero() ||
		!filter.Manages(&svc, filter.Options{LBClass: r.LBClass, RequireClass: r.RequireClass}) ||
		svc.Annotations[autoAssignAnnotation] == "false" {
		return ctrl.Result{}, r.deleteAutoBindingIfOwned(ctx, &svc, autoKey)
	}

	others, err := r.bindingsForService(ctx, &svc)
	if err != nil {
		return ctrl.Result{}, err
	}
	for i := range others {
		if others[i].Name == autoKey.Name && isAutoBinding(&others[i]) {
			continue
		}
		// A manual or Gateway-created binding wins over auto-allocation.
		return ctrl.Result{}, r.deleteAutoBindingIfOwned(ctx, &svc, autoKey)
	}

	requested, err := requestedIPs(&svc)
	if err != nil {
		r.event(&svc, corev1.EventTypeWarning, "InvalidRequestedIP", err.Error())
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	sharingKey := serviceSharingKey(&svc)
	shared, err := r.findSharedAllocation(ctx, &svc, sharingKey, autoKey)
	if err != nil {
		return ctrl.Result{}, err
	}

	var pool *easylv1alpha1.VIPPool
	var message string
	if len(requested) > 0 || shared == nil {
		pool, message, err = r.selectPool(ctx, &svc, requested)
		if err != nil {
			return ctrl.Result{}, err
		}
		if pool == nil && len(requested) == 0 {
			if message != "" {
				r.event(&svc, corev1.EventTypeWarning, "NoMatchingPool", message)
			}
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
		}
	}

	desired := desiredAutoBinding(&svc, autoKey.Name, pool, requested, shared)

	if !existingFound {
		if err := r.Create(ctx, desired); err != nil {
			return ctrl.Result{}, err
		}
		metrics.AutoBindings.WithLabelValues("created").Inc()
		r.event(&svc, corev1.EventTypeNormal, "AutoBindingCreated",
			fmt.Sprintf("created VIPBinding %s", desired.Name))
		logger.Info("created VIPBinding for Service", "service", svc.Name, "binding", desired.Name)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	if !isAutoBinding(&existing) {
		r.event(&svc, corev1.EventTypeWarning, "BindingConflict",
			fmt.Sprintf("VIPBinding %s already exists and is not managed by auto-allocation", autoKey.Name))
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	mergedLabels := mergeLabels(existing.Labels, desired.Labels)
	if equality.Semantic.DeepEqual(existing.Spec, desired.Spec) &&
		equality.Semantic.DeepEqual(existing.Labels, mergedLabels) &&
		equality.Semantic.DeepEqual(existing.OwnerReferences, desired.OwnerReferences) {
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	existing.Spec = desired.Spec
	existing.Labels = mergedLabels
	existing.OwnerReferences = desired.OwnerReferences
	if err := r.Update(ctx, &existing); err != nil {
		return ctrl.Result{}, err
	}
	metrics.AutoBindings.WithLabelValues("updated").Inc()
	r.event(&svc, corev1.EventTypeNormal, "AutoBindingUpdated",
		fmt.Sprintf("updated VIPBinding %s", existing.Name))
	logger.Info("updated VIPBinding for Service", "service", svc.Name, "binding", existing.Name)
	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

func (r *ServiceAllocationReconciler) bindingsForService(ctx context.Context, svc *corev1.Service) ([]easylv1alpha1.VIPBinding, error) {
	var list easylv1alpha1.VIPBindingList
	if err := r.List(ctx, &list, client.InNamespace(svc.Namespace)); err != nil {
		return nil, err
	}
	out := make([]easylv1alpha1.VIPBinding, 0, len(list.Items))
	for i := range list.Items {
		if list.Items[i].Spec.ServiceRef.Name == svc.Name {
			out = append(out, list.Items[i])
		}
	}
	return out, nil
}

func (r *ServiceAllocationReconciler) selectPool(ctx context.Context, svc *corev1.Service, requested []string) (*easylv1alpha1.VIPPool, string, error) {
	if name := poolAnnotation(svc); name != "" {
		var pool easylv1alpha1.VIPPool
		if err := r.Get(ctx, types.NamespacedName{Name: name}, &pool); err != nil {
			if client.IgnoreNotFound(err) == nil {
				return nil, fmt.Sprintf("VIPPool %q not found", name), nil
			}
			return nil, "", err
		}
		return &pool, "", nil
	}

	var list easylv1alpha1.VIPPoolList
	if err := r.List(ctx, &list); err != nil {
		return nil, "", err
	}
	sort.Slice(list.Items, func(i, j int) bool {
		return list.Items[i].Name < list.Items[j].Name
	})

	var fallback *easylv1alpha1.VIPPool
	var exhausted *easylv1alpha1.VIPPool
	for i := range list.Items {
		pool := &list.Items[i]
		if !pool.AutoAssignEnabled() {
			continue
		}
		matches, err := poolMatchesService(ctx, r.Client, pool, svc)
		if err != nil {
			return nil, "", err
		}
		if !matches {
			continue
		}
		if exhausted == nil {
			exhausted = pool
		}
		if len(requested) == 1 && poolContainsIP(pool, requested[0]) {
			return pool, "", nil
		}
		free, err := r.poolHasFreeAddress(ctx, pool, types.NamespacedName{
			Namespace: svc.Namespace,
			Name:      autoBindingName(svc.Name),
		})
		if err != nil || !free {
			continue
		}
		if fallback == nil {
			fallback = pool
		}
	}
	if fallback != nil {
		return fallback, "", nil
	}
	if exhausted != nil {
		// Return an exhausted pool so the binding reports PoolExhausted
		// instead of silently staying unallocated.
		return exhausted, "", nil
	}
	return nil, "no VIPPool matches this Service", nil
}

func (r *ServiceAllocationReconciler) poolHasFreeAddress(ctx context.Context, pool *easylv1alpha1.VIPPool, exclude types.NamespacedName) (bool, error) {
	addresses, err := ipam.Expand(pool.Spec.Addresses)
	if err != nil {
		return false, err
	}
	addresses = usablePoolAddresses(pool, addresses)
	used, err := r.usedAddresses(ctx, exclude)
	if err != nil {
		return false, err
	}
	for _, addr := range addresses {
		if _, ok := used[addr]; !ok {
			return true, nil
		}
	}
	return false, nil
}

func (r *ServiceAllocationReconciler) usedAddresses(ctx context.Context, exclude types.NamespacedName) (map[netip.Addr]struct{}, error) {
	var list easylv1alpha1.VIPBindingList
	if err := r.List(ctx, &list); err != nil {
		return nil, err
	}
	used := make(map[netip.Addr]struct{})
	for i := range list.Items {
		binding := &list.Items[i]
		if binding.Namespace == exclude.Namespace && binding.Name == exclude.Name {
			continue
		}
		ips := binding.Status.AllocatedIPs
		for _, raw := range ips {
			if addr, err := parseIP(raw); err == nil {
				used[addr] = struct{}{}
			}
		}
	}
	return used, nil
}

type sharedAllocation struct {
	ip       string
	poolName string
	mode     easylv1alpha1.VIPMode
}

func (r *ServiceAllocationReconciler) findSharedAllocation(ctx context.Context, svc *corev1.Service, key string, self types.NamespacedName) (*sharedAllocation, error) {
	if key == "" {
		return nil, nil
	}
	var list easylv1alpha1.VIPBindingList
	if err := r.List(ctx, &list); err != nil {
		return nil, err
	}
	sort.Slice(list.Items, func(i, j int) bool {
		if list.Items[i].Namespace == list.Items[j].Namespace {
			return list.Items[i].Name < list.Items[j].Name
		}
		return list.Items[i].Namespace < list.Items[j].Namespace
	})

	for i := range list.Items {
		binding := &list.Items[i]
		if binding.Spec.SharingKey != key {
			continue
		}
		if binding.Namespace == self.Namespace && binding.Name == self.Name {
			continue
		}
		if !bindingBefore(binding, self) {
			continue
		}
		ips := binding.Status.AllocatedIPs
		if len(ips) == 0 {
			continue
		}
		otherSvc, err := r.bindingService(ctx, binding)
		if err != nil || otherSvc == nil || portsConflict(svc, otherSvc) {
			continue
		}
		ip := ips[0]
		if _, err := netip.ParseAddr(ip); err != nil {
			continue
		}
		out := &sharedAllocation{ip: ip, mode: binding.Spec.Mode}
		if out.mode == "" {
			out.mode = easylv1alpha1.VIPModeL2
		}
		if binding.Spec.PoolRef != nil {
			var pool easylv1alpha1.VIPPool
			if err := r.Get(ctx, types.NamespacedName{Name: binding.Spec.PoolRef.Name}, &pool); err == nil &&
				poolContainsIP(&pool, ip) {
				out.poolName = pool.Name
			}
		}
		return out, nil
	}
	return nil, nil
}

func bindingBefore(binding *easylv1alpha1.VIPBinding, key types.NamespacedName) bool {
	if binding.Namespace == key.Namespace {
		return binding.Name < key.Name
	}
	return binding.Namespace < key.Namespace
}

func (r *ServiceAllocationReconciler) bindingService(ctx context.Context, binding *easylv1alpha1.VIPBinding) (*corev1.Service, error) {
	if binding.Spec.ServiceRef.Name == "" {
		return nil, nil
	}
	var svc corev1.Service
	if err := r.Get(ctx, types.NamespacedName{Namespace: binding.Namespace, Name: binding.Spec.ServiceRef.Name}, &svc); err != nil {
		return nil, err
	}
	return &svc, nil
}

func portsConflict(a, b *corev1.Service) bool {
	for _, pa := range a.Spec.Ports {
		for _, pb := range b.Spec.Ports {
			if pa.Port == pb.Port && serviceProtocol(pa.Protocol) == serviceProtocol(pb.Protocol) {
				return true
			}
		}
	}
	return false
}

func serviceProtocol(protocol corev1.Protocol) corev1.Protocol {
	if protocol == "" {
		return corev1.ProtocolTCP
	}
	return protocol
}

func serviceSharingKey(svc *corev1.Service) string {
	return firstNonEmpty(
		svc.Annotations[metalLBSharedIPAnnotation],
		svc.Annotations[metalLBSharedIPAnnotationLegacy],
	)
}

func poolMatchesService(ctx context.Context, c client.Client, pool *easylv1alpha1.VIPPool, svc *corev1.Service) (bool, error) {
	allocation := pool.Spec.ServiceAllocation
	if allocation == nil {
		return true, nil
	}

	if allocation.NamespaceSelector != nil {
		var ns corev1.Namespace
		if err := c.Get(ctx, types.NamespacedName{Name: svc.Namespace}, &ns); err != nil {
			return false, err
		}
		selector, err := metav1.LabelSelectorAsSelector(allocation.NamespaceSelector)
		if err != nil {
			return false, fmt.Errorf("VIPPool %q has an invalid namespaceSelector: %w", pool.Name, err)
		}
		if !selector.Matches(labels.Set(ns.Labels)) {
			return false, nil
		}
	}

	if allocation.ServiceSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(allocation.ServiceSelector)
		if err != nil {
			return false, fmt.Errorf("VIPPool %q has an invalid serviceSelector: %w", pool.Name, err)
		}
		if !selector.Matches(labels.Set(svc.Labels)) {
			return false, nil
		}
	}

	return true, nil
}

func poolContainsIP(pool *easylv1alpha1.VIPPool, raw string) bool {
	target, err := netip.ParseAddr(raw)
	if err != nil {
		return false
	}
	target = target.Unmap()
	addresses, err := ipam.Expand(pool.Spec.Addresses)
	if err != nil {
		return false
	}
	for _, addr := range addresses {
		if addr == target {
			return true
		}
	}
	return false
}

func requestedIPs(svc *corev1.Service) ([]string, error) {
	raw := firstNonEmpty(
		svc.Annotations[metalLBIPsAnnotation],
		svc.Annotations[metalLBIPsAnnotationLegacy],
	)
	if raw == "" {
		raw = svc.Spec.LoadBalancerIP
	}

	var out []string
	seen := make(map[netip.Addr]struct{})
	for _, part := range strings.Split(raw, ",") {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		addr, err := netip.ParseAddr(value)
		if err != nil {
			return nil, fmt.Errorf("invalid requested IP %q: %w", value, err)
		}
		addr = addr.Unmap()
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		out = append(out, addr.String())
	}
	return out, nil
}

func poolAnnotation(svc *corev1.Service) string {
	return firstNonEmpty(
		svc.Annotations[metalLBPoolAnnotation],
		svc.Annotations[metalLBPoolAnnotationLegacy],
	)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func desiredAutoBinding(svc *corev1.Service, name string, pool *easylv1alpha1.VIPPool, requested []string, shared *sharedAllocation) *easylv1alpha1.VIPBinding {
	labels := map[string]string{
		"app.kubernetes.io/managed-by": "easylb",
		autoBindingLabel:               "true",
	}
	mode := easylv1alpha1.VIPModeL2
	if pool != nil {
		labels[autoBindingPool] = pool.Name
		mode = pool.ModeOrDefault()
	}

	spec := easylv1alpha1.VIPBindingSpec{
		ServiceRef: easylv1alpha1.ServiceReference{Name: svc.Name},
		Mode:       mode,
		SharingKey: serviceSharingKey(svc),
	}
	switch {
	case len(requested) > 0:
		if pool != nil && len(requested) == 1 && poolContainsIP(pool, requested[0]) {
			spec.PoolRef = &easylv1alpha1.PoolReference{Name: pool.Name}
			spec.RequestedIP = requested[0]
		} else {
			spec.VIPs = append([]string(nil), requested...)
		}
	case shared != nil:
		if shared.mode != "" {
			spec.Mode = shared.mode
		}
		if shared.poolName != "" {
			spec.PoolRef = &easylv1alpha1.PoolReference{Name: shared.poolName}
			spec.RequestedIP = shared.ip
			labels[autoBindingPool] = shared.poolName
		} else {
			spec.VIPs = []string{shared.ip}
		}
	case pool != nil:
		spec.PoolRef = &easylv1alpha1.PoolReference{Name: pool.Name}
	default:
		spec.VIPs = append([]string(nil), requested...)
	}

	return &easylv1alpha1.VIPBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: svc.Namespace,
			Labels:    labels,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         "v1",
				Kind:               "Service",
				Name:               svc.Name,
				UID:                svc.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(false),
			}},
		},
		Spec: spec,
	}
}

func mergeLabels(current, desired map[string]string) map[string]string {
	merged := make(map[string]string, len(current)+len(desired))
	for key, value := range current {
		merged[key] = value
	}
	for key, value := range desired {
		merged[key] = value
	}
	return merged
}

func autoBindingName(serviceName string) string {
	const prefix = autoBindingPrefix
	maxLen := 253 - len(prefix)
	if len(serviceName) > maxLen {
		serviceName = strings.TrimRight(serviceName[:maxLen], "-")
	}
	return prefix + serviceName
}

func isAutoBinding(binding *easylv1alpha1.VIPBinding) bool {
	return binding.Labels[autoBindingLabel] == "true"
}

func ownedByService(binding *easylv1alpha1.VIPBinding, svc *corev1.Service) bool {
	for _, ref := range binding.OwnerReferences {
		if ref.Kind == "Service" && ref.Name == svc.Name {
			return true
		}
	}
	return len(binding.OwnerReferences) == 0
}

func (r *ServiceAllocationReconciler) deleteAutoBinding(ctx context.Context, namespace, name string) error {
	var binding easylv1alpha1.VIPBinding
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &binding); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !isAutoBinding(&binding) {
		return nil
	}
	if err := r.Delete(ctx, &binding); err != nil {
		return client.IgnoreNotFound(err)
	}
	metrics.AutoBindings.WithLabelValues("deleted").Inc()
	return nil
}

func (r *ServiceAllocationReconciler) deleteAutoBindingIfOwned(ctx context.Context, svc *corev1.Service, key types.NamespacedName) error {
	var binding easylv1alpha1.VIPBinding
	if err := r.Get(ctx, key, &binding); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !isAutoBinding(&binding) || !ownedByService(&binding, svc) {
		return nil
	}
	if err := r.Delete(ctx, &binding); err != nil {
		return client.IgnoreNotFound(err)
	}
	metrics.AutoBindings.WithLabelValues("deleted").Inc()
	r.event(svc, corev1.EventTypeNormal, "AutoBindingDeleted",
		fmt.Sprintf("deleted VIPBinding %s", binding.Name))
	return nil
}

func (r *ServiceAllocationReconciler) event(svc *corev1.Service, eventType, reason, message string) {
	if r.Recorder != nil {
		r.Recorder.Event(svc, eventType, reason, message)
	}
}

// SetupWithManager registers the controller with the manager.
func (r *ServiceAllocationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("service-allocation").
		For(&corev1.Service{}).
		Watches(&easylv1alpha1.VIPPool{},
			handler.EnqueueRequestsFromMapFunc(r.servicesForPool)).
		Watches(&easylv1alpha1.VIPBinding{},
			handler.EnqueueRequestsFromMapFunc(r.serviceForBinding)).
		Watches(&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(r.servicesForNamespace)).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(r)
}

func (r *ServiceAllocationReconciler) servicesForPool(ctx context.Context, _ client.Object) []reconcile.Request {
	var services corev1.ServiceList
	if err := r.List(ctx, &services); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0, len(services.Items))
	for i := range services.Items {
		svc := &services.Items[i]
		if filter.Manages(svc, filter.Options{LBClass: r.LBClass, RequireClass: r.RequireClass}) {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(svc)})
		}
	}
	return requests
}

func (r *ServiceAllocationReconciler) servicesForNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		return nil
	}
	var services corev1.ServiceList
	if err := r.List(ctx, &services, client.InNamespace(ns.Name)); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0, len(services.Items))
	for i := range services.Items {
		svc := &services.Items[i]
		if filter.Manages(svc, filter.Options{LBClass: r.LBClass, RequireClass: r.RequireClass}) {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(svc)})
		}
	}
	return requests
}

func (r *ServiceAllocationReconciler) serviceForBinding(_ context.Context, obj client.Object) []reconcile.Request {
	binding, ok := obj.(*easylv1alpha1.VIPBinding)
	if !ok || binding.Spec.ServiceRef.Name == "" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Namespace: binding.Namespace,
			Name:      binding.Spec.ServiceRef.Name,
		},
	}}
}

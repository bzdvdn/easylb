// Package speaker advertises l2 VIPs with ARP and per-VIP Leases.
package speaker

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	easylv1alpha1 "github.com/bzdvdn/easylb/api/v1alpha1"
	"github.com/bzdvdn/easylb/internal/filter"
)

// Reconciler is the per-node speaker. It only advertises VIPs with mode=l2;
// external VIPs are left untouched.
type Reconciler struct {
	client.Client
	KubeClient kubernetes.Interface
	Recorder   record.EventRecorder

	// Ctx is the process lifetime context. Leader election runners are tied to
	// it so that a graceful shutdown releases the per-VIP Leases immediately
	// instead of waiting for leaseDuration.
	Ctx context.Context

	NodeName       string
	PodName        string
	LBClass        string
	RequireClass   bool
	InterfaceName  string
	NodeIPType     corev1.NodeAddressType
	LeaseNamespace string
	LeaseDuration  time.Duration
	RenewDeadline  time.Duration
	RetryPeriod    time.Duration
	RequeueAfter   time.Duration

	mu      sync.Mutex
	runners map[types.NamespacedName]*runner
}

// Reconcile implements reconcile.Reconciler.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	r.mu.Lock()
	if r.runners == nil {
		r.runners = make(map[types.NamespacedName]*runner)
	}
	r.mu.Unlock()

	var binding easylv1alpha1.VIPBinding
	if err := r.Get(ctx, req.NamespacedName, &binding); err != nil {
		if client.IgnoreNotFound(err) == nil {
			r.stop(req.NamespacedName, true)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if binding.Spec.Mode != easylv1alpha1.VIPModeL2 {
		r.stop(req.NamespacedName, false)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	var svc corev1.Service
	serviceKey := types.NamespacedName{Namespace: binding.Namespace, Name: binding.Spec.ServiceRef.Name}
	if err := r.Get(ctx, serviceKey, &svc); err != nil {
		if client.IgnoreNotFound(err) == nil {
			r.stop(req.NamespacedName, false)
			return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
		}
		return ctrl.Result{}, err
	}
	if !filter.Manages(&svc, filter.Options{LBClass: r.LBClass, RequireClass: r.RequireClass}) {
		r.stop(req.NamespacedName, false)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	// Advertise only addresses validated by the VIPBinding controller.
	rawVIPs := binding.Status.AllocatedIPs
	addresses := make([]netip.Addr, 0, len(rawVIPs))
	for _, raw := range rawVIPs {
		addr, err := netip.ParseAddr(raw)
		if err != nil {
			logger.V(1).Info("skipping invalid VIP", "vip", raw, "error", err.Error())
			continue
		}
		addresses = append(addresses, addr.Unmap())
	}
	if len(addresses) == 0 {
		r.stop(req.NamespacedName, false)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	var node corev1.Node
	if err := r.Get(ctx, types.NamespacedName{Name: r.NodeName}, &node); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !nodeReady(&node) {
		r.stop(req.NamespacedName, false)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	var sliceList discoveryv1.EndpointSliceList
	if err := r.List(ctx, &sliceList,
		client.InNamespace(svc.Namespace),
		client.MatchingLabels{discoveryv1.LabelServiceName: svc.Name},
	); err != nil {
		return ctrl.Result{}, err
	}
	slices := make([]*discoveryv1.EndpointSlice, 0, len(sliceList.Items))
	for i := range sliceList.Items {
		slices = append(slices, &sliceList.Items[i])
	}
	if !Eligible(r.NodeName, &svc, &node, slices) {
		r.stop(req.NamespacedName, false)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	iface, err := r.interfaceForNode(&node)
	if err != nil {
		r.event(&binding, corev1.EventTypeWarning, "InterfaceNotFound", err.Error())
		r.stop(req.NamespacedName, false)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	usable := make([]netip.Addr, 0, len(addresses))
	for _, vip := range addresses {
		assigned, err := interfaceHasIP(iface, vip)
		if err != nil {
			logger.V(1).Info("failed to inspect interface addresses", "interface", iface.Name, "error", err.Error())
			continue
		}
		if assigned {
			r.event(&binding, corev1.EventTypeWarning, "VIPAlreadyAssigned",
				fmt.Sprintf("VIP %s is already assigned to %s; refusing to advertise it", vip, iface.Name))
			continue
		}
		usable = append(usable, vip)
	}
	if len(usable) == 0 {
		r.stop(req.NamespacedName, false)
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	}

	r.ensure(req.NamespacedName, usable, iface, logger)
	return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
}

func (r *Reconciler) interfaceForNode(node *corev1.Node) (*net.Interface, error) {
	if r.InterfaceName != "" {
		return net.InterfaceByName(r.InterfaceName)
	}

	nodeIPType := r.NodeIPType
	if nodeIPType == "" {
		nodeIPType = corev1.NodeInternalIP
	}
	for _, addr := range node.Status.Addresses {
		if addr.Type != nodeIPType {
			continue
		}
		parsed, err := netip.ParseAddr(addr.Address)
		if err != nil {
			continue
		}
		return InterfaceForIP(parsed)
	}
	return nil, fmt.Errorf("node %s has no %s address", node.Name, nodeIPType)
}

func (r *Reconciler) identity() string {
	if r.PodName != "" {
		return r.NodeName + "/" + r.PodName
	}
	return r.NodeName
}

func (r *Reconciler) ensure(key types.NamespacedName, vips []netip.Addr, iface *net.Interface, logger logr.Logger) {
	r.mu.Lock()
	existing := r.runners[key]
	if existing != nil && existing.matches(vips, iface) {
		r.mu.Unlock()
		return
	}
	if existing != nil {
		delete(r.runners, key)
	}
	r.mu.Unlock()

	if existing != nil {
		existing.stop()
	}

	parent := r.Ctx
	if parent == nil {
		parent = context.Background()
	}
	newRunner := startRunner(
		parent,
		key,
		vips,
		iface,
		r.KubeClient,
		r.LeaseNamespace,
		r.NodeName,
		r.identity(),
		r.LeaseDuration,
		r.RenewDeadline,
		r.RetryPeriod,
		logger,
	)

	r.mu.Lock()
	r.runners[key] = newRunner
	r.mu.Unlock()
}

func (r *Reconciler) stop(key types.NamespacedName, deleteLease bool) {
	r.mu.Lock()
	existing := r.runners[key]
	delete(r.runners, key)
	r.mu.Unlock()

	if existing != nil {
		existing.stop()
	}
	if deleteLease && r.KubeClient != nil && r.LeaseNamespace != "" {
		_ = r.KubeClient.CoordinationV1().Leases(r.LeaseNamespace).
			Delete(context.Background(), leaseName(key), metav1.DeleteOptions{})
	}
}

func (r *Reconciler) event(binding *easylv1alpha1.VIPBinding, eventType, reason, message string) {
	if r.Recorder != nil {
		r.Recorder.Event(binding, eventType, reason, message)
	}
}

// SetupWithManager registers the speaker with the manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&easylv1alpha1.VIPBinding{}).
		Watches(&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.bindingsForNode),
			builder.WithPredicates(nodeChangedPredicate()),
		).
		Watches(&corev1.Service{},
			handler.EnqueueRequestsFromMapFunc(r.bindingsForService),
		).
		Watches(&discoveryv1.EndpointSlice{},
			handler.EnqueueRequestsFromMapFunc(r.bindingsForEndpointSlice),
		).
		Complete(r)
}

func (r *Reconciler) bindingsForNode(ctx context.Context, _ client.Object) []reconcile.Request {
	var list easylv1alpha1.VIPBindingList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		if list.Items[i].Spec.Mode == easylv1alpha1.VIPModeL2 {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&list.Items[i]),
			})
		}
	}
	return requests
}

func (r *Reconciler) bindingsForService(ctx context.Context, obj client.Object) []reconcile.Request {
	svc, ok := obj.(*corev1.Service)
	if !ok {
		return nil
	}
	return r.bindingsForServiceRef(ctx, svc.Namespace, svc.Name)
}

func (r *Reconciler) bindingsForEndpointSlice(ctx context.Context, obj client.Object) []reconcile.Request {
	slice, ok := obj.(*discoveryv1.EndpointSlice)
	if !ok {
		return nil
	}
	name := slice.Labels[discoveryv1.LabelServiceName]
	if name == "" {
		return nil
	}
	return r.bindingsForServiceRef(ctx, slice.Namespace, name)
}

func (r *Reconciler) bindingsForServiceRef(ctx context.Context, namespace, name string) []reconcile.Request {
	var list easylv1alpha1.VIPBindingList
	if err := r.List(ctx, &list, client.InNamespace(namespace)); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		binding := &list.Items[i]
		if binding.Spec.Mode != easylv1alpha1.VIPModeL2 || binding.Spec.ServiceRef.Name != name {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(binding),
		})
	}
	return requests
}

func nodeChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return true },
		DeleteFunc:  func(event.DeleteEvent) bool { return true },
		GenericFunc: func(event.GenericEvent) bool { return false },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldNode, ok := e.ObjectOld.(*corev1.Node)
			if !ok {
				return true
			}
			newNode, ok := e.ObjectNew.(*corev1.Node)
			if !ok {
				return true
			}
			return !equality.Semantic.DeepEqual(oldNode.Status.Addresses, newNode.Status.Addresses) ||
				nodeReady(oldNode) != nodeReady(newNode)
		},
	}
}

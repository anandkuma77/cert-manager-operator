// FILE: pkg/controller/http01proxy/controller.go
//
// WHAT IT DOES (max 5 lines):
// Main controller reconciliation logic for HTTP01Proxy resources. Implements the
// core reconcile loop that watches for HTTP01Proxy create/update/delete events and
// child resources (DaemonSet, RBAC, NetworkPolicies). Coordinates the workflow:
// add finalizer → validate platform → deploy resources → update status. Handles
// graceful deletion by cleaning up all created resources before removing finalizer.
//
// HOW IT DOES IT (max 5 lines):
// Uses controller-runtime framework's Reconcile pattern. SetupWithManager configures
// watches on HTTP01Proxy and all child resources (triggers reconcile when they change).
// Reconcile() is the entry point called by controller-runtime when events occur.
// Delegates actual work to processReconcileRequest → reconcileHTTP01ProxyDeployment
// (in infrastructure.go/install_http01proxy.go). Updates status based on success/failure.

package http01proxy

import (
	"context"  // For cancellation and timeouts
	"fmt"      // For error formatting
	"os"       // For reading environment variables (proxy image)
	"sync"     // For mutex protecting cached platform info

	appsv1 "k8s.io/api/apps/v1"                // DaemonSet types
	corev1 "k8s.io/api/core/v1"                // ServiceAccount types
	networkingv1 "k8s.io/api/networking/v1"    // NetworkPolicy types
	rbacv1 "k8s.io/api/rbac/v1"                // RBAC types
	"k8s.io/apimachinery/pkg/api/errors"       // For errors.IsNotFound()
	"k8s.io/apimachinery/pkg/types"            // For types.NamespacedName
	"k8s.io/client-go/tools/record"            // For event recording

	ctrl "sigs.k8s.io/controller-runtime"                 // Controller-runtime framework
	"sigs.k8s.io/controller-runtime/pkg/builder"          // For controller builder
	"sigs.k8s.io/controller-runtime/pkg/client"           // Kubernetes client
	"sigs.k8s.io/controller-runtime/pkg/handler"          // Event handlers
	"sigs.k8s.io/controller-runtime/pkg/predicate"        // Watch predicates
	"sigs.k8s.io/controller-runtime/pkg/reconcile"        // Reconcile types

	"github.com/go-logr/logr" // Logging interface

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"   // HTTP01Proxy API types
	"github.com/openshift/cert-manager-operator/pkg/controller/common"            // Shared controller utilities
)

// RequestEnqueueLabelValue is the label value used for filtering reconcile events.
// Child resources (DaemonSet, ServiceAccount, etc.) have label "cert-manager.io/managed-by=cert-manager-http01-proxy"
// When they change, controller only reconciles if this label matches
const RequestEnqueueLabelValue = http01proxyCommonName

// Reconciler reconciles an HTTP01Proxy object.
// Controller-runtime calls Reconcile() on this struct when events occur
type Reconciler struct {
	common.CtrlClient                          // Embedded: Kubernetes client for API calls

	eventRecorder record.EventRecorder         // For recording events shown in `kubectl describe`
	log           logr.Logger                  // Structured logger with controller name prefix

	proxyImage string                          // Container image for proxy (from env var)

	cachedPlatform *platformInfo               // Cached platform detection result (avoids repeated Infrastructure fetches)
	platformMu     sync.Mutex                  // Protects cachedPlatform from concurrent access
}

// +kubebuilder:rbac markers define RBAC permissions needed by this controller
// controller-gen reads these and generates config/rbac/role.yaml
// +kubebuilder:rbac:groups=operator.openshift.io,resources=http01proxies,verbs=get;list;watch;update;patch
// ↑ Controller needs to read HTTP01Proxy objects and update them (add finalizer, update status)
// +kubebuilder:rbac:groups=operator.openshift.io,resources=http01proxies/status,verbs=get;update;patch
// ↑ Separate permission for status subresource (allows updating status without modifying spec)
// +kubebuilder:rbac:groups=operator.openshift.io,resources=http01proxies/finalizers,verbs=update
// ↑ Permission to add/remove finalizers (prevents deletion until cleanup completes)
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// ↑ Create/manage NetworkPolicies for proxy security
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// ↑ Create/manage DaemonSet for proxy pods
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// ↑ Create/manage ServiceAccount for proxy pods (empty group = core/v1)
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
// ↑ Create/manage RBAC for proxy ServiceAccount
// +kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions;ingresses;infrastructures,verbs=get;list;watch
// ↑ Read OpenShift cluster config for platform detection and VIP discovery
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,resourceNames=privileged,verbs=use
// ↑ Bind ServiceAccount to privileged SCC (allows hostNetwork + NET_ADMIN)

// New returns a new Reconciler instance.
// Called once during operator startup by setup_manager.go
func New(mgr ctrl.Manager) (*Reconciler, error) {
	c, err := common.NewClient(mgr)                  // Create Kubernetes client from manager
	if err != nil {
		return nil, err
	}
	return &Reconciler{
		CtrlClient:    c,                            // Kubernetes API client
		eventRecorder: mgr.GetEventRecorderFor(ControllerName), // Event recorder with controller name
		log:           ctrl.Log.WithName(ControllerName),       // Logger with controller name prefix
		proxyImage:    os.Getenv(http01proxyImageNameEnvVarName), // Read proxy image from env var at startup
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
// Configures watches and predicates - called once during operator startup
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	// mapFunc maps child resource events back to the parent HTTP01Proxy object
	// When DaemonSet changes, this function returns {name: "default", namespace: "cert-manager-operator"}
	// so controller reconciles the HTTP01Proxy, not the DaemonSet directly
	mapFunc := func(ctx context.Context, obj client.Object) []reconcile.Request {
		r.log.V(4).Info("received reconcile event", "object", fmt.Sprintf("%T", obj), "name", obj.GetName(), "namespace", obj.GetNamespace())

		objLabels := obj.GetLabels()                 // Get labels from the changed object
		// Check if object has our managed-by label
		if objLabels != nil && objLabels[common.ManagedResourceLabelKey] == RequestEnqueueLabelValue {
			namespace := obj.GetNamespace()
			if namespace == "" {                     // ClusterRole/ClusterRoleBinding have no namespace
				namespace = common.OperatorNamespace // Use operator namespace for lookup
			}
			// Return reconcile request for the singleton HTTP01Proxy named "default"
			return []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      http01proxyObjectName,  // Always "default" (singleton)
						Namespace: namespace,
					},
				},
			}
		}

		r.log.V(4).Info("object not of interest, ignoring", "object", fmt.Sprintf("%T", obj), "name", obj.GetName())
		return []reconcile.Request{}                 // Empty slice = don't reconcile
	}

	// Predicate: filter which objects to watch based on labels
	// Only watch objects with our managed-by label (ignores other DaemonSets, etc.)
	controllerManagedResources := predicate.NewPredicateFuncs(func(object client.Object) bool {
		return object.GetLabels() != nil && object.GetLabels()[common.ManagedResourceLabelKey] == RequestEnqueueLabelValue
	})

	controllerManagedResourcePredicates := builder.WithPredicates(controllerManagedResources)
	// For child resources, also ignore status-only updates (only reconcile on spec/metadata changes)
	// GenerationChangedPredicate: only trigger on generation change (spec/metadata), not status
	withIgnoreStatusUpdatePredicates := builder.WithPredicates(predicate.GenerationChangedPredicate{}, controllerManagedResources)

	// Build and register controller with manager
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.HTTP01Proxy{}).            // Primary resource: reconcile when HTTP01Proxy changes
		Named(ControllerName).                   // Controller name for logging/metrics
		Watches(&appsv1.DaemonSet{}, handler.EnqueueRequestsFromMapFunc(mapFunc), withIgnoreStatusUpdatePredicates). // Watch DaemonSets with our label
		Watches(&rbacv1.ClusterRole{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates). // Watch ClusterRoles
		Watches(&rbacv1.ClusterRoleBinding{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates). // Watch ClusterRoleBindings
		Watches(&corev1.ServiceAccount{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates). // Watch ServiceAccounts
		Watches(&networkingv1.NetworkPolicy{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates). // Watch NetworkPolicies
		Complete(r)                              // Build and start controller (r.Reconcile will be called on events)
}

// Reconcile compares the state specified by the HTTP01Proxy object against the actual cluster state.
// Called by controller-runtime when HTTP01Proxy or watched child resources change
// Core reconciliation pattern: desired state (HTTP01Proxy spec) vs actual state (deployed resources)
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.log.V(1).Info("reconciling", "request", req)

	// Safety check: only process HTTP01Proxy in operator's namespace
	// HTTP01Proxy is namespace-scoped, but singleton so only one namespace matters
	if req.Namespace != common.OperatorNamespace {
		r.log.V(1).Info("ignoring http01proxy in unexpected namespace", "namespace", req.Namespace, "expected", common.OperatorNamespace)
		return ctrl.Result{}, nil                // Don't requeue, just ignore
	}

	// Fetch the HTTP01Proxy object from API server
	proxy := &v1alpha1.HTTP01Proxy{}
	if err := r.Get(ctx, req.NamespacedName, proxy); err != nil {
		if errors.IsNotFound(err) {              // Object was deleted between event and reconcile
			r.log.V(1).Info("http01proxy object not found, skipping reconciliation", "request", req)
			return ctrl.Result{}, nil            // Don't requeue, deletion already happened
		}
		return ctrl.Result{}, fmt.Errorf("failed to fetch http01proxy %q during reconciliation: %w", req.NamespacedName, err)
	}

	// Check if object is marked for deletion (kubectl delete was called)
	if !proxy.DeletionTimestamp.IsZero() {
		r.log.V(1).Info("http01proxy is marked for deletion", "namespace", req.NamespacedName)

		// Clean up all resources we created (DaemonSet, ServiceAccount, RBAC, NetworkPolicies)
		if err := r.cleanUp(ctx, proxy); err != nil {
			return ctrl.Result{}, fmt.Errorf("clean up failed for %q http01proxy deletion: %w", req.NamespacedName, err)
		}

		// Remove our finalizer so Kubernetes can delete the object
		if err := r.removeFinalizer(ctx, proxy); err != nil {
			return ctrl.Result{}, err
		}

		r.log.V(1).Info("removed finalizer, cleanup complete", "request", req.NamespacedName)
		return ctrl.Result{}, nil                // Deletion complete, don't requeue
	}

	// Object is not being deleted - normal reconciliation
	// Add finalizer if not already present (blocks deletion until we clean up)
	if err := r.addFinalizer(ctx, proxy); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update %q http01proxy with finalizers: %w", req.NamespacedName, err)
	}

	// Delegate to main processing logic
	return r.processReconcileRequest(ctx, proxy, req.NamespacedName)
}

// processReconcileRequest handles the main reconciliation logic after finalizer is ensured
// Separates finalizer logic from deployment logic for clarity
func (r *Reconciler) processReconcileRequest(ctx context.Context, proxy *v1alpha1.HTTP01Proxy, req types.NamespacedName) (ctrl.Result, error) {
	// Log if this is the first reconciliation of a newly created object
	if !common.ContainsAnnotation(proxy, controllerProcessedAnnotation) && len(proxy.Status.Conditions) == 0 {
		r.log.V(1).Info("starting reconciliation of newly created http01proxy", "namespace", proxy.GetNamespace(), "name", proxy.GetName())
	}

	// Do the actual work: validate platform and deploy resources
	// reconcileHTTP01ProxyDeployment is in infrastructure.go
	reconcileErr := r.reconcileHTTP01ProxyDeployment(ctx, proxy)
	if reconcileErr != nil {
		r.log.Error(reconcileErr, "failed to reconcile HTTP01Proxy deployment", "request", req)
	}

	// Update status based on success/failure and determine requeue behavior
	// HandleReconcileResult translates error into status conditions (Available/Degraded)
	return common.HandleReconcileResult(
		&proxy.Status.ConditionalStatus,         // Status object to update
		reconcileErr,                            // Error from reconciliation (nil if success)
		r.log.WithValues("namespace", proxy.GetNamespace(), "name", proxy.GetName()),
		func(prependErr error) error {
			return r.updateCondition(ctx, proxy, prependErr) // Callback to persist status to API server
		},
		defaultRequeueTime,                      // Requeue after 30 seconds on transient errors
	)
}

// cleanUp handles deletion of http01proxy gracefully.
// Removes all resources created by controller in reverse order of creation
// Called when HTTP01Proxy is marked for deletion (has DeletionTimestamp)
func (r *Reconciler) cleanUp(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
	r.log.V(1).Info("cleaning up http01proxy resources", "namespace", proxy.GetNamespace(), "name", proxy.GetName())
	r.eventRecorder.Eventf(proxy, corev1.EventTypeNormal, "CleanUp", "cleaning up resources for http01proxy %s/%s", proxy.GetNamespace(), proxy.GetName())

	// Delete in reverse order of creation (DaemonSet → ServiceAccount → RBAC → NetworkPolicies)
	if err := r.deleteDaemonSet(ctx, proxy); err != nil {
		return fmt.Errorf("failed to delete daemonset: %w", err)
	}
	if err := r.deleteServiceAccount(ctx, proxy); err != nil {
		return fmt.Errorf("failed to delete serviceaccount: %w", err)
	}
	if err := r.deleteRBACResources(ctx); err != nil {
		return fmt.Errorf("failed to delete rbac resources: %w", err)
	}
	if err := r.deleteNetworkPolicies(ctx, proxy); err != nil {
		return fmt.Errorf("failed to delete network policies: %w", err)
	}

	return nil                                   // All cleanup successful
}

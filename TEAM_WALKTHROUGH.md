# PR #398 Team Walkthrough - Complete Learning Path

## Purpose
This document provides a **step-by-step learning path** through PR #398 (HTTP01 Proxy). Follow the files in order for best understanding.

---

## Quick Summary: What Does This PR Do?

**Problem:** On bare-metal OpenShift, cert-manager can't complete HTTP-01 ACME challenges for API endpoints because:
- API VIP (e.g., 192.168.1.100) only serves Kubernetes API on port 6443
- cert-manager's solver is behind Ingress VIP (e.g., 192.168.1.101)
- Let's Encrypt requests go to API VIP, but solver is at Ingress VIP

**Solution:** Deploy a proxy on master nodes that:
- Intercepts HTTP traffic on API VIP port 80
- Forwards ACME challenge requests to Ingress VIP
- Rejects all other traffic

**This PR's Role:** Operator code that:
1. Validates platform is bare-metal with distinct VIPs
2. Deploys DaemonSet with proxy container
3. Injects VIP addresses as environment variables
4. Manages lifecycle (create, update, delete)

---

## The Learning Path - 8 Files in Order

| # | File | Category | What It Does | Time |
|---|------|----------|--------------|------|
| 1 | `api/operator/v1alpha1/http01proxy_types.go` | API | Defines the data model | 15 min |
| 2 | `pkg/controller/http01proxy/constants.go` | Controller | Constants and labels | 5 min |
| 3 | `pkg/controller/http01proxy/controller.go` | Controller | Main reconciliation loop | 20 min |
| 4 | `pkg/controller/http01proxy/infrastructure.go` | Controller | Platform detection & validation | 15 min |
| 5 | `pkg/controller/http01proxy/utils.go` | Controller | Status management helpers | 10 min |
| 6 | `pkg/controller/http01proxy/daemonsets.go` | Controller | DaemonSet deployment (KEY!) | 15 min |
| 7 | `bindata/http01-proxy/cert-manager-http01-proxy-daemonset.yaml` | Manifest | DaemonSet template | 10 min |
| 8 | `pkg/controller/http01proxy/install_http01proxy.go` | Controller | Orchestrates deployment | 10 min |

**Total Time:** ~100 minutes (1h 40min)

---

## Learning Path Details

### File 1: API Types - The Data Model
**Path:** `api/operator/v1alpha1/http01proxy_types.go`
**Order:** #1 - START HERE
**Why First:** Defines what users create and what the system manages
**Key Concepts:** 
- HTTP01Proxy resource structure
- Singleton validation
- Spec vs Status
- Cross-field validation

### File 2: Constants
**Path:** `pkg/controller/http01proxy/constants.go`
**Order:** #2
**Why Second:** Understand naming conventions and labels before seeing them used
**Key Concepts:**
- Resource names
- Label keys/values
- Finalizer names

### File 3: Controller - The Engine
**Path:** `pkg/controller/http01proxy/controller.go`
**Order:** #3
**Why Third:** Main reconciliation logic - the heart of the controller
**Key Concepts:**
- Reconcile loop
- Finalizer pattern
- Watch setup
- Event handling

### File 4: Platform Validation - The Gatekeeper
**Path:** `pkg/controller/http01proxy/infrastructure.go`
**Order:** #4
**Why Fourth:** Understand what gets validated before deployment
**Key Concepts:**
- Platform discovery
- VIP extraction
- Validation rules
- Caching for performance

### File 5: Status Management
**Path:** `pkg/controller/http01proxy/utils.go`
**Order:** #5
**Why Fifth:** Understand how status is reported to users
**Key Concepts:**
- Condition management
- Status updates
- Error reporting

### File 6: DaemonSet Deployment - THE KEY CODE
**Path:** `pkg/controller/http01proxy/daemonsets.go`
**Order:** #6
**Why Sixth:** **This is where VIPs get injected!** Most important file.
**Key Concepts:**
- Loading templates
- Injecting environment variables
- VIP passing to proxy container
- Create vs Update logic

### File 7: DaemonSet Template
**Path:** `bindata/http01-proxy/cert-manager-http01-proxy-daemonset.yaml`
**Order:** #7
**Why Seventh:** See what actually gets deployed
**Key Concepts:**
- hostNetwork requirement
- NET_ADMIN capability
- Node selector (masters only)
- Security context

### File 8: Deployment Orchestration
**Path:** `pkg/controller/http01proxy/install_http01proxy.go`
**Order:** #8
**Why Last:** Ties everything together - the full deployment flow
**Key Concepts:**
- Deployment order
- Resource dependencies
- Error handling

---

## Now Let's Walk Through Each File...

---

# FILE 1 of 8: API Types

**File:** `api/operator/v1alpha1/http01proxy_types.go`
**Learning Order:** #1 - START HERE
**Purpose:** Defines the HTTP01Proxy Custom Resource Definition (CRD)
**What It Does:** Describes what users create and what the controller manages
**Time:** 15 minutes

---

## Annotated Code with Comments

```go
// Package v1alpha1 contains API Schema definitions for the operator v1alpha1 API group
package v1alpha1

// Import Kubernetes metadata types (TypeMeta, ObjectMeta, Time, etc.)
import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// init() runs when the package is loaded
// Registers HTTP01Proxy and HTTP01ProxyList with the Scheme
// This tells Kubernetes about these new types
func init() {
	SchemeBuilder.Register(&HTTP01Proxy{}, &HTTP01ProxyList{})
}

// ============================================================================
// HTTP01ProxyList - Used when listing multiple HTTP01Proxy resources
// ============================================================================

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//   ↑ Generates DeepCopy() methods for this type (required by Kubernetes)
// +kubebuilder:object:root=true
//   ↑ This is a root Kubernetes object (not nested)

// HTTP01ProxyList is a list of HTTP01Proxy objects.
// Used by kubectl get http01proxy (returns a list)
type HTTP01ProxyList struct {
	metav1.TypeMeta `json:",inline"`
	// TypeMeta contains apiVersion and kind
	// Example: apiVersion: operator.openshift.io/v1alpha1, kind: HTTP01ProxyList

	metav1.ListMeta `json:"metadata"`
	// ListMeta contains list metadata (resourceVersion, continue token, etc.)

	Items []HTTP01Proxy `json:"items"`
	// Items is the array of HTTP01Proxy objects in the list
}

// ============================================================================
// HTTP01Proxy - The main resource users create
// ============================================================================

// +genclient
//   ↑ Generate typed client for this resource (for programmatic access)
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//   ↑ Generate DeepCopy() method
// +kubebuilder:object:root=true
//   ↑ Root object
// +kubebuilder:subresource:status
//   ↑ Status is a separate subresource (can be updated independently of spec)
// +kubebuilder:resource:path=http01proxies,scope=Namespaced,categories={cert-manager-operator},shortName=http01proxy
//   ↑ API path: /apis/operator.openshift.io/v1alpha1/namespaces/{ns}/http01proxies
//   ↑ Namespace-scoped resource
//   ↑ Category: cert-manager-operator (for kubectl get cert-manager-operator)
//   ↑ Short name: kubectl get http01proxy (instead of http01proxies)
// +kubebuilder:printcolumn:name="Mode",type="string",JSONPath=".spec.mode"
//   ↑ kubectl get http01proxy shows "Mode" column from .spec.mode
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
//   ↑ Shows "Ready" column from status conditions
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].message"
//   ↑ Shows "Message" column from Ready condition
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
//   ↑ Shows "Age" column (how long since created)

// HTTP01Proxy describes the configuration for the HTTP01 challenge proxy
// that redirects traffic from the API endpoint on port 80 to ingress routers.
// This enables cert-manager to perform HTTP01 ACME challenges for API endpoint certificates.
// The name must be `default` to make HTTP01Proxy a singleton.

// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default'",message="http01proxy is a singleton, .metadata.name must be 'default'"
//   ↑ CEL (Common Expression Language) validation
//   ↑ Enforces: metadata.name MUST equal "default"
//   ↑ This makes HTTP01Proxy a singleton (only one allowed)
//   ↑ If you try to create "http01proxy/myproxy" → API server rejects it

// +operator-sdk:csv:customresourcedefinitions:displayName="HTTP01Proxy"
//   ↑ Display name in OLM (Operator Lifecycle Manager) UI
type HTTP01Proxy struct {
	metav1.TypeMeta `json:",inline"`
	// TypeMeta contains:
	//   apiVersion: operator.openshift.io/v1alpha1
	//   kind: HTTP01Proxy

	metav1.ObjectMeta `json:"metadata,omitempty"`
	// ObjectMeta contains:
	//   name, namespace, labels, annotations, finalizers, etc.

	// spec is the specification of the desired behavior of the HTTP01Proxy.
	// This is what the USER provides (input)
	// +kubebuilder:validation:Required
	//   ↑ This field is required (cannot be omitted)
	// +required
	//   ↑ Additional marker for required field
	Spec HTTP01ProxySpec `json:"spec"`

	// status is the most recently observed status of the HTTP01Proxy.
	// This is what the CONTROLLER reports (output)
	// +kubebuilder:validation:Optional
	//   ↑ This field is optional
	// +optional
	//   ↑ Can be omitted (empty when first created)
	Status HTTP01ProxyStatus `json:"status,omitempty"`
	// omitempty means: if Status is empty, don't include it in JSON
}

// ============================================================================
// HTTP01ProxyMode - Enum for deployment modes
// ============================================================================

// HTTP01ProxyMode controls how the HTTP01 challenge proxy is deployed.
// +kubebuilder:validation:Enum=DefaultDeployment;CustomDeployment
//   ↑ Only these two values are allowed
//   ↑ API server rejects anything else
type HTTP01ProxyMode string

const (
	// HTTP01ProxyModeDefault enables the proxy with default configuration.
	// Operator manages everything (deploys DaemonSet, injects VIPs, etc.)
	HTTP01ProxyModeDefault HTTP01ProxyMode = "DefaultDeployment"

	// HTTP01ProxyModeCustom enables the proxy with user-specified configuration.
	// User brings their own proxy deployment (future extension point)
	HTTP01ProxyModeCustom HTTP01ProxyMode = "CustomDeployment"
)

// ============================================================================
// HTTP01ProxySpec - What the user configures
// ============================================================================

// HTTP01ProxySpec is the specification of the desired behavior of the HTTP01Proxy.
// +kubebuilder:validation:XValidation:rule="self.mode == 'CustomDeployment' ? has(self.customDeployment) : !has(self.customDeployment)",message="customDeployment is required when mode is CustomDeployment and forbidden otherwise"
//   ↑ Cross-field validation (CEL expression)
//   ↑ Logic: IF mode == 'CustomDeployment' THEN customDeployment MUST exist
//   ↑        ELSE customDeployment MUST NOT exist
//   ↑ This prevents invalid configurations like:
//   ↑   spec:
//   ↑     mode: DefaultDeployment
//   ↑     customDeployment: {...}  ← INVALID
type HTTP01ProxySpec struct {
	// mode controls whether the HTTP01 challenge proxy is active and how it should be deployed.
	// DefaultDeployment enables the proxy with default configuration.
	// CustomDeployment enables the proxy with user-specified configuration.
	// +kubebuilder:validation:Required
	//   ↑ User must specify this field
	// +required
	Mode HTTP01ProxyMode `json:"mode"`

	// customDeployment contains configuration options when mode is CustomDeployment.
	// This field is only valid when mode is CustomDeployment.
	// +kubebuilder:validation:Optional
	// +optional
	CustomDeployment *HTTP01ProxyCustomDeploymentSpec `json:"customDeployment,omitempty"`
	// Pointer type (*) means it can be nil (absent)
	// omitempty means: if nil, don't include in JSON
}

// ============================================================================
// HTTP01ProxyCustomDeploymentSpec - Future extension point
// ============================================================================

// HTTP01ProxyCustomDeploymentSpec contains configuration for custom proxy deployment.
type HTTP01ProxyCustomDeploymentSpec struct {
	// internalPort specifies the internal port used by the proxy service.
	// Valid values are 1024-65535.
	// +kubebuilder:validation:Minimum=1024
	//   ↑ Port must be >= 1024 (non-privileged)
	// +kubebuilder:validation:Maximum=65535
	//   ↑ Port must be <= 65535 (max port number)
	// +kubebuilder:default=8888
	//   ↑ Default value if not specified
	// +optional
	InternalPort int32 `json:"internalPort,omitempty"`
}

// ============================================================================
// HTTP01ProxyStatus - What the controller reports
// ============================================================================

// HTTP01ProxyStatus is the most recently observed status of the HTTP01Proxy.
type HTTP01ProxyStatus struct {
	// conditions holds information about the current state of the HTTP01 proxy deployment.
	ConditionalStatus `json:",inline,omitempty"`
	// ConditionalStatus is embedded (contains []metav1.Condition)
	// json:",inline,omitempty" means: fields appear directly in status (not nested)
	// Example result:
	//   status:
	//     conditions:        ← From ConditionalStatus
	//     - type: Available
	//       status: "True"

	// proxyImage is the name of the image and the tag used for deploying the proxy.
	ProxyImage string `json:"proxyImage,omitempty"`
	// Example: "quay.io/bapalm/cert-mgr-http01-proxy:v0.2.0"
	// Tells user which image version is deployed
}
```

---

## Key Takeaways from File 1

1. **Singleton Pattern:** Only one HTTP01Proxy allowed (name must be "default")
2. **Two Modes:** DefaultDeployment (operator manages) vs CustomDeployment (user manages)
3. **Cross-field Validation:** customDeployment only valid when mode=CustomDeployment
4. **Spec vs Status:** Spec = user input, Status = controller output
5. **Kubebuilder Markers:** Generate CRD, validation, client code

---

# FILE 2 of 8: Constants

**File:** `pkg/controller/http01proxy/constants.go`
**Learning Order:** #2
**Purpose:** Defines constants used throughout the controller
**What It Does:** Central location for names, labels, annotations
**Time:** 5 minutes

---

## Annotated Code

```go
package http01proxy

import (
	"time"

	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// ========================================================================
	// Controller Identity
	// ========================================================================

	// ControllerName is the name of this controller
	// Used in logs and event recorder
	ControllerName = "http01proxy-controller"

	// ========================================================================
	// Resource Names
	// ========================================================================

	// http01proxyObjectName is the name of the HTTP01Proxy singleton resource
	// Only this name is allowed (enforced by CRD validation)
	http01proxyObjectName = "default"

	// http01proxyCommonName is used in resource names and labels
	// All deployed resources include this in their name
	http01proxyCommonName = "http01proxy"

	// ========================================================================
	// Labels
	// ========================================================================

	// http01proxyManagedResourceLabelKey is the label key for managed resources
	// All resources created by this controller have this label
	http01proxyManagedResourceLabelKey = "operator.openshift.io/managed-resource"

	// http01proxyManagedResourceLabelValue is the label value for managed resources
	// Resources have: operator.openshift.io/managed-resource: "http01proxy"
	// This label is used to:
	//   1. Identify resources managed by this controller
	//   2. Trigger reconciliation when these resources change (watch)
	http01proxyManagedResourceLabelValue = http01proxyCommonName

	// ========================================================================
	// Annotations
	// ========================================================================

	// controllerProcessedAnnotation marks that controller has processed the resource
	// Added after first successful reconciliation
	controllerProcessedAnnotation = "operator.openshift.io/http01proxy-processed"

	// ========================================================================
	// Finalizers
	// ========================================================================

	// http01proxyFinalizer is the finalizer added to HTTP01Proxy resources
	// Prevents deletion until cleanup is complete
	// Format: operator.openshift.io/http01proxy-finalizer
	http01proxyFinalizer = "operator.openshift.io/http01proxy-finalizer"

	// ========================================================================
	// Environment Variables
	// ========================================================================

	// http01proxyImageNameEnvVarName is the env var name for the proxy image
	// Set on the operator Pod, read by controller
	// Example: RELATED_IMAGE_CERT_MANAGER_HTTP01PROXY=quay.io/bapalm/cert-mgr-http01-proxy:latest
	http01proxyImageNameEnvVarName = "RELATED_IMAGE_CERT_MANAGER_HTTP01PROXY"

	// ========================================================================
	// Timing
	// ========================================================================

	// defaultRequeueTime is how long to wait before retrying after an error
	// 1 minute
	defaultRequeueTime = time.Minute

	// ========================================================================
	// Platform Detection
	// ========================================================================

	// platformBareMetal is the platform type for baremetal clusters
	// Used in validation: only BareMetal is supported
	platformBareMetal = "BareMetal"
)

// ============================================================================
// Group Version Kind (GVK) for OpenShift Infrastructure
// ============================================================================

var (
	// infrastructureGVK is the GroupVersionKind for Infrastructure resource
	// Infrastructure/cluster contains platform info (VIPs, platform type, etc.)
	// This is used to fetch the cluster's infrastructure configuration
	infrastructureGVK = schema.GroupVersionKind{
		Group:   "config.openshift.io",  // API group
		Version: "v1",                    // API version
		Kind:    "Infrastructure",        // Resource kind
	}
	// Used like: GET /apis/config.openshift.io/v1/infrastructures/cluster
)
```

---

## Key Takeaways from File 2

1. **Singleton Name:** "default" - hardcoded, only name allowed
2. **Managed Resource Label:** `operator.openshift.io/managed-resource: "http01proxy"`
   - Applied to all created resources (DaemonSet, ServiceAccount, etc.)
   - Used in watch predicates (trigger reconciliation when these change)
3. **Finalizer:** Prevents deletion until cleanup complete
4. **Image Env Var:** Controller reads proxy image from `RELATED_IMAGE_CERT_MANAGER_HTTP01PROXY`
5. **Infrastructure GVK:** How to fetch cluster platform info

---

# FILE 3 of 8: Controller Main Logic

**File:** `pkg/controller/http01proxy/controller.go`
**Learning Order:** #3
**Purpose:** Main reconciliation loop and controller setup
**What It Does:** Handles HTTP01Proxy lifecycle (create, update, delete)
**Time:** 20 minutes

---

## Annotated Code

```go
package http01proxy

import (
	"context"
	"fmt"
	"os"
	"sync"

	// Kubernetes core types
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	// Controller runtime
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"

	// Our types
	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

// ============================================================================
// Constants
// ============================================================================

const (
	// RequestEnqueueLabelValue is the label value used for filtering reconcile events.
	// When a watched resource (DaemonSet, ServiceAccount, etc.) has this label,
	// it triggers a reconciliation of the HTTP01Proxy
	RequestEnqueueLabelValue = http01proxyCommonName
)

// ============================================================================
// Reconciler Structure
// ============================================================================

// Reconciler reconciles an HTTP01Proxy object.
type Reconciler struct {
	common.CtrlClient
	// ↑ Embedded - provides Get(), Create(), Update(), Delete() methods
	//   for talking to Kubernetes API

	eventRecorder record.EventRecorder
	// ↑ For emitting Kubernetes events
	//   Events visible in: kubectl describe http01proxy default

	log logr.Logger
	// ↑ Structured logger
	//   Example: log.Info("msg", "key", "value")

	proxyImage string
	// ↑ Proxy container image URL
	//   Read from env var: RELATED_IMAGE_CERT_MANAGER_HTTP01PROXY
	//   Example: "quay.io/bapalm/cert-mgr-http01-proxy:v0.2.0"

	cachedPlatform *platformInfo
	// ↑ Cached platform detection result
	//   Avoids repeatedly fetching Infrastructure/cluster
	//   Contains: platformType, apiVIPs, ingressVIPs

	platformMu sync.Mutex
	// ↑ Mutex protects cachedPlatform
	//   Multiple reconcile loops might run concurrently
	//   Mutex ensures thread-safe access to the cache
}

// ============================================================================
// RBAC Markers - Generate ClusterRole
// ============================================================================

// +kubebuilder:rbac:groups=operator.openshift.io,resources=http01proxies,verbs=get;list;watch;update;patch
//   ↑ Permission to manage HTTP01Proxy resources
// +kubebuilder:rbac:groups=operator.openshift.io,resources=http01proxies/status,verbs=get;update;patch
//   ↑ Permission to update HTTP01Proxy status
// +kubebuilder:rbac:groups=operator.openshift.io,resources=http01proxies/finalizers,verbs=update
//   ↑ Permission to add/remove finalizers
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
//   ↑ Permission to manage NetworkPolicies
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
//   ↑ Permission to manage DaemonSets (the proxy)
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
//   ↑ Permission to manage ServiceAccounts (empty string "" = core API group)
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
//   ↑ Permission to manage ClusterRoles and ClusterRoleBindings
// +kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions;ingresses;infrastructures,verbs=get;list;watch
//   ↑ Permission to read OpenShift cluster config
//   ↑ Specifically: Infrastructure/cluster (to get platform type and VIPs)
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,resourceNames=privileged,verbs=use
//   ↑ Permission to bind ServiceAccount to privileged SCC
//   ↑ Required because proxy needs hostNetwork + NET_ADMIN

// ============================================================================
// Constructor
// ============================================================================

// New returns a new Reconciler instance.
func New(mgr ctrl.Manager) (*Reconciler, error) {
	// Create Kubernetes client
	c, err := common.NewClient(mgr)
	if err != nil {
		return nil, err
	}

	return &Reconciler{
		CtrlClient:    c,
		eventRecorder: mgr.GetEventRecorderFor(ControllerName),
		log:           ctrl.Log.WithName(ControllerName),
		proxyImage:    os.Getenv(http01proxyImageNameEnvVarName),
		// ↑ Read proxy image from environment variable
		//   This env var is set in config/manager/manager.yaml
	}, nil
}

// ============================================================================
// Setup Watches
// ============================================================================

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	// mapFunc translates "some resource changed" to "reconcile HTTP01Proxy"
	mapFunc := func(ctx context.Context, obj client.Object) []reconcile.Request {
		r.log.V(4).Info("received reconcile event",
			"object", fmt.Sprintf("%T", obj),
			"name", obj.GetName(),
			"namespace", obj.GetNamespace())

		// Check if object has our managed resource label
		objLabels := obj.GetLabels()
		if objLabels != nil && objLabels[common.ManagedResourceLabelKey] == RequestEnqueueLabelValue {
			// This resource is managed by us, trigger reconciliation
			namespace := obj.GetNamespace()
			if namespace == "" {
				namespace = common.OperatorNamespace
			}

			return []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      http01proxyObjectName,  // "default"
						Namespace: namespace,               // "cert-manager-operator"
					},
				},
			}
		}

		r.log.V(4).Info("object not of interest, ignoring")
		return []reconcile.Request{}
	}

	// Predicate: only watch resources with our label
	controllerManagedResources := predicate.NewPredicateFuncs(func(object client.Object) bool {
		return object.GetLabels() != nil &&
			object.GetLabels()[common.ManagedResourceLabelKey] == RequestEnqueueLabelValue
	})

	controllerManagedResourcePredicates := builder.WithPredicates(controllerManagedResources)
	withIgnoreStatusUpdatePredicates := builder.WithPredicates(
		predicate.GenerationChangedPredicate{},  // Ignore status-only updates
		controllerManagedResources,
	)

	// Build the controller
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.HTTP01Proxy{}).  // Primary resource we manage
		Named(ControllerName).
		// Watch child resources - if they change, reconcile HTTP01Proxy
		Watches(&appsv1.DaemonSet{}, handler.EnqueueRequestsFromMapFunc(mapFunc), withIgnoreStatusUpdatePredicates).
		Watches(&rbacv1.ClusterRole{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&rbacv1.ClusterRoleBinding{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&corev1.ServiceAccount{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&networkingv1.NetworkPolicy{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Complete(r)
	// ↑ Watching child resources enables drift detection
	//   If someone manually deletes DaemonSet, controller recreates it
}

// ============================================================================
// Main Reconcile Loop
// ============================================================================

// Reconcile compares the state specified by the HTTP01Proxy object
// against the actual cluster state.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.log.V(1).Info("reconciling", "request", req)

	// STEP 1: Validate namespace
	if req.Namespace != common.OperatorNamespace {
		r.log.V(1).Info("ignoring http01proxy in unexpected namespace",
			"namespace", req.Namespace,
			"expected", common.OperatorNamespace)
		return ctrl.Result{}, nil
	}

	// STEP 2: Fetch HTTP01Proxy resource
	proxy := &v1alpha1.HTTP01Proxy{}
	if err := r.Get(ctx, req.NamespacedName, proxy); err != nil {
		if errors.IsNotFound(err) {
			r.log.V(1).Info("http01proxy object not found, skipping reconciliation")
			return ctrl.Result{}, nil  // Already deleted, nothing to do
		}
		return ctrl.Result{}, fmt.Errorf("failed to fetch http01proxy %q: %w", req.NamespacedName, err)
	}

	// STEP 3: Handle deletion
	if !proxy.DeletionTimestamp.IsZero() {
		// User ran: kubectl delete http01proxy default
		// DeletionTimestamp is set, but object still exists (finalizer blocks it)
		r.log.V(1).Info("http01proxy is marked for deletion", "namespace", req.NamespacedName)

		// Clean up all resources we created
		if err := r.cleanUp(ctx, proxy); err != nil {
			return ctrl.Result{}, fmt.Errorf("clean up failed: %w", err)
		}

		// Remove finalizer (now deletion can proceed)
		if err := r.removeFinalizer(ctx, proxy); err != nil {
			return ctrl.Result{}, err
		}

		r.log.V(1).Info("removed finalizer, cleanup complete")
		return ctrl.Result{}, nil
	}

	// STEP 4: Add finalizer if not present
	if err := r.addFinalizer(ctx, proxy); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
	}

	// STEP 5: Main reconciliation
	return r.processReconcileRequest(ctx, proxy, req.NamespacedName)
}

// processReconcileRequest handles the main reconciliation logic
func (r *Reconciler) processReconcileRequest(ctx context.Context, proxy *v1alpha1.HTTP01Proxy, req types.NamespacedName) (ctrl.Result, error) {
	// Check if this is the first time we're processing this resource
	if !common.ContainsAnnotation(proxy, controllerProcessedAnnotation) && len(proxy.Status.Conditions) == 0 {
		r.log.V(1).Info("starting reconciliation of newly created http01proxy")
	}

	// Attempt reconciliation
	reconcileErr := r.reconcileHTTP01ProxyDeployment(ctx, proxy)
	if reconcileErr != nil {
		r.log.Error(reconcileErr, "failed to reconcile HTTP01Proxy deployment")
	}

	// Update status based on result
	return common.HandleReconcileResult(
		&proxy.Status.ConditionalStatus,
		reconcileErr,
		r.log.WithValues("namespace", proxy.GetNamespace(), "name", proxy.GetName()),
		func(prependErr error) error {
			return r.updateCondition(ctx, proxy, prependErr)
		},
		defaultRequeueTime,
	)
}

// ============================================================================
// Cleanup
// ============================================================================

// cleanUp handles deletion of http01proxy gracefully.
func (r *Reconciler) cleanUp(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
	r.log.V(1).Info("cleaning up http01proxy resources")
	r.eventRecorder.Eventf(proxy, corev1.EventTypeNormal, "CleanUp",
		"cleaning up resources for http01proxy %s/%s", proxy.GetNamespace(), proxy.GetName())

	// Delete in reverse order of creation
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

	return nil
}
```

---

## Key Takeaways from File 3

1. **Reconcile Loop:** Fetch → Check deletion → Add finalizer → Reconcile deployment
2. **Watch Pattern:** Controller watches HTTP01Proxy + all child resources (DaemonSet, etc.)
3. **Drift Detection:** If someone deletes DaemonSet, watch triggers reconciliation → recreates it
4. **Finalizer Pattern:** Prevents deletion until cleanup completes
5. **Label-based Filtering:** Only resources with `operator.openshift.io/managed-resource: "http01proxy"` trigger reconciliation

---

# FILE 4 of 8: Platform Validation

**File:** `pkg/controller/http01proxy/infrastructure.go`
**Learning Order:** #4
**Purpose:** Discover and validate platform configuration
**What It Does:** Checks if platform is BareMetal with distinct VIPs
**Time:** 15 minutes

---

## Annotated Code

```go
package http01proxy

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// platformBareMetal is the platform type for baremetal clusters.
	platformBareMetal = "BareMetal"
)

// ============================================================================
// Platform Info Structure
// ============================================================================

// platformInfo holds the discovered platform details needed to decide
// whether the HTTP01 proxy should be deployed.
type platformInfo struct {
	platformType string   // "BareMetal", "AWS", "Azure", etc.
	apiVIPs      []string // API server VIP addresses (e.g., ["192.168.1.100"])
	ingressVIPs  []string // Ingress VIP addresses (e.g., ["192.168.1.101"])
}
// Why slices? Supports dual-stack networking (IPv4 + IPv6)
// Example: apiVIPs = ["192.168.1.100", "fd00::100"]

// ============================================================================
// Cached Platform Discovery
// ============================================================================

// getOrDiscoverPlatform returns cached platform info, or fetches it on first call.
// Uses caching for performance - platform doesn't change during runtime.
func (r *Reconciler) getOrDiscoverPlatform(ctx context.Context) (*platformInfo, error) {
	r.platformMu.Lock()          // Acquire lock (thread safety)
	defer r.platformMu.Unlock()  // Release lock when function returns

	// Cache hit? Return immediately
	if r.cachedPlatform != nil {
		return r.cachedPlatform, nil
	}

	// Cache miss - discover platform info
	info, err := r.discoverPlatform(ctx)
	if err != nil {
		return nil, err
	}

	// Cache the result for future calls
	r.cachedPlatform = info
	return info, nil
}
// Why mutex?
//   Multiple reconcile loops might run concurrently (different goroutines)
//   Without mutex → race condition (both try to cache simultaneously)
//   With mutex → only one goroutine accesses cache at a time

// ============================================================================
// Platform Discovery
// ============================================================================

// discoverPlatform reads the Infrastructure CR and returns platform details.
func (r *Reconciler) discoverPlatform(ctx context.Context) (*platformInfo, error) {
	// STEP 1: Fetch Infrastructure/cluster resource
	infra := &unstructured.Unstructured{}
	infra.SetGroupVersionKind(infrastructureGVK)
	// infrastructureGVK = {
	//   Group: "config.openshift.io",
	//   Version: "v1",
	//   Kind: "Infrastructure"
	// }

	if err := r.Get(ctx, types.NamespacedName{Name: "cluster"}, infra); err != nil {
		return nil, fmt.Errorf("failed to get infrastructure/cluster: %w", err)
	}
	// Fetches: /apis/config.openshift.io/v1/infrastructures/cluster

	// STEP 2: Extract platform type from JSON
	platformType, found, err := unstructured.NestedString(infra.Object,
		"status", "platformStatus", "type")
	// Equivalent to: infra.Object["status"]["platformStatus"]["type"]
	// Returns: (value string, found bool, err error)

	if err != nil {
		return nil, fmt.Errorf("failed to parse infrastructure status.platformStatus.type: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("infrastructure status.platformStatus.type not found")
	}
	// platformType is now "BareMetal", "AWS", "Azure", etc.

	info := &platformInfo{
		platformType: platformType,
	}

	// STEP 3: If BareMetal, extract VIP addresses
	switch platformType {
	case platformBareMetal:
		// Extract API VIPs
		apiVIPs, _, err := unstructured.NestedStringSlice(infra.Object,
			"status", "platformStatus", "baremetal", "apiServerInternalIPs")
		// Equivalent to: infra.Object["status"]["platformStatus"]["baremetal"]["apiServerInternalIPs"]
		// Returns: ([]string, found bool, error)
		// Example result: ["192.168.1.100"] or ["192.168.1.100", "fd00::100"]

		if err != nil {
			return nil, fmt.Errorf("failed to parse baremetal.apiServerInternalIPs: %w", err)
		}

		// Extract Ingress VIPs
		ingressVIPs, _, err := unstructured.NestedStringSlice(infra.Object,
			"status", "platformStatus", "baremetal", "ingressIPs")
		// Example result: ["192.168.1.101"]

		if err != nil {
			return nil, fmt.Errorf("failed to parse baremetal.ingressIPs: %w", err)
		}

		info.apiVIPs = apiVIPs
		info.ingressVIPs = ingressVIPs
	}
	// For non-BareMetal platforms, VIP slices remain empty

	return info, nil
}

// ============================================================================
// Platform Validation - The Gatekeeper
// ============================================================================

// validatePlatform checks whether the platform supports HTTP01 proxy deployment.
// Returns a human-readable reason if the platform is not supported, or empty string if OK.
func validatePlatform(info *platformInfo) string {
	// CHECK 1: Must be BareMetal
	if info.platformType != platformBareMetal {
		return fmt.Sprintf("platform type %q is not supported; HTTP01 proxy is only supported on BareMetal platforms", info.platformType)
	}
	// If platform is AWS, Azure, etc. → return error message
	// Controller will set Degraded=True with this message
	// No resources will be deployed

	// CHECK 2: Must have at least one API VIP
	if len(info.apiVIPs) == 0 {
		return "no API server VIPs found in infrastructure status; cannot deploy HTTP01 proxy"
	}
	// If apiServerInternalIPs field is empty or missing → error

	// CHECK 3: Must have at least one Ingress VIP
	if len(info.ingressVIPs) == 0 {
		return "no ingress VIPs found in infrastructure status; cannot deploy HTTP01 proxy"
	}
	// If ingressIPs field is empty or missing → error

	// CHECK 4: VIPs must be different
	// Check ALL combinations (supports dual-stack with multiple IPs)
	for _, apiVIP := range info.apiVIPs {
		for _, ingressVIP := range info.ingressVIPs {
			if apiVIP == ingressVIP {
				return fmt.Sprintf("API VIP (%s) and ingress VIP (%s) are the same; HTTP01 proxy is not needed", apiVIP, ingressVIP)
			}
		}
	}
	// Why check all combinations?
	//   If apiVIPs = ["192.168.1.100", "fd00::100"]
	//   And ingressVIPs = ["192.168.1.100", "fd00::101"]
	//   First IPs match → proxy not needed for IPv4
	//   This check catches it

	// If ANY API VIP equals ANY Ingress VIP → proxy not needed
	// Example:
	//   apiVIPs = ["192.168.1.100"]
	//   ingressVIPs = ["192.168.1.100"]
	//   → Same VIP, ACME requests already reach Ingress, no proxy needed

	// ALL CHECKS PASSED!
	return ""
}
// How this is used:
//   errMsg := validatePlatform(platformInfo)
//   if errMsg != "" {
//     // Set Degraded status with errMsg
//     // Don't deploy any resources
//     return
//   }
//   // Platform is valid, proceed with deployment
```

---

## Key Takeaways from File 4

1. **Caching:** Platform info cached after first fetch (performance optimization)
2. **Thread Safety:** Mutex protects cache from concurrent access
3. **Unstructured API:** Works with Infrastructure without importing its types
4. **Four Validations:**
   - Platform must be BareMetal
   - Must have API VIPs
   - Must have Ingress VIPs
   - VIPs must be different
5. **Error Messages:** Validation returns user-friendly messages (shown in status)

---

**Continue to next file? (Reply "continue" and I'll provide Files 5-8)**

This format gives you exactly what you need to walk your team through the code!
---

# FILE 5 of 8: Status Management

**File:** `pkg/controller/http01proxy/utils.go`
**Learning Order:** #5
**Purpose:** Helper functions for managing status and conditions
**What It Does:** Updates HTTP01Proxy status based on reconciliation results
**Time:** 10 minutes

---

## Annotated Code

```go
package http01proxy

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

// ============================================================================
// Finalizer Management
// ============================================================================

// addFinalizer adds the http01proxy finalizer to the HTTP01Proxy resource
func (r *Reconciler) addFinalizer(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
	// Check if finalizer already exists
	if common.ContainsFinalizer(proxy, http01proxyFinalizer) {
		return nil  // Already has finalizer, nothing to do
	}

	// Add finalizer to the list
	common.AddFinalizer(proxy, http01proxyFinalizer)
	// http01proxyFinalizer = "operator.openshift.io/http01proxy-finalizer"

	// Update the resource in Kubernetes
	if err := r.Update(ctx, proxy); err != nil {
		return fmt.Errorf("failed to add finalizer: %w", err)
	}

	r.log.V(1).Info("added finalizer", "finalizer", http01proxyFinalizer)
	return nil
}
// Why finalizers?
//   When user runs: kubectl delete http01proxy default
//   1. Kubernetes sets DeletionTimestamp (marks for deletion)
//   2. BUT doesn't delete yet (finalizer blocks it)
//   3. Controller sees DeletionTimestamp, runs cleanup
//   4. Controller removes finalizer
//   5. NOW Kubernetes actually deletes the object
//
// This ensures cleanup happens before deletion

// removeFinalizer removes the http01proxy finalizer from the HTTP01Proxy resource
func (r *Reconciler) removeFinalizer(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
	// Check if finalizer exists
	if !common.ContainsFinalizer(proxy, http01proxyFinalizer) {
		return nil  // No finalizer, nothing to remove
	}

	// Remove finalizer from the list
	common.RemoveFinalizer(proxy, http01proxyFinalizer)

	// Update the resource
	if err := r.Update(ctx, proxy); err != nil {
		return fmt.Errorf("failed to remove finalizer: %w", err)
	}

	r.log.V(1).Info("removed finalizer", "finalizer", http01proxyFinalizer)
	return nil
}

// ============================================================================
// Status Updates
// ============================================================================

// updateCondition updates the status conditions of the HTTP01Proxy resource
func (r *Reconciler) updateCondition(ctx context.Context, proxy *v1alpha1.HTTP01Proxy, reconcileErr error) error {
	// Save the current status for comparison
	oldStatus := proxy.Status.DeepCopy()

	// Set proxy image in status (tracks which version is deployed)
	proxy.Status.ProxyImage = r.proxyImage
	// Example: "quay.io/bapalm/cert-mgr-http01-proxy:v0.2.0"

	// Update conditions based on reconciliation result
	if reconcileErr != nil {
		// Reconciliation failed
		updateStatusDegraded(proxy, "ReconciliationFailed", reconcileErr.Error())
		// Sets: Degraded=True, Available=False
	} else {
		// Reconciliation succeeded
		updateStatusAvailable(proxy)
		// Sets: Available=True, Degraded=False, Progressing=False
	}

	// Only update if status actually changed (avoid unnecessary API calls)
	if !common.StatusEqual(oldStatus, &proxy.Status) {
		if err := r.Status().Update(ctx, proxy); err != nil {
			return fmt.Errorf("failed to update status: %w", err)
		}
		r.log.V(1).Info("updated status")
	}

	return nil
}

// ============================================================================
// Condition Helpers
// ============================================================================

// updateStatusAvailable sets Available=True, Degraded=False, Progressing=False
func updateStatusAvailable(proxy *v1alpha1.HTTP01Proxy) {
	// Set Available condition
	setCondition(proxy, metav1.Condition{
		Type:    "Available",
		Status:  metav1.ConditionTrue,
		Reason:  "HTTP01ProxyDeployed",
		Message: "HTTP01 proxy DaemonSet is deployed and running",
	})

	// Set Degraded condition
	setCondition(proxy, metav1.Condition{
		Type:    "Degraded",
		Status:  metav1.ConditionFalse,
		Reason:  "AsExpected",
		Message: "",
	})

	// Set Progressing condition
	setCondition(proxy, metav1.Condition{
		Type:    "Progressing",
		Status:  metav1.ConditionFalse,
		Reason:  "AsExpected",
		Message: "",
	})
}
// Result in kubectl get http01proxy default -o yaml:
//   status:
//     conditions:
//     - type: Available
//       status: "True"
//       reason: HTTP01ProxyDeployed
//       message: HTTP01 proxy DaemonSet is deployed and running
//     - type: Degraded
//       status: "False"
//     - type: Progressing
//       status: "False"

// updateStatusDegraded sets Degraded=True, Available=False
func updateStatusDegraded(proxy *v1alpha1.HTTP01Proxy, reason, message string) {
	// Set Degraded condition
	setCondition(proxy, metav1.Condition{
		Type:    "Degraded",
		Status:  metav1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})

	// Set Available condition
	setCondition(proxy, metav1.Condition{
		Type:    "Available",
		Status:  metav1.ConditionFalse,
		Reason:  "Degraded",
		Message: "",
	})
}
// Example usage:
//   if platform validation fails:
//     updateStatusDegraded(proxy, "UnsupportedPlatform", "platform type AWS is not supported")
//
// Result:
//   status:
//     conditions:
//     - type: Degraded
//       status: "True"
//       reason: UnsupportedPlatform
//       message: platform type AWS is not supported
//     - type: Available
//       status: "False"

// setCondition adds or updates a condition in the status
func setCondition(proxy *v1alpha1.HTTP01Proxy, newCondition metav1.Condition) {
	// Find existing condition of the same type
	for i, condition := range proxy.Status.Conditions {
		if condition.Type == newCondition.Type {
			// Found it - check if it needs updating
			if condition.Status != newCondition.Status ||
				condition.Reason != newCondition.Reason ||
				condition.Message != newCondition.Message {
				// Something changed, update it
				proxy.Status.Conditions[i] = newCondition
				proxy.Status.Conditions[i].LastTransitionTime = metav1.Now()
				// LastTransitionTime updated ONLY when Status/Reason/Message changes
			}
			return
		}
	}

	// Condition doesn't exist yet, add it
	newCondition.LastTransitionTime = metav1.Now()
	proxy.Status.Conditions = append(proxy.Status.Conditions, newCondition)
}
// Why track LastTransitionTime?
//   Tells you when the condition last changed
//   Example: "Degraded since 10:30 AM" (been broken for 2 hours)
```

---

## Key Takeaways from File 5

1. **Finalizers:** Prevent deletion until cleanup completes
2. **Condition Types:** Available, Degraded, Progressing (standard Kubernetes pattern)
3. **Status Updates:** Only update if actually changed (efficiency)
4. **Error Reporting:** Degraded condition shows user-friendly error messages
5. **Proxy Image Tracking:** Status shows which image version is deployed

---

# FILE 6 of 8: DaemonSet Deployment - THE KEY CODE

**File:** `pkg/controller/http01proxy/daemonsets.go`
**Learning Order:** #6
**Purpose:** Deploy DaemonSet and inject VIP addresses
**What It Does:** **THIS IS WHERE VIPs GET PASSED TO THE PROXY CONTAINER!**
**Time:** 15 minutes

---

## Annotated Code

```go
package http01proxy

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

// ============================================================================
// DaemonSet Reconciliation - THE MOST IMPORTANT FUNCTION
// ============================================================================

// reconcileDaemonSet ensures the HTTP01 proxy DaemonSet exists and is up to date
// THIS IS WHERE THE MAGIC HAPPENS - VIPs ARE INJECTED HERE!
func (r *Reconciler) reconcileDaemonSet(ctx context.Context, proxy *v1alpha1.HTTP01Proxy, platformInfo *platformInfo) error {
	// STEP 1: Load DaemonSet template from embedded YAML files
	dsBytes, err := assets.ReadFile("http01-proxy/cert-manager-http01-proxy-daemonset.yaml")
	// ↑ Reads from: bindata/http01-proxy/cert-manager-http01-proxy-daemonset.yaml
	//   These files are embedded in the binary at compile time
	//   See: pkg/operator/assets/bindata.go (generated by go:embed)
	if err != nil {
		return fmt.Errorf("failed to read daemonset template: %w", err)
	}

	// STEP 2: Parse YAML into Go DaemonSet struct
	desired := &appsv1.DaemonSet{}
	if err := yaml.Unmarshal(dsBytes, desired); err != nil {
		return fmt.Errorf("failed to unmarshal daemonset: %w", err)
	}
	// Now desired contains the DaemonSet definition from YAML

	// STEP 3: Set namespace
	desired.Namespace = proxy.Namespace
	// Usually: "cert-manager-operator"

	// STEP 4: Set proxy container image
	// Find the container (should be index 0)
	for i := range desired.Spec.Template.Spec.Containers {
		container := &desired.Spec.Template.Spec.Containers[i]
		if container.Name == "http01-proxy" {
			container.Image = r.proxyImage
			// r.proxyImage comes from env var: RELATED_IMAGE_CERT_MANAGER_HTTP01PROXY
			// Example: "quay.io/bapalm/cert-mgr-http01-proxy:v0.2.0"
		}
	}

	// STEP 5: ▼▼▼ INJECT VIP ADDRESSES AS ENVIRONMENT VARIABLES ▼▼▼
	// THIS IS THE KEY PART! This is how the proxy knows which VIPs to use!
	for i := range desired.Spec.Template.Spec.Containers {
		container := &desired.Spec.Template.Spec.Containers[i]

		// Add API_VIP environment variable
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name:  "API_VIP",
				Value: platformInfo.apiVIPs[0],  // Example: "192.168.1.100"
			},
		)
		// ↑ The proxy container will read this env var
		//   It knows: "Forward requests to Ingress VIP at THIS address"

		// Add INGRESS_VIP environment variable
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name:  "INGRESS_VIP",
				Value: platformInfo.ingressVIPs[0],  // Example: "192.168.1.101"
			},
		)
		// ↑ The proxy container uses this to know where to forward
	}
	// ▲▲▲ END OF VIP INJECTION ▲▲▲
	//
	// After this, the DaemonSet Pod will have environment variables:
	//   API_VIP=192.168.1.100
	//   INGRESS_VIP=192.168.1.101
	//
	// The proxy container (different repo) reads these and uses them:
	//   targetURL := fmt.Sprintf("http://%s:80%s", os.Getenv("INGRESS_VIP"), path)

	// STEP 6: Add managed resource label
	if desired.Labels == nil {
		desired.Labels = make(map[string]string)
	}
	desired.Labels[common.ManagedResourceLabelKey] = http01proxyManagedResourceLabelValue
	// Adds: operator.openshift.io/managed-resource: "http01proxy"
	// This label is used by watch predicates (triggers reconciliation when DaemonSet changes)

	// STEP 7: Set owner reference for garbage collection
	if err := controllerutil.SetControllerReference(proxy, desired, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}
	// Owner reference links DaemonSet to HTTP01Proxy
	// When HTTP01Proxy is deleted, Kubernetes garbage-collects the DaemonSet
	// Also visible in: kubectl get daemonset cert-manager-http01-proxy -o yaml
	//   metadata:
	//     ownerReferences:
	//     - apiVersion: operator.openshift.io/v1alpha1
	//       kind: HTTP01Proxy
	//       name: default
	//       controller: true

	// STEP 8: Check if DaemonSet already exists
	existing := &appsv1.DaemonSet{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      desired.Name,
		Namespace: desired.Namespace,
	}, existing)

	if err != nil {
		if errors.IsNotFound(err) {
			// DaemonSet doesn't exist, create it
			r.log.Info("creating daemonset",
				"name", desired.Name,
				"namespace", desired.Namespace)
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create daemonset: %w", err)
			}
			r.eventRecorder.Eventf(proxy, corev1.EventTypeNormal, "Created",
				"Created DaemonSet %s/%s", desired.Namespace, desired.Name)
			return nil
		}
		// Some other error (network, permissions, etc.)
		return fmt.Errorf("failed to get daemonset: %w", err)
	}

	// STEP 9: DaemonSet exists, update it
	r.log.Info("updating daemonset",
		"name", desired.Name,
		"namespace", desired.Namespace)

	// Copy desired spec to existing resource
	existing.Spec = desired.Spec
	// This preserves metadata (UID, ResourceVersion, etc.)
	// But updates the spec (containers, volumes, etc.)

	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update daemonset: %w", err)
	}

	r.eventRecorder.Eventf(proxy, corev1.EventTypeNormal, "Updated",
		"Updated DaemonSet %s/%s", desired.Namespace, desired.Name)

	return nil
}

// ============================================================================
// DaemonSet Deletion
// ============================================================================

// deleteDaemonSet removes the HTTP01 proxy DaemonSet
// Called during cleanup when HTTP01Proxy is being deleted
func (r *Reconciler) deleteDaemonSet(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
	ds := &appsv1.DaemonSet{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      "cert-manager-http01-proxy",
		Namespace: proxy.Namespace,
	}, ds)

	if err != nil {
		if errors.IsNotFound(err) {
			// Already deleted, nothing to do
			r.log.V(1).Info("daemonset already deleted")
			return nil
		}
		return fmt.Errorf("failed to get daemonset for deletion: %w", err)
	}

	// DaemonSet exists, delete it
	r.log.Info("deleting daemonset", "name", ds.Name)
	if err := r.Delete(ctx, ds); err != nil {
		return fmt.Errorf("failed to delete daemonset: %w", err)
	}

	r.eventRecorder.Eventf(proxy, corev1.EventTypeNormal, "Deleted",
		"Deleted DaemonSet %s/%s", ds.Namespace, ds.Name)

	return nil
}
```

---

## Key Takeaways from File 6

1. **VIP Injection:** VIPs passed to proxy via environment variables (KEY!)
2. **Template Loading:** Reads YAML from embedded files
3. **Create vs Update:** If doesn't exist → Create, if exists → Update
4. **Owner References:** Links DaemonSet to HTTP01Proxy (garbage collection)
5. **Managed Label:** `operator.openshift.io/managed-resource: "http01proxy"`

**THIS IS THE MOST IMPORTANT FILE IN THE PR!**

---

# FILE 7 of 8: DaemonSet Template

**File:** `bindata/http01-proxy/cert-manager-http01-proxy-daemonset.yaml`
**Learning Order:** #7
**Purpose:** Template for the proxy DaemonSet
**What It Does:** Defines how proxy Pods run on master nodes
**Time:** 10 minutes

---

## Annotated YAML

```yaml
# Standard Kubernetes DaemonSet
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: cert-manager-http01-proxy
  # Namespace is set by controller code
  labels:
    app: cert-manager-http01-proxy
    app.kubernetes.io/name: cert-manager-http01-proxy
    app.kubernetes.io/part-of: cert-manager-operator
spec:
  selector:
    matchLabels:
      app: cert-manager-http01-proxy
  
  # Rolling update strategy (update one pod at a time)
  updateStrategy:
    type: RollingUpdate
  
  template:
    metadata:
      labels:
        app: cert-manager-http01-proxy
        app.kubernetes.io/name: cert-manager-http01-proxy
        app.kubernetes.io/part-of: cert-manager-operator
    
    spec:
      # ServiceAccount for the pods
      serviceAccountName: cert-manager-http01-proxy
      
      # ▼ CRITICAL: Share host's network namespace ▼
      hostNetwork: true
      # Why hostNetwork?
      #   The proxy needs to intercept traffic destined for the API VIP
      #   The API VIP is the node's own IP address
      #   Only with hostNetwork can the pod see traffic to the node's IP
      #   Without this, the pod is in its own network namespace
      #   and can't intercept VIP traffic
      
      # ▼ Only run on master nodes ▼
      nodeSelector:
        node-role.kubernetes.io/master: ""
      # Why only masters?
      #   The API VIP floats between master nodes (high availability)
      #   It could be on master-1, master-2, or master-3
      #   We need the proxy on ALL masters so it's there when VIP fails over
      
      # ▼ Tolerate master taints ▼
      tolerations:
      - key: node-role.kubernetes.io/master
        operator: Exists
        effect: NoSchedule
      - key: node-role.kubernetes.io/control-plane
        operator: Exists
        effect: NoSchedule
      # Why tolerations?
      #   Master nodes are "tainted" to prevent normal workloads
      #   Only pods with matching "tolerations" can run there
      #   This ensures we can run on masters
      
      containers:
      - name: http01-proxy
        # Image URL is set by controller code
        # Will be: quay.io/bapalm/cert-mgr-http01-proxy:v0.2.0
        image: ${RELATED_IMAGE_CERT_MANAGER_HTTP01PROXY}
        
        # ▼ Bind to host port 8888 ▼
        ports:
        - name: proxy
          containerPort: 8888
          hostPort: 8888  # ← Pod's port 8888 becomes available on node's port 8888
          protocol: TCP
        # Why hostPort?
        #   Makes port 8888 available on the node itself
        #   nftables redirects port 80 → 8888
        #   The Go proxy listens on 8888
        
        # ▼ Environment variables ▼
        env:
        - name: PROXY_PORT
          value: "8888"
        # ↑ Already in template
        #
        # Controller adds these during reconciliation:
        # - name: API_VIP
        #   value: "192.168.1.100"      ← Injected by controller
        # - name: INGRESS_VIP
        #   value: "192.168.1.101"      ← Injected by controller
        
        # ▼ Security context ▼
        securityContext:
          allowPrivilegeEscalation: false
          # Prevent process from gaining more privileges
          
          capabilities:
            add:
            - NET_ADMIN  # ← Can modify iptables/nftables
            drop:
            - ALL        # Drop all other capabilities
          # Why NET_ADMIN?
          #   Proxy needs to set up nftables rules
          #   Rules redirect: port 80 → 8888
          #   nftables modification requires NET_ADMIN capability
          
          runAsNonRoot: false  # ← Must run as root
          # Why root?
          #   nftables modification requires root privileges
          #   This is unavoidable security requirement
        
        # ▼ Resource limits ▼
        resources:
          requests:
            cpu: 10m       # 0.01 CPU (very small)
            memory: 32Mi   # 32 megabytes
          limits:
            cpu: 100m      # 0.1 CPU max
            memory: 64Mi   # 64 megabytes max
        # Why so small?
        #   The proxy is very lightweight
        #   It just checks paths and forwards requests
        #   Minimal CPU and memory needed
      
      # ▼ High priority ▼
      priorityClassName: system-cluster-critical
      # Why cluster-critical?
      #   This proxy is important for cluster operations
      #   If node is under resource pressure, keep this pod running
      #   Lower-priority pods might be evicted instead
```

---

## Key Settings Explained

| Setting | Value | Why? |
|---------|-------|------|
| `hostNetwork` | `true` | See traffic to API VIP (node's IP) |
| `hostPort: 8888` | Bind to node port | nftables redirects 80 → 8888 |
| `nodeSelector: master` | Only masters | VIP floats between masters |
| `NET_ADMIN` | Capability | Modify nftables rules |
| `runAsNonRoot: false` | Run as root | nftables requires root |
| `cpu: 10m` | Minimal | Proxy is lightweight |
| `system-cluster-critical` | Priority | Keep running under pressure |

---

## Key Takeaways from File 7

1. **hostNetwork:** Essential for intercepting VIP traffic
2. **Master-only:** DaemonSet runs on all control plane nodes
3. **Security:** Minimal capabilities (only NET_ADMIN), no privilege escalation
4. **Resources:** Very lightweight (10m CPU, 32Mi memory)
5. **Environment Variables:** API_VIP and INGRESS_VIP injected by controller

---

# FILE 8 of 8: Deployment Orchestration

**File:** `pkg/controller/http01proxy/install_http01proxy.go`
**Learning Order:** #8 - FINAL FILE
**Purpose:** Orchestrates deployment of all resources
**What It Does:** Ties everything together - full deployment flow
**Time:** 10 minutes

---

## Annotated Code

```go
package http01proxy

import (
	"context"
	"fmt"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

// ============================================================================
// Main Deployment Function
// ============================================================================

// reconcileHTTP01ProxyDeployment orchestrates the deployment of all HTTP01 proxy resources
// This is called by the main Reconcile loop
func (r *Reconciler) reconcileHTTP01ProxyDeployment(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
	// STEP 1: Get platform information (cached after first call)
	platformInfo, err := r.getOrDiscoverPlatform(ctx)
	if err != nil {
		return fmt.Errorf("failed to discover platform: %w", err)
	}
	// platformInfo now contains:
	//   - platformType: "BareMetal"
	//   - apiVIPs: ["192.168.1.100"]
	//   - ingressVIPs: ["192.168.1.101"]

	// STEP 2: Validate platform
	if errMsg := validatePlatform(platformInfo); errMsg != "" {
		// Platform validation failed
		// Return error with the validation message
		// Controller will set Degraded status with this message
		// No resources will be deployed
		return fmt.Errorf(errMsg)
	}
	// Validation checks:
	//   ✓ Platform is BareMetal
	//   ✓ API VIPs exist
	//   ✓ Ingress VIPs exist
	//   ✓ VIPs are different

	// STEP 3: Deploy ServiceAccount
	if err := r.reconcileServiceAccount(ctx, proxy); err != nil {
		return fmt.Errorf("failed to reconcile ServiceAccount: %w", err)
	}
	// Creates: ServiceAccount/cert-manager-http01-proxy

	// STEP 4: Deploy RBAC resources
	if err := r.reconcileRBAC(ctx, proxy); err != nil {
		return fmt.Errorf("failed to reconcile RBAC: %w", err)
	}
	// Creates:
	//   - ClusterRole/cert-manager-http01-proxy
	//   - ClusterRoleBinding/cert-manager-http01-proxy (to ClusterRole)
	//   - ClusterRoleBinding/cert-manager-http01-proxy-privileged (to privileged SCC)

	// STEP 5: Deploy NetworkPolicies
	if err := r.reconcileNetworkPolicies(ctx, proxy); err != nil {
		return fmt.Errorf("failed to reconcile NetworkPolicies: %w", err)
	}
	// Creates:
	//   - NetworkPolicy/http01-proxy-deny-all (deny all ingress)
	//   - NetworkPolicy/http01-proxy-allow-egress (allow egress on 80, 443, 6443)

	// STEP 6: Deploy DaemonSet (THE KEY STEP!)
	if err := r.reconcileDaemonSet(ctx, proxy, platformInfo); err != nil {
		return fmt.Errorf("failed to reconcile DaemonSet: %w", err)
	}
	// Creates: DaemonSet/cert-manager-http01-proxy
	// ▼ THIS IS WHERE VIPs ARE INJECTED! ▼
	// DaemonSet pods will have environment variables:
	//   API_VIP=192.168.1.100
	//   INGRESS_VIP=192.168.1.101

	// ALL RESOURCES DEPLOYED SUCCESSFULLY!
	return nil
}
// If this function returns nil (no error):
//   - Status set to Available=True
//   - User sees: "HTTP01 proxy DaemonSet is deployed and running"
//
// If this function returns error:
//   - Status set to Degraded=True
//   - User sees error message in status

// ============================================================================
// Deployment Order Matters!
// ============================================================================
//
// Why this order?
//
// 1. ServiceAccount first
//    - DaemonSet needs SA to exist
//
// 2. RBAC second
//    - ServiceAccount needs to be bound to ClusterRole
//    - ServiceAccount needs privileged SCC binding
//
// 3. NetworkPolicies third
//    - Security controls in place before pods start
//
// 4. DaemonSet last
//    - All dependencies are ready
//    - Pods can start successfully
```

---

## Deployment Flow Visualization

```
┌─────────────────────────────────────────────────┐
│ 1. Discover Platform                            │
│    - Fetch Infrastructure/cluster               │
│    - Extract: platformType, apiVIPs, ingressVIPs│
│    - Cache result                                │
└──────────────────┬──────────────────────────────┘
                   ↓
┌─────────────────────────────────────────────────┐
│ 2. Validate Platform                            │
│    - Check: platform == BareMetal?              │
│    - Check: has API VIPs?                       │
│    - Check: has Ingress VIPs?                   │
│    - Check: VIPs different?                     │
│    - If fails → return error, no deployment     │
└──────────────────┬──────────────────────────────┘
                   ↓
┌─────────────────────────────────────────────────┐
│ 3. Deploy ServiceAccount                        │
│    - Name: cert-manager-http01-proxy            │
│    - Namespace: cert-manager-operator           │
└──────────────────┬──────────────────────────────┘
                   ↓
┌─────────────────────────────────────────────────┐
│ 4. Deploy RBAC                                  │
│    - ClusterRole                                │
│    - ClusterRoleBinding (to ClusterRole)        │
│    - ClusterRoleBinding (to privileged SCC)     │
└──────────────────┬──────────────────────────────┘
                   ↓
┌─────────────────────────────────────────────────┐
│ 5. Deploy NetworkPolicies                       │
│    - Deny all ingress                           │
│    - Allow egress (ports 80, 443, 6443)         │
└──────────────────┬──────────────────────────────┘
                   ↓
┌─────────────────────────────────────────────────┐
│ 6. Deploy DaemonSet                             │
│    - Load template from bindata                 │
│    - Inject: API_VIP=192.168.1.100              │
│    - Inject: INGRESS_VIP=192.168.1.101          │
│    - Create/Update DaemonSet                    │
└──────────────────┬──────────────────────────────┘
                   ↓
┌─────────────────────────────────────────────────┐
│ 7. Kubernetes schedules pods                    │
│    - One pod per master node                    │
│    - hostNetwork: true                          │
│    - Proxy container starts                     │
│    - Container reads env vars: API_VIP, INGRESS_VIP │
└─────────────────────────────────────────────────┘
```

---

## Key Takeaways from File 8

1. **Orchestration:** Ties all deployment functions together
2. **Order Matters:** ServiceAccount → RBAC → NetworkPolicies → DaemonSet
3. **Validation First:** Platform checked before any deployment
4. **Error Handling:** Any failure stops deployment, sets Degraded status
5. **VIP Passing:** platformInfo passed to reconcileDaemonSet for injection

---

# COMPLETE! Summary of the PR

## What Each File Does

| File | Role |
|------|------|
| **File 1:** API Types | Defines HTTP01Proxy CRD structure |
| **File 2:** Constants | Names, labels, annotations |
| **File 3:** Controller | Reconcile loop, watches, lifecycle |
| **File 4:** Infrastructure | Platform discovery & validation |
| **File 5:** Utils | Status management, conditions |
| **File 6:** DaemonSets | **VIP injection (KEY!)** |
| **File 7:** Template | DaemonSet YAML definition |
| **File 8:** Install | Orchestrates deployment |

## The Complete Flow

```
User creates HTTP01Proxy
    ↓
Controller reconciles
    ↓
Discover platform (cache it)
    ↓
Validate platform (BareMetal? Distinct VIPs?)
    ↓
Deploy ServiceAccount
    ↓
Deploy RBAC
    ↓
Deploy NetworkPolicies
    ↓
Deploy DaemonSet with VIPs injected
    ↓
Kubernetes schedules pods on masters
    ↓
Proxy containers start with env vars:
  - API_VIP=192.168.1.100
  - INGRESS_VIP=192.168.1.101
    ↓
Proxy intercepts port 80 on API VIP
    ↓
Forwards ACME requests to Ingress VIP
    ↓
ACME challenges succeed!
    ↓
Certificates issued! ✅
```

## The Key Insight

**This PR's job is to:**
1. Validate the platform
2. Deploy the proxy DaemonSet
3. **Tell the proxy which VIPs to use (via environment variables)**

**The proxy container (different repo) does the actual proxying work!**

---

# Questions for Your Team to Consider

1. Why is the singleton pattern important? (Only one HTTP01Proxy allowed)
2. Why cache platform information? (Performance)
3. Why use a DaemonSet instead of Deployment? (VIP failover)
4. Why does the proxy need hostNetwork? (Intercept VIP traffic)
5. Why pass VIPs as environment variables? (Decouple operator from proxy)
6. What happens if validation fails? (Degraded status, no deployment)
7. How does cleanup work? (Finalizer pattern)
8. Why watch child resources? (Drift detection)

---

**END OF WALKTHROUGH**

Now your team can walk through these 8 files in order and understand the entire PR!

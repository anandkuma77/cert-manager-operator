# HTTP01 Proxy - Code Walkthrough
## Hands-On Deep Dive into the Implementation

This document complements the main learning guide with detailed code walkthroughs you can follow along with.

---

## File-by-File Breakdown

### 1. API Types: The Data Model

**File: `api/operator/v1alpha1/http01proxy_types.go`**

This file defines the **shape of your data** - what users create and what the system tracks.

#### The Main Resource

```go
// HTTP01Proxy describes the configuration for the HTTP01 challenge proxy
// that redirects traffic from the API endpoint on port 80 to ingress routers.
type HTTP01Proxy struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   HTTP01ProxySpec   `json:"spec"`
    Status HTTP01ProxyStatus `json:"status,omitempty"`
}
```

**What's happening here?**

- `TypeMeta`: Contains `apiVersion` and `kind` (like `kind: HTTP01Proxy`)
- `ObjectMeta`: Contains `name`, `namespace`, `labels`, `annotations`, etc.
- `Spec`: User's desired state ("I want this configuration")
- `Status`: System's current state ("Here's what's actually running")

**Singleton Enforcement** (line 42):
```go
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default'",message="http01proxy is a singleton, .metadata.name must be 'default'"
```

This is a **CEL (Common Expression Language) validation**:
- `self.metadata.name == 'default'` - the name MUST be "default"
- If you try to create `myproxy` or `http01proxy-prod`, it will be rejected
- This prevents multiple instances from conflicting

**Try it yourself:**
```bash
# This works:
kubectl apply -f - <<EOF
apiVersion: operator.openshift.io/v1alpha1
kind: HTTP01Proxy
metadata:
  name: default
  namespace: cert-manager-operator
spec:
  mode: DefaultDeployment
EOF

# This FAILS:
kubectl apply -f - <<EOF
apiVersion: operator.openshift.io/v1alpha1
kind: HTTP01Proxy
metadata:
  name: my-proxy  # ❌ Validation error!
  namespace: cert-manager-operator
spec:
  mode: DefaultDeployment
EOF
```

#### The Spec

```go
type HTTP01ProxySpec struct {
    // mode controls whether the HTTP01 challenge proxy is active and how it should be deployed.
    Mode HTTP01ProxyMode `json:"mode"`

    // customDeployment contains configuration options when mode is CustomDeployment.
    CustomDeployment *HTTP01ProxyCustomDeploymentSpec `json:"customDeployment,omitempty"`
}
```

**Key observation:**
- Only 2 fields! This is intentionally minimal.
- `mode` is required (no `omitempty` tag)
- `customDeployment` is optional (pointer type with `omitempty`)

**Cross-field validation** (line 75):
```go
// +kubebuilder:validation:XValidation:rule="self.mode == 'CustomDeployment' ? has(self.customDeployment) : !has(self.customDeployment)",message="customDeployment is required when mode is CustomDeployment and forbidden otherwise"
```

Breaking this down:
```
IF mode == 'CustomDeployment':
    THEN customDeployment MUST be present
ELSE:
    customDeployment MUST NOT be present
```

**This prevents invalid configurations like:**
```yaml
spec:
  mode: DefaultDeployment
  customDeployment:  # ❌ Not allowed with DefaultDeployment
    internalPort: 9999
```

#### The Status

```go
type HTTP01ProxyStatus struct {
    // conditions holds information about the current state
    ConditionalStatus `json:",inline,omitempty"`

    // proxyImage is the name of the image and the tag used for deploying the proxy
    ProxyImage string `json:"proxyImage,omitempty"`
}
```

**What is ConditionalStatus?**
It's embedded from another type (likely in a common package). Let's see what it contains:

```go
// Somewhere in common code (not in this PR, but conceptually):
type ConditionalStatus struct {
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

**Conditions follow the Kubernetes standard:**
```yaml
status:
  conditions:
  - type: Available
    status: "True"
    reason: ProxyDeployed
    message: "HTTP01 proxy DaemonSet is running"
    lastTransitionTime: "2026-06-26T12:00:00Z"
  - type: Degraded
    status: "False"
    reason: AsExpected
  - type: Progressing
    status: "False"
    reason: AsExpected
  proxyImage: quay.io/bapalm/cert-mgr-http01-proxy:v0.2.0
```

**Why this pattern?**
- Consistent with other Kubernetes resources
- Tools like `kubectl wait` can watch conditions
- Clear troubleshooting path

---

### 2. Controller: The Brain

**File: `pkg/controller/http01proxy/controller.go`**

#### Reconciler Structure

```go
type Reconciler struct {
    common.CtrlClient  // Embedded - gives us Get(), Create(), Update(), Delete()

    eventRecorder record.EventRecorder  // For emitting k8s events
    log           logr.Logger            // For logging
    
    proxyImage string  // From env var: RELATED_IMAGE_CERT_MANAGER_HTTP01PROXY

    cachedPlatform *platformInfo  // Cached to avoid repeated API calls
    platformMu     sync.Mutex     // Protects cachedPlatform
}
```

**Why cache platform info?**
- Platform doesn't change during runtime
- Getting Infrastructure/cluster is an API call
- Reconcile might happen hundreds of times
- Cache once, reuse forever → performance win

**Thread safety:**
- Multiple reconciliation loops might run concurrently
- `sync.Mutex` ensures only one goroutine accesses `cachedPlatform` at a time
- Without this → **race condition!**

#### RBAC Markers

```go
// +kubebuilder:rbac:groups=operator.openshift.io,resources=http01proxies,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=http01proxies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=http01proxies/finalizers,verbs=update
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions;ingresses;infrastructures,verbs=get;list;watch
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,resourceNames=privileged,verbs=use
```

**What are these?**
- **Code annotations** that generate RBAC rules
- When you run `make update`, these become ClusterRole YAML
- Defines what permissions the operator needs

**Reading them:**
```
groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete

Translates to:
- Group: apps (DaemonSet is in apps/v1)
- Resource: daemonsets
- Verbs: get, list, watch, create, update, patch, delete
```

**Why does it need these specific permissions?**
- `http01proxies`: Manage the CR itself
- `daemonsets`: Create/update/delete the proxy DaemonSet
- `serviceaccounts`: Create SA for the proxy
- `clusterroles/clusterrolebindings`: Create RBAC for the proxy
- `networkpolicies`: Create NetworkPolicies
- `infrastructures`: Read platform info (API/Ingress VIPs)
- `securitycontextconstraints,resourceNames=privileged`: Bind proxy to privileged SCC

#### Setup with Manager

```go
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
    mapFunc := func(ctx context.Context, obj client.Object) []reconcile.Request {
        r.log.V(4).Info("received reconcile event", "object", fmt.Sprintf("%T", obj), "name", obj.GetName())

        objLabels := obj.GetLabels()
        if objLabels != nil && objLabels[common.ManagedResourceLabelKey] == RequestEnqueueLabelValue {
            namespace := obj.GetNamespace()
            if namespace == "" {
                namespace = common.OperatorNamespace
            }
            return []reconcile.Request{
                {
                    NamespacedName: types.NamespacedName{
                        Name:      http01proxyObjectName,      // "default"
                        Namespace: namespace,
                    },
                },
            }
        }

        r.log.V(4).Info("object not of interest, ignoring", "object", fmt.Sprintf("%T", obj), "name", obj.GetName())
        return []reconcile.Request{}
    }

    // ... predicates and watches ...
```

**What is mapFunc?**
- A **mapping function** that translates "some resource changed" into "which HTTP01Proxy should we reconcile?"
- Example flow:
  1. DaemonSet `cert-manager-http01-proxy` is updated
  2. Kubernetes notifies the controller
  3. `mapFunc` is called with the DaemonSet object
  4. It checks: does this DaemonSet have the right label?
  5. If yes → enqueue reconcile request for `HTTP01Proxy/default`
  6. If no → ignore

**Why this pattern?**
- Controller watches multiple resource types (DaemonSet, ServiceAccount, ClusterRole, etc.)
- When any changes, we need to reconcile the HTTP01Proxy
- But we don't want to reconcile for *every* DaemonSet in the cluster
- **Solution:** Label our managed resources, filter on that label

**The label check:**
```go
if objLabels != nil && objLabels[common.ManagedResourceLabelKey] == RequestEnqueueLabelValue
```

Where:
- `common.ManagedResourceLabelKey` = `"operator.openshift.io/managed-resource"`
- `RequestEnqueueLabelValue` = `"http01proxy"` (the constant `http01proxyCommonName`)

So managed resources will have:
```yaml
metadata:
  labels:
    operator.openshift.io/managed-resource: "http01proxy"
```

#### The Watches

```go
return ctrl.NewControllerManagedBy(mgr).
    For(&v1alpha1.HTTP01Proxy{}).  // Primary resource
    Named(ControllerName).
    Watches(&appsv1.DaemonSet{}, handler.EnqueueRequestsFromMapFunc(mapFunc), withIgnoreStatusUpdatePredicates).
    Watches(&rbacv1.ClusterRole{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
    Watches(&rbacv1.ClusterRoleBinding{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
    Watches(&corev1.ServiceAccount{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
    Watches(&networkingv1.NetworkPolicy{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
    Complete(r)
```

**What this means:**
```
Reconcile HTTP01Proxy when:
1. The HTTP01Proxy itself changes (For)
2. A managed DaemonSet changes (Watches)
3. A managed ClusterRole changes (Watches)
4. A managed ClusterRoleBinding changes (Watches)
5. A managed ServiceAccount changes (Watches)
6. A managed NetworkPolicy changes (Watches)
```

**Why watch child resources?**
- **Drift detection:** If someone manually deletes the DaemonSet, controller recreates it
- **Ownership:** Controller "owns" these resources, should keep them in sync
- **Declarative:** User declares desired state, controller ensures actual state matches

**Predicates:**
- `withIgnoreStatusUpdatePredicates`: Don't reconcile on status-only changes (efficiency)
- `controllerManagedResourcePredicates`: Only watch resources with our label

#### The Reconcile Function

```go
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    r.log.V(1).Info("reconciling", "request", req)

    // 1. Namespace check
    if req.Namespace != common.OperatorNamespace {
        r.log.V(1).Info("ignoring http01proxy in unexpected namespace", "namespace", req.Namespace, "expected", common.OperatorNamespace)
        return ctrl.Result{}, nil
    }

    // 2. Fetch the HTTP01Proxy
    proxy := &v1alpha1.HTTP01Proxy{}
    if err := r.Get(ctx, req.NamespacedName, proxy); err != nil {
        if errors.IsNotFound(err) {
            r.log.V(1).Info("http01proxy object not found, skipping reconciliation", "request", req)
            return ctrl.Result{}, nil  // Already deleted, nothing to do
        }
        return ctrl.Result{}, fmt.Errorf("failed to fetch http01proxy %q: %w", req.NamespacedName, err)
    }

    // 3. Handle deletion
    if !proxy.DeletionTimestamp.IsZero() {
        r.log.V(1).Info("http01proxy is marked for deletion", "namespace", req.NamespacedName)

        if err := r.cleanUp(ctx, proxy); err != nil {
            return ctrl.Result{}, fmt.Errorf("clean up failed: %w", err)
        }

        if err := r.removeFinalizer(ctx, proxy); err != nil {
            return ctrl.Result{}, err
        }

        r.log.V(1).Info("removed finalizer, cleanup complete", "request", req.NamespacedName)
        return ctrl.Result{}, nil
    }

    // 4. Add finalizer if not present
    if err := r.addFinalizer(ctx, proxy); err != nil {
        return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
    }

    // 5. Process the actual reconciliation
    return r.processReconcileRequest(ctx, proxy, req.NamespacedName)
}
```

**Step-by-step breakdown:**

**Step 1: Namespace check**
```go
if req.Namespace != common.OperatorNamespace {
    return ctrl.Result{}, nil
}
```
- `common.OperatorNamespace` = `"cert-manager-operator"`
- HTTP01Proxy must be in this namespace
- If someone creates it elsewhere → ignore it

**Step 2: Fetch resource**
```go
proxy := &v1alpha1.HTTP01Proxy{}
if err := r.Get(ctx, req.NamespacedName, proxy); err != nil {
    if errors.IsNotFound(err) {
        return ctrl.Result{}, nil  // Already deleted
    }
    return ctrl.Result{}, err  // Real error
}
```

**Why check IsNotFound?**
- Reconcile might be triggered after deletion
- Resource is gone → nothing to do → success
- Other errors (network, permissions) → return error to retry

**Step 3: Deletion handling**
```go
if !proxy.DeletionTimestamp.IsZero() {
```
- When user runs `kubectl delete http01proxy default`
- Kubernetes doesn't delete immediately
- It sets `deletionTimestamp` to current time
- Controller gets a chance to clean up
- Then finalizer is removed
- Then Kubernetes actually deletes the object

**Step 4: Add finalizer**
```go
if err := r.addFinalizer(ctx, proxy); err != nil {
    return ctrl.Result{}, err
}
```
- Finalizer is like a "lock" on the object
- Prevents deletion until controller is done cleaning up
- Controller adds it on first reconcile
- Removes it after cleanup

#### The Cleanup Function

```go
func (r *Reconciler) cleanUp(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
    r.log.V(1).Info("cleaning up http01proxy resources", "namespace", proxy.GetNamespace(), "name", proxy.GetName())
    r.eventRecorder.Eventf(proxy, corev1.EventTypeNormal, "CleanUp", "cleaning up resources for http01proxy %s/%s", proxy.GetNamespace(), proxy.GetName())

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

**What it does:**
1. Delete DaemonSet
2. Delete ServiceAccount
3. Delete ClusterRole and ClusterRoleBindings
4. Delete NetworkPolicies

**Why explicit deletion?**
- These are cluster-scoped resources (ClusterRole, ClusterRoleBinding)
- They don't have owner references (can't be set across namespaces)
- Must be deleted manually

**What about owner references?**
- For namespace-scoped resources (DaemonSet, ServiceAccount, NetworkPolicy)
- Controller sets `ownerReferences` pointing to HTTP01Proxy
- Kubernetes garbage collector *could* delete them automatically
- But explicit deletion is clearer and more reliable

---

### 3. Platform Detection: The Gatekeeper

**File: `pkg/controller/http01proxy/infrastructure.go`**

#### Platform Info Structure

```go
type platformInfo struct {
    platformType string
    apiVIPs      []string
    ingressVIPs  []string
}
```

**Why slices for VIPs?**
- Dual-stack networking (IPv4 + IPv6)
- High availability configurations
- Future extensibility

**Example:**
```go
platformInfo{
    platformType: "BareMetal",
    apiVIPs:      []string{"192.168.1.100", "fd00::100"},  // IPv4 + IPv6
    ingressVIPs:  []string{"192.168.1.101", "fd00::101"},
}
```

#### Caching with Mutex

```go
func (r *Reconciler) getOrDiscoverPlatform(ctx context.Context) (*platformInfo, error) {
    r.platformMu.Lock()
    defer r.platformMu.Unlock()
    
    if r.cachedPlatform != nil {
        return r.cachedPlatform, nil  // Return cached
    }
    
    info, err := r.discoverPlatform(ctx)
    if err != nil {
        return nil, err
    }
    
    r.cachedPlatform = info  // Cache it
    return info, nil
}
```

**The mutex pattern:**
```go
r.platformMu.Lock()       // Acquire lock
defer r.platformMu.Unlock()  // Release lock when function returns
```

**Why defer?**
- Ensures unlock happens even if function panics
- Cleaner code (don't need unlock before every return)

**Race condition example without mutex:**
```
Goroutine 1:                  Goroutine 2:
if r.cachedPlatform != nil {  if r.cachedPlatform != nil {
    // nil, so continue             // nil, so continue
}                             }
info = discoverPlatform()     info = discoverPlatform()
r.cachedPlatform = info       r.cachedPlatform = info
```
Both goroutines call `discoverPlatform()` → wasteful!

**With mutex:**
```
Goroutine 1:                  Goroutine 2:
Lock()                        Lock() ← blocks, waiting
if r.cachedPlatform != nil {
    // nil
}
info = discoverPlatform()
r.cachedPlatform = info
Unlock()                      ← now acquires lock
                              if r.cachedPlatform != nil {
                                  return cached ← yay!
                              }
```

#### Discovery Logic

```go
func (r *Reconciler) discoverPlatform(ctx context.Context) (*platformInfo, error) {
    // 1. Get Infrastructure/cluster using unstructured API
    infra := &unstructured.Unstructured{}
    infra.SetGroupVersionKind(infrastructureGVK)

    if err := r.Get(ctx, types.NamespacedName{Name: "cluster"}, infra); err != nil {
        return nil, fmt.Errorf("failed to get infrastructure/cluster: %w", err)
    }

    // 2. Extract platform type
    platformType, found, err := unstructured.NestedString(infra.Object, "status", "platformStatus", "type")
    if err != nil {
        return nil, fmt.Errorf("failed to parse infrastructure status.platformStatus.type: %w", err)
    }
    if !found {
        return nil, fmt.Errorf("infrastructure status.platformStatus.type not found")
    }

    info := &platformInfo{
        platformType: platformType,
    }

    // 3. If BareMetal, extract VIPs
    switch platformType {
    case platformBareMetal:
        apiVIPs, _, err := unstructured.NestedStringSlice(infra.Object, "status", "platformStatus", "baremetal", "apiServerInternalIPs")
        if err != nil {
            return nil, fmt.Errorf("failed to parse baremetal.apiServerInternalIPs: %w", err)
        }
        ingressVIPs, _, err := unstructured.NestedStringSlice(infra.Object, "status", "platformStatus", "baremetal", "ingressIPs")
        if err != nil {
            return nil, fmt.Errorf("failed to parse baremetal.ingressIPs: %w", err)
        }
        info.apiVIPs = apiVIPs
        info.ingressVIPs = ingressVIPs
    }

    return info, nil
}
```

**Why unstructured.Unstructured?**
- Infrastructure CRD is from `config.openshift.io`
- Not part of this module's API types
- Could import it, but that adds dependency
- **unstructured** lets us work with it without typed structs

**How NestedString works:**
```go
unstructured.NestedString(infra.Object, "status", "platformStatus", "type")

// Equivalent to accessing a nested map:
obj["status"]["platformStatus"]["type"]

// Returns: (value string, found bool, err error)
```

**Example Infrastructure object:**
```yaml
apiVersion: config.openshift.io/v1
kind: Infrastructure
metadata:
  name: cluster
status:
  platformStatus:
    type: BareMetal
    baremetal:
      apiServerInternalIPs:
      - 192.168.1.100
      ingressIPs:
      - 192.168.1.101
```

#### Validation Logic

```go
func validatePlatform(info *platformInfo) string {
    // Check 1: Must be BareMetal
    if info.platformType != platformBareMetal {
        return fmt.Sprintf("platform type %q is not supported; HTTP01 proxy is only supported on BareMetal platforms", info.platformType)
    }

    // Check 2: Must have API VIPs
    if len(info.apiVIPs) == 0 {
        return "no API server VIPs found in infrastructure status; cannot deploy HTTP01 proxy"
    }

    // Check 3: Must have Ingress VIPs
    if len(info.ingressVIPs) == 0 {
        return "no ingress VIPs found in infrastructure status; cannot deploy HTTP01 proxy"
    }

    // Check 4: VIPs must be different
    for _, apiVIP := range info.apiVIPs {
        for _, ingressVIP := range info.ingressVIPs {
            if apiVIP == ingressVIP {
                return fmt.Sprintf("API VIP (%s) and ingress VIP (%s) are the same; HTTP01 proxy is not needed", apiVIP, ingressVIP)
            }
        }
    }

    // All checks passed
    return ""
}
```

**Return pattern:**
- Returns empty string `""` if valid
- Returns human-readable error message if invalid
- Caller checks: `if msg != "" { /* degraded */ }`

**Why this pattern instead of returning error?**
- Error messages become status condition messages
- Want friendly user-facing text
- Don't want stack traces in status

**The nested loop:**
```go
for _, apiVIP := range info.apiVIPs {
    for _, ingressVIP := range info.ingressVIPs {
        if apiVIP == ingressVIP {
            return "..."
        }
    }
}
```

Checks **all combinations**:
- apiVIPs = [A, B]
- ingressVIPs = [X, Y]
- Checks: A==X, A==Y, B==X, B==Y

If **any** pair matches → VIPs are not distinct → proxy not needed

---

## Key Patterns & Idioms

### 1. The Finalizer Pattern

**Lifecycle:**
```
User creates HTTP01Proxy
    ↓
Controller adds finalizer
    ↓
[Normal operation - resource exists]
    ↓
User deletes HTTP01Proxy
    ↓
Kubernetes sets deletionTimestamp
    ↓
Object still exists (finalizer prevents deletion)
    ↓
Controller sees deletionTimestamp != nil
    ↓
Controller cleans up child resources
    ↓
Controller removes finalizer
    ↓
Kubernetes deletes HTTP01Proxy
```

**Code pattern:**
```go
const finalizerName = "operator.openshift.io/http01proxy"

func (r *Reconciler) addFinalizer(ctx context.Context, obj *v1alpha1.HTTP01Proxy) error {
    if !controllerutil.ContainsFinalizer(obj, finalizerName) {
        controllerutil.AddFinalizer(obj, finalizerName)
        return r.Update(ctx, obj)
    }
    return nil
}

func (r *Reconciler) removeFinalizer(ctx context.Context, obj *v1alpha1.HTTP01Proxy) error {
    if controllerutil.ContainsFinalizer(obj, finalizerName) {
        controllerutil.RemoveFinalizer(obj, finalizerName)
        return r.Update(ctx, obj)
    }
    return nil
}
```

### 2. The Reconcile Loop

**"Level-triggered" vs "Edge-triggered":**

**Edge-triggered (bad):**
```go
// Anti-pattern
func Reconcile(req Request) {
    if event.Type == "CREATED" {
        createDaemonSet()
    } else if event.Type == "UPDATED" {
        updateDaemonSet()
    } else if event.Type == "DELETED" {
        deleteDaemonSet()
    }
}
```
**Problem:** What if controller was down during CREATE? Misses it forever.

**Level-triggered (good):**
```go
// Best practice
func Reconcile(req Request) {
    desired := computeDesiredState()
    actual := getCurrentState()
    reconcile(desired, actual)  // Make actual match desired
}
```
**Benefits:**
- Self-healing
- Idempotent
- Controller can crash and recover

### 3. Condition Management

**Standard condition types:**
```go
const (
    ConditionAvailable   = "Available"
    ConditionDegraded    = "Degraded"
    ConditionProgressing = "Progressing"
)
```

**Meaning:**
- **Available=True**: Feature is working
- **Degraded=True**: Something is wrong
- **Progressing=True**: Change in progress

**Combinations:**
```
Available=True, Degraded=False, Progressing=False
→ Healthy, stable

Available=False, Degraded=True, Progressing=False
→ Broken, stable (won't fix itself)

Available=True, Degraded=False, Progressing=True
→ Healthy but updating

Available=False, Degraded=True, Progressing=True
→ Broken, trying to fix
```

### 4. Event Recording

```go
r.eventRecorder.Eventf(
    proxy,                      // Object the event is about
    corev1.EventTypeNormal,     // Type: Normal or Warning
    "Reconciling",              // Reason (short string)
    "Reconciling HTTP01Proxy",  // Message (human-readable)
)
```

**Shows up in:**
```bash
$ kubectl describe http01proxy default
Events:
  Type    Reason       Age   From                Message
  ----    ------       ----  ----                -------
  Normal  Reconciling  1m    http01proxy-controller  Reconciling HTTP01Proxy
  Normal  Available    1m    http01proxy-controller  DaemonSet deployed successfully
```

---

## Debugging Walkthrough

### Scenario: DaemonSet Not Deploying

**Symptoms:**
```bash
$ kubectl get http01proxy default -o yaml
status:
  conditions:
  - type: Degraded
    status: "True"
    reason: ReconciliationFailed
    message: "failed to reconcile daemonset: ..."
```

**Debug steps:**

**1. Check controller logs:**
```bash
kubectl logs -n cert-manager-operator deployment/cert-manager-operator-controller-manager
```

Look for:
```
ERROR reconciling HTTP01Proxy default: failed to create daemonset: ...
```

**2. Check RBAC:**
```bash
# Does the operator SA have permissions?
kubectl auth can-i create daemonsets \
    --as system:serviceaccount:cert-manager-operator:cert-manager-operator-controller-manager \
    -n cert-manager-operator
```

**3. Check the desired state:**
```go
// In code: what is the desired DaemonSet?
// Add debug logging:
r.log.Info("desired daemonset", "daemonset", desired)
```

**4. Check for drift:**
```bash
# Is there an existing DaemonSet with different ownership?
kubectl get daemonset cert-manager-http01-proxy -n cert-manager-operator -o yaml
```

---

## Practice Exercises

### Exercise 1: Add a Field

**Task:** Add a `logLevel` field to HTTP01ProxySpec

**Steps:**
1. Edit `api/operator/v1alpha1/http01proxy_types.go`:
```go
type HTTP01ProxySpec struct {
    Mode HTTP01ProxyMode `json:"mode"`
    CustomDeployment *HTTP01ProxyCustomDeploymentSpec `json:"customDeployment,omitempty"`
    
    // NEW: LogLevel controls logging verbosity (0-5)
    // +kubebuilder:validation:Minimum=0
    // +kubebuilder:validation:Maximum=5
    // +kubebuilder:default=1
    // +optional
    LogLevel int `json:"logLevel,omitempty"`
}
```

2. Run `make update` to regenerate deepcopy methods

3. Update controller to use it (in daemonsets.go):
```go
container.Args = append(container.Args, fmt.Sprintf("--log-level=%d", proxy.Spec.LogLevel))
```

4. Update CRD:
```bash
make manifests
kubectl apply -f config/crd/bases/operator.openshift.io_http01proxies.yaml
```

### Exercise 2: Add Platform Support

**Task:** Support AWS platform (where API/Ingress use ALBs)

**Steps:**
1. Edit `infrastructure.go`:
```go
const (
    platformBareMetal = "BareMetal"
    platformAWS       = "AWS"  // NEW
)

func validatePlatform(info *platformInfo) string {
    switch info.platformType {
    case platformBareMetal:
        // existing validation
    case platformAWS:
        // AWS-specific validation
        return ""  // For now, just allow it
    default:
        return fmt.Sprintf("platform %q not supported", info.platformType)
    }
}
```

2. Add AWS-specific VIP discovery in `discoverPlatform()`

### Exercise 3: Add Metrics

**Task:** Export Prometheus metrics for reconciliation

**Steps:**
1. Import prometheus:
```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "sigs.k8s.io/controller-runtime/pkg/metrics"
)
```

2. Define metrics:
```go
var (
    reconcileCount = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "http01proxy_reconcile_total",
            Help: "Total reconciliations",
        },
        []string{"result"},  // success or error
    )
)

func init() {
    metrics.Registry.MustRegister(reconcileCount)
}
```

3. Increment in Reconcile:
```go
func (r *Reconciler) Reconcile(...) (ctrl.Result, error) {
    result, err := r.processReconcile(...)
    if err != nil {
        reconcileCount.WithLabelValues("error").Inc()
    } else {
        reconcileCount.WithLabelValues("success").Inc()
    }
    return result, err
}
```

---

## Summary

You've now walked through:
- ✅ API types and validation
- ✅ Controller structure and RBAC
- ✅ Reconciliation loop and finalizers
- ✅ Platform detection and caching
- ✅ Common patterns and idioms
- ✅ Debugging approaches
- ✅ Extension exercises

**Next steps:**
1. Read the actual PR code with this guide open
2. Try the practice exercises
3. Run the operator locally and step through with a debugger
4. Present to your team with confidence!

---

*Happy coding!* 🚀

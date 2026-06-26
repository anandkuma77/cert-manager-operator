# HTTP01 Proxy - Hands-On Lab Exercise
## Interactive Learning Through Code Exploration

This lab helps you learn by doing. Follow along to explore the actual code and understand how it works.

---

## Prerequisites

- ✅ You're in the `cert-manager-operator` directory
- ✅ You're on the `cm-716-http01-proxy` branch
- ✅ You have a code editor open
- ✅ You've read at least the Executive Summary from the Learning Guide

---

## Lab 1: Exploring the API Types (15 minutes)

### Objective
Understand the data model and validation rules.

### Steps

**1. Open the API types file:**
```bash
code api/operator/v1alpha1/http01proxy_types.go
# Or use your preferred editor
```

**2. Find the HTTP01Proxy struct (around line 44)**
```go
type HTTP01Proxy struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec   HTTP01ProxySpec   `json:"spec"`
    Status HTTP01ProxyStatus `json:"status,omitempty"`
}
```

**Question:** Why is Status marked with `omitempty` but Spec is not?

<details>
<summary>Answer</summary>

Spec is required (users must provide it), while Status is managed by the controller and initially empty when the resource is created.
</details>

**3. Look at the singleton validation (line 42)**
```go
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default'",message="..."
```

**Task:** Try to understand this CEL expression.
- `self` = the HTTP01Proxy object being created
- `self.metadata.name` = its name
- `== 'default'` = must equal "default"

**4. Find the cross-field validation (line 75)**
```go
// +kubebuilder:validation:XValidation:rule="self.mode == 'CustomDeployment' ? has(self.customDeployment) : !has(self.customDeployment)"
```

**Task:** Break this down:
- IF mode is CustomDeployment
- THEN customDeployment MUST exist
- ELSE customDeployment MUST NOT exist

**5. Check the Status structure (line 102)**
```go
type HTTP01ProxyStatus struct {
    ConditionalStatus `json:",inline,omitempty"`
    ProxyImage string `json:"proxyImage,omitempty"`
}
```

**Question:** What does `json:",inline,omitempty"` mean?

<details>
<summary>Answer</summary>

The `inline` tag means the fields of ConditionalStatus are embedded directly into HTTP01ProxyStatus in the JSON output, not nested. So instead of `status.conditionalStatus.conditions`, you get `status.conditions`.
</details>

---

## Lab 2: Tracing the Controller (20 minutes)

### Objective
Follow the reconciliation flow through the code.

### Steps

**1. Open the controller:**
```bash
code pkg/controller/http01proxy/controller.go
```

**2. Find the Reconcile function (line 114)**

**Task:** Add mental breakpoints at these key points:
- Line 117: Namespace check
- Line 122: Fetch HTTP01Proxy
- Line 131: Deletion check
- Line 146: Add finalizer
- Line 150: Process reconciliation

**3. Follow the deletion path (line 131-144)**

Read the `cleanUp` function (line 175).

**Task:** List what gets deleted in order:
1. ________________
2. ________________
3. ________________
4. ________________

<details>
<summary>Answer</summary>

1. DaemonSet
2. ServiceAccount
3. RBAC Resources (ClusterRole, ClusterRoleBindings)
4. NetworkPolicies
</details>

**4. Find the watches setup (line 71)**

```go
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
```

**Task:** Count how many resource types are watched:
- HTTP01Proxy itself: ____
- DaemonSet: ____
- ClusterRole: ____
- ClusterRoleBinding: ____
- ServiceAccount: ____
- NetworkPolicy: ____

Total: ____

<details>
<summary>Answer</summary>

6 types total (1 primary + 5 secondary)
</details>

**5. Understand the mapFunc (line 72)**

**Question:** Why do we need a mapping function?

<details>
<summary>Answer</summary>

When a child resource (like DaemonSet) changes, we need to know which HTTP01Proxy to reconcile. The mapFunc translates "DaemonSet X changed" into "reconcile HTTP01Proxy default".
</details>

---

## Lab 3: Platform Detection Deep Dive (15 minutes)

### Objective
Understand how the controller validates platforms.

### Steps

**1. Open the infrastructure file:**
```bash
code pkg/controller/http01proxy/infrastructure.go
```

**2. Study the platformInfo struct (line 18)**

**Task:** What three pieces of information does it hold?
1. ________________
2. ________________
3. ________________

<details>
<summary>Answer</summary>

1. platformType
2. apiVIPs (slice)
3. ingressVIPs (slice)
</details>

**3. Find the caching mechanism (line 25)**

```go
func (r *Reconciler) getOrDiscoverPlatform(ctx context.Context) (*platformInfo, error) {
    r.platformMu.Lock()
    defer r.platformMu.Unlock()
    if r.cachedPlatform != nil {
        return r.cachedPlatform, nil  // Cache hit!
    }
    // Cache miss, discover it
```

**Question:** Why use a mutex here?

<details>
<summary>Answer</summary>

Multiple reconciliation loops might run concurrently (different goroutines). The mutex ensures only one goroutine accesses `cachedPlatform` at a time, preventing race conditions.
</details>

**4. Read the validation function (line 79)**

**Task:** List all validation checks:
1. ________________
2. ________________
3. ________________
4. ________________

<details>
<summary>Answer</summary>

1. Platform type must be BareMetal
2. Must have at least one API VIP
3. Must have at least one Ingress VIP
4. No API VIP can equal any Ingress VIP
</details>

**5. Trace the unstructured API usage (line 40)**

```go
infra := &unstructured.Unstructured{}
infra.SetGroupVersionKind(infrastructureGVK)
```

**Question:** Why use unstructured instead of typed structs?

<details>
<summary>Answer</summary>

Infrastructure is from `config.openshift.io`, not part of this module. Using unstructured avoids adding that dependency and works with any version.
</details>

---

## Lab 4: Examining Deployment Manifests (15 minutes)

### Objective
Understand what actually gets deployed.

### Steps

**1. Open the DaemonSet manifest:**
```bash
code bindata/http01-proxy/cert-manager-http01-proxy-daemonset.yaml
```

**2. Find critical settings:**

**Task:** Fill in the blanks:

```yaml
hostNetwork: ____  # Line 24
nodeSelector:
  node-role.kubernetes.io/master: ____  # Line 26

securityContext:
  capabilities:
    add:
      - ____  # Line 49
  runAsNonRoot: ____  # Line 52
```

<details>
<summary>Answer</summary>

```yaml
hostNetwork: true
nodeSelector:
  node-role.kubernetes.io/master: ""
securityContext:
  capabilities:
    add:
      - NET_ADMIN
  runAsNonRoot: false
```
</details>

**3. Check resource limits (line 54)**

**Question:** Why such low resource requests (10m CPU, 32Mi RAM)?

<details>
<summary>Answer</summary>

The proxy is very lightweight - it just checks paths and forwards requests. Minimal resource usage ensures it doesn't impact the master node's capacity.
</details>

**4. Find the priority class (line 60)**

```yaml
priorityClassName: system-cluster-critical
```

**Question:** What does this mean?

<details>
<summary>Answer</summary>

This Pod has high priority. If the cluster is under resource pressure, lower-priority Pods might be evicted, but this one stays running because it's critical for cluster operations.
</details>

**5. Open NetworkPolicy:**
```bash
code bindata/networkpolicies/http01-proxy-deny-all-networkpolicy.yaml
```

**Task:** What does this policy do?

<details>
<summary>Answer</summary>

Denies all ingress traffic to the proxy Pods. The proxy doesn't need incoming connections - it only makes outgoing connections to the Ingress VIP.
</details>

---

## Lab 5: Understanding RBAC (10 minutes)

### Objective
Learn what permissions the operator and proxy need.

### Steps

**1. Open the controller file again:**
```bash
code pkg/controller/http01proxy/controller.go
```

**2. Find the RBAC markers (lines 46-54)**

```go
// +kubebuilder:rbac:groups=operator.openshift.io,resources=http01proxies,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=http01proxies/status,verbs=get;update;patch
// ... more lines
```

**Task:** For each permission, explain why it's needed:

| Resource | Verbs | Why? |
|----------|-------|------|
| http01proxies | get;list;watch;update;patch | ________________ |
| daemonsets | get;list;watch;create;update;patch;delete | ________________ |
| infrastructures | get;list;watch | ________________ |
| securitycontextconstraints | use | ________________ |

<details>
<summary>Answer</summary>

| Resource | Verbs | Why? |
|----------|-------|------|
| http01proxies | get;list;watch;update;patch | Manage the HTTP01Proxy resource itself (read and update) |
| daemonsets | get;list;watch;create;update;patch;delete | Deploy and manage the proxy DaemonSet |
| infrastructures | get;list;watch | Read platform info to validate bare-metal requirements |
| securitycontextconstraints | use | Bind proxy ServiceAccount to privileged SCC (required for hostNetwork + NET_ADMIN) |
</details>

**3. Open the ClusterRole:**
```bash
code bindata/http01-proxy/cert-manager-http01-proxy-clusterrole.yaml
```

**Question:** Why is it empty?

<details>
<summary>Answer</summary>

The proxy Pod doesn't need to access the Kubernetes API. It just forwards HTTP requests. The ClusterRole exists for consistency and potential future use, but grants no permissions.
</details>

---

## Lab 6: Simulating Reconciliation (20 minutes)

### Objective
Mentally trace what happens during reconciliation.

### Scenario
User creates this resource:
```yaml
apiVersion: operator.openshift.io/v1alpha1
kind: HTTP01Proxy
metadata:
  name: default
  namespace: cert-manager-operator
spec:
  mode: DefaultDeployment
```

On a bare-metal cluster with:
- Platform: BareMetal
- API VIP: 192.168.1.100
- Ingress VIP: 192.168.1.101

### Steps

**1. Initial Creation**

**Task:** Trace through what happens (use controller.go):

Step 1: Reconcile triggered (line 114)
- Namespace check: ________________ (passes)
- Fetch HTTP01Proxy: ________________ (succeeds)
- DeletionTimestamp: ________________ (nil)

Step 2: Add finalizer (line 146)
- Contains finalizer? ________________ (no)
- Action: ________________

<details>
<summary>Answer</summary>

- Namespace check: cert-manager-operator (passes)
- Fetch HTTP01Proxy: succeeds
- DeletionTimestamp: nil (not being deleted)
- Contains finalizer? no
- Action: Add finalizer, update resource, requeue
</details>

**2. Second Reconciliation (after finalizer added)**

**Task:** What happens in `processReconcileRequest`?

Step 1: Platform discovery (infrastructure.go line 25)
- First call? ________________
- Cache hit? ________________
- Action: ________________

Step 2: Platform validation (infrastructure.go line 79)
- Platform type: ________________
- API VIPs: ________________
- Ingress VIPs: ________________
- Validation result: ________________

<details>
<summary>Answer</summary>

- First call? yes
- Cache hit? no (not cached yet)
- Action: Fetch Infrastructure/cluster, parse VIPs, cache result
- Platform type: BareMetal ✅
- API VIPs: [192.168.1.100] ✅
- Ingress VIPs: [192.168.1.101] ✅
- Validation result: OK (VIPs are different)
</details>

**3. Resource Deployment**

**Task:** What gets created? (In order, from install_http01proxy.go)

1. ________________
2. ________________
3. ________________
4. ________________

<details>
<summary>Answer</summary>

1. ServiceAccount
2. RBAC (ClusterRole, ClusterRoleBindings)
3. NetworkPolicies
4. DaemonSet
</details>

**4. Status Update**

**Task:** What conditions get set?

| Condition | Status | Reason |
|-----------|--------|--------|
| Available | ____ | ____ |
| Degraded | ____ | ____ |
| Progressing | ____ | ____ |

<details>
<summary>Answer</summary>

| Condition | Status | Reason |
|-----------|--------|--------|
| Available | True | HTTP01ProxyDeployed |
| Degraded | False | AsExpected |
| Progressing | False | AsExpected |
</details>

**5. Deletion**

**Task:** User runs `kubectl delete http01proxy default`. What happens?

Step 1: Kubernetes sets ________________
Step 2: Reconcile sees ________________ (line 131)
Step 3: cleanUp() deletes (line 175):
  - ________________
  - ________________
  - ________________
  - ________________
Step 4: ________________ removed (line 138)
Step 5: Kubernetes deletes the object

<details>
<summary>Answer</summary>

- Step 1: deletionTimestamp to current time
- Step 2: deletionTimestamp is not zero
- Step 3: DaemonSet, ServiceAccount, RBAC, NetworkPolicies
- Step 4: Finalizer removed
- Step 5: Object deleted
</details>

---

## Lab 7: Debugging Exercise (15 minutes)

### Objective
Practice troubleshooting common issues.

### Scenario 1: Platform Not Supported

**Given:** User creates HTTP01Proxy on AWS cluster

**Task:** Trace what happens:
1. Platform discovery returns: platformType = ________________
2. Validation checks: ________________
3. Result: ________________
4. Status condition set: ________________

<details>
<summary>Answer</summary>

1. platformType = "AWS"
2. Validation checks: `platformType != "BareMetal"`
3. Result: Validation fails with "platform type AWS is not supported"
4. Status condition set: Degraded=True with reason "UnsupportedPlatform"
</details>

**Question:** Does the controller deploy the DaemonSet?

<details>
<summary>Answer</summary>

No! When validation fails, the controller sets Degraded status and returns without deploying resources. This prevents unnecessary resource creation on unsupported platforms.
</details>

### Scenario 2: Missing VIP

**Given:** Bare-metal cluster but Ingress VIP is not set

**Task:** What happens?
1. Platform type: ________________
2. API VIPs: ________________
3. Ingress VIPs: ________________
4. Validation result: ________________

<details>
<summary>Answer</summary>

1. Platform type: BareMetal ✅
2. API VIPs: [192.168.1.100] ✅
3. Ingress VIPs: [] (empty slice) ❌
4. Validation fails: "no ingress VIPs found"
</details>

### Scenario 3: Same VIPs

**Given:** Both VIPs are 192.168.1.100

**Task:** Is this valid?

<details>
<summary>Answer</summary>

No! The validation checks if any API VIP equals any Ingress VIP. If they're the same, the proxy isn't needed - ACME requests already reach the Ingress controller.
</details>

---

## Lab 8: Code Modification Exercise (20 minutes)

### Objective
Make a small change to understand how the system works.

### Exercise 1: Add a Log Message

**Task:** Add a log message when platform is cached.

**File:** `pkg/controller/http01proxy/infrastructure.go`

**Location:** Line 29 (in `getOrDiscoverPlatform`)

**Add this:**
```go
func (r *Reconciler) getOrDiscoverPlatform(ctx context.Context) (*platformInfo, error) {
    r.platformMu.Lock()
    defer r.platformMu.Unlock()
    if r.cachedPlatform != nil {
        // ADD THIS LINE:
        r.log.V(2).Info("using cached platform info", "platformType", r.cachedPlatform.platformType)
        return r.cachedPlatform, nil
    }
    // ... rest
```

**Why V(2)?**
- V(0) = errors/warnings
- V(1) = important events
- V(2) = detailed debug info
- Higher = more verbose

### Exercise 2: Add a Validation

**Task:** Reject if there are more than 2 API VIPs (hypothetical limitation).

**File:** `pkg/controller/http01proxy/infrastructure.go`

**Location:** Line 83 (in `validatePlatform`, after checking empty VIPs)

**Add this:**
```go
if len(info.apiVIPs) > 2 {
    return "too many API VIPs; maximum 2 supported"
}
```

### Exercise 3: Add a Status Field

**Task:** Track when the proxy was last reconciled.

**File:** `api/operator/v1alpha1/http01proxy_types.go`

**Location:** Line 108 (in `HTTP01ProxyStatus`)

**Add this:**
```go
type HTTP01ProxyStatus struct {
    ConditionalStatus `json:",inline,omitempty"`
    ProxyImage string `json:"proxyImage,omitempty"`
    
    // ADD THIS:
    // LastReconcileTime is when the controller last reconciled this resource
    // +optional
    LastReconcileTime *metav1.Time `json:"lastReconcileTime,omitempty"`
}
```

**Then in controller.go, line ~170 (after successful reconciliation):**
```go
now := metav1.Now()
proxy.Status.LastReconcileTime = &now
```

**Note:** This is just an exercise. Don't commit these changes!

---

## Lab 9: Test Your Understanding (15 minutes)

### Quick Quiz

**1. What is the purpose of the finalizer?**
<details>
<summary>Answer</summary>
To prevent the HTTP01Proxy from being deleted until cleanup is complete. Ensures DaemonSet and other resources are removed before the CR is gone.
</details>

**2. Why do we watch child resources (DaemonSet, etc.)?**
<details>
<summary>Answer</summary>
For drift detection. If someone manually deletes the DaemonSet, the controller notices and recreates it. Keeps actual state in sync with desired state.
</details>

**3. Why cache platform info?**
<details>
<summary>Answer</summary>
Performance. Platform info doesn't change, but reconciliation happens often. Caching avoids repeated API calls to fetch Infrastructure/cluster.
</details>

**4. Why use DaemonSet instead of Deployment?**
<details>
<summary>Answer</summary>
The API VIP can fail over to any master node. We need the proxy running on ALL masters so it's there when the VIP moves.
</details>

**5. Why does the proxy need hostNetwork?**
<details>
<summary>Answer</summary>
To intercept traffic destined for the VIP (which is the node's IP). Only possible in the host's network namespace.
</details>

**6. What happens if validation fails?**
<details>
<summary>Answer</summary>
Controller sets Degraded=True with a descriptive message. Does NOT deploy any resources. User sees clear error in status.
</details>

**7. What does the proxy actually do?**
<details>
<summary>Answer</summary>
Checks if request path starts with `/.well-known/acme-challenge/`. If yes, forward to Ingress VIP. If no, return 403.
</details>

**8. Why two NetworkPolicies?**
<details>
<summary>Answer</summary>
Defense in depth. Deny-all ingress (no incoming) + allow-egress (only necessary ports). Limits blast radius if proxy is compromised.
</details>

---

## Lab 10: Bonus Challenge (20 minutes)

### Challenge 1: Add IPv6 Support (Conceptual)

**Task:** Outline what code changes would be needed.

Think about:
1. API types - any changes needed?
2. Platform detection - how to extract IPv6 VIPs?
3. Validation - any new checks?
4. DaemonSet - would it need changes?

<details>
<summary>Hints</summary>

1. API types: No changes needed (VIPs are already string slices)
2. Platform detection: Parse both `apiServerInternalIPs` and `apiServerInternalIPsIPv6` fields
3. Validation: Check both IPv4 and IPv6 VIPs, ensure at least one of each type is different
4. DaemonSet: Might need to support dual-stack networking config
</details>

### Challenge 2: Add Metrics (Conceptual)

**Task:** What metrics would be useful?

Example metrics to track:
1. ________________
2. ________________
3. ________________

<details>
<summary>Suggestions</summary>

1. `http01proxy_reconcile_total{result="success|error"}` - Count of reconciliations
2. `http01proxy_platform_validation_result{platform="...", valid="true|false"}` - Platform validation outcomes
3. `http01proxy_daemonset_ready_replicas` - How many proxy Pods are ready
4. `http01proxy_requests_forwarded_total` - Count of ACME requests forwarded (would require proxy instrumentation)
</details>

### Challenge 3: Design a Test Case

**Task:** Write a test outline for platform validation.

```go
func TestValidatePlatform(t *testing.T) {
    tests := []struct {
        name     string
        platform platformInfo
        wantErr  bool
        errMsg   string
    }{
        // Test case 1: Happy path
        {
            name: "valid bare-metal with distinct VIPs",
            platform: platformInfo{
                platformType: "BareMetal",
                apiVIPs:      []string{"192.168.1.100"},
                ingressVIPs:  []string{"192.168.1.101"},
            },
            wantErr: false,
        },
        // ADD MORE TEST CASES:
        // - Invalid platform type
        // - Missing API VIP
        // - Missing Ingress VIP
        // - Same VIPs
        // - Multiple VIPs (dual-stack)
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := validatePlatform(&tt.platform)
            if tt.wantErr {
                if result == "" {
                    t.Errorf("expected error, got nil")
                }
                if result != tt.errMsg {
                    t.Errorf("expected error %q, got %q", tt.errMsg, result)
                }
            } else {
                if result != "" {
                    t.Errorf("expected no error, got %q", result)
                }
            }
        })
    }
}
```

---

## Wrap-Up

### What You've Learned

Through these labs, you've:
- ✅ Explored the API type definitions and validation rules
- ✅ Traced the controller reconciliation flow
- ✅ Understood platform detection and caching
- ✅ Examined deployment manifests and security settings
- ✅ Learned about RBAC permissions
- ✅ Simulated full reconciliation scenarios
- ✅ Practiced debugging common issues
- ✅ Made conceptual code changes
- ✅ Tested your understanding

### Next Steps

1. **Review your notes** from each lab
2. **Try the code modifications** (in a test branch!)
3. **Go through the debugging scenarios** with the actual code open
4. **Practice explaining** what you learned out loud

### Reflection Questions

1. What was the most surprising thing you learned?
2. What concept was hardest to understand?
3. What would you explain differently to your team?
4. What questions do you still have?

---

**Congratulations! You've completed the hands-on lab!** 🎉

Now you're ready to present with confidence, backed by hands-on code exploration.

---

*Pro tip: The best way to solidify this knowledge is to teach it. Practice your presentation soon while it's fresh!*

# HTTP01 Proxy PR - Complete Learning Guide
## For Presentation to Your Team

---

## Table of Contents
1. [Executive Summary](#executive-summary)
2. [Background Concepts You Need to Know](#background-concepts)
3. [The Problem Being Solved](#the-problem)
4. [The Solution Architecture](#the-solution)
5. [Code Implementation Deep Dive](#code-implementation)
6. [Testing & Verification](#testing)
7. [Presentation Structure for Your Team](#presentation-guide)

---

## Executive Summary

**What is this PR?**
This PR adds a new HTTP01 Challenge Proxy controller to the cert-manager-operator that enables automatic SSL/TLS certificate issuance for OpenShift API endpoints on bare-metal clusters.

**Why does it matter?**
On bare-metal OpenShift clusters, the API server and Ingress controller use different IP addresses (VIPs). This prevents cert-manager from completing ACME HTTP-01 challenges for API certificates because the challenge requests can't reach the cert-manager solver.

**What does it do?**
It deploys a reverse proxy on control plane nodes that:
- Intercepts HTTP traffic on port 80 at the API VIP
- Forwards ONLY ACME challenge requests (`/.well-known/acme-challenge/*`) to the Ingress VIP
- Rejects all other traffic
- Enables automated certificate issuance from Let's Encrypt (or other ACME CAs)

**Key Metrics:**
- **Lines Changed:** ~3,200 lines (52 files modified)
- **New CRD:** `HTTP01Proxy` (v1alpha1)
- **Feature Gate:** Alpha, disabled by default
- **Tracking:** Jira CM-716, Enhancement openshift/enhancements#1929

---

## Background Concepts

### 1. What is cert-manager?

**cert-manager** is a Kubernetes add-on that automates the management of SSL/TLS certificates.

**Think of it like this:**
- Instead of manually creating certificates, renewing them before expiry, and updating them in your cluster
- cert-manager does all of this automatically
- It integrates with Certificate Authorities (CAs) like Let's Encrypt, HashiCorp Vault, or private CAs

**Key Components:**
```
User creates Certificate -----→ cert-manager watches
                                       ↓
                              Creates CertificateRequest
                                       ↓
                              Issuer provisions certificate
                                       ↓
                              Stores in Kubernetes Secret
```

### 2. What is ACME Protocol?

**ACME = Automatic Certificate Management Environment**

It's the protocol that Let's Encrypt uses to verify you own a domain before issuing a certificate.

**The Flow:**
```
1. You request a certificate for api.cluster.example.com
2. ACME server says: "Prove you control this domain"
3. You complete a "challenge" to prove ownership
4. ACME server issues the certificate
```

### 3. What is HTTP-01 Challenge?

**One of the ways to prove domain ownership.**

**How it works:**
```
1. ACME server gives you a TOKEN
2. You must serve that TOKEN at:
   http://<YOUR_DOMAIN>/.well-known/acme-challenge/<TOKEN>

3. ACME server makes an HTTP request to that URL
4. If it gets the correct TOKEN back, you've proven ownership
5. Certificate is issued
```

**Example:**
```bash
# You want a cert for: api.cluster.example.com

# ACME server says: serve this token
http://api.cluster.example.com/.well-known/acme-challenge/ABC123

# ACME server makes a request:
curl http://api.cluster.example.com/.well-known/acme-challenge/ABC123

# If it gets back "ABC123.SIGNATURE", you proved you control the domain
```

### 4. What is OpenShift?

**OpenShift** = Enterprise Kubernetes platform by Red Hat

**Key difference from vanilla Kubernetes:**
- More opinionated (secure by default)
- Includes operators for common services
- Has concepts like Routes (instead of just Ingress)
- Integrated registry, monitoring, logging

### 5. What is a Bare-Metal Cluster?

**Bare-Metal** = Physical servers (not cloud VMs)

**Key characteristic:**
- You control the networking infrastructure
- You define Virtual IPs (VIPs) for high availability
- In OpenShift bare-metal, there are typically TWO VIPs:
  - **API VIP:** for Kubernetes API server (e.g., 192.168.1.100)
  - **Ingress VIP:** for application traffic (e.g., 192.168.1.101)

**Why two VIPs?**
- Separation of concerns
- API traffic vs user application traffic
- Different availability/scaling requirements

### 6. What is a VIP (Virtual IP)?

**VIP = Virtual IP Address**

**Think of it as:**
- A single IP address that "floats" between multiple servers
- Provides high availability - if one server fails, another takes over the VIP
- Clients always connect to the same VIP, regardless of which server is handling it

**Example:**
```
Control Plane Nodes:
- master-1: 192.168.1.10
- master-2: 192.168.1.11  
- master-3: 192.168.1.12

API VIP: 192.168.1.100 (floats between masters)
Ingress VIP: 192.168.1.101 (floats between masters)

Users always connect to 192.168.1.100 for API
```

### 7. What is an Operator?

**Kubernetes Operator Pattern:**
- A custom controller that manages complex applications
- Encodes operational knowledge into code
- Watches Custom Resources and reconciles desired state

**cert-manager-operator** manages the cert-manager installation on OpenShift

---

## The Problem

### The Challenge with Bare-Metal OpenShift

**On bare-metal OpenShift clusters:**

1. **API VIP** (e.g., 192.168.1.100) → Kubernetes API server
2. **Ingress VIP** (e.g., 192.168.1.101) → OpenShift Router (Ingress)

**When you want a certificate for `api.cluster.example.com`:**

```
DNS: api.cluster.example.com → 192.168.1.100 (API VIP)
```

**The Problem:**

```
ACME Server: "Prove you control api.cluster.example.com"
             "I'll make an HTTP request to:"
             http://api.cluster.example.com/.well-known/acme-challenge/TOKEN

ACME request → DNS resolves to 192.168.1.100 (API VIP)
            → API VIP only serves Kubernetes API (port 6443)
            → API VIP does NOT serve HTTP on port 80
            → ACME request FAILS ❌
```

**Why can't the API VIP serve HTTP-01 challenges?**
- The API server only listens on port 6443 (HTTPS for Kubernetes API)
- It doesn't have an HTTP server on port 80
- It's not designed to serve arbitrary HTTP content

**Where does cert-manager's HTTP-01 solver run?**
- cert-manager creates a Pod with an HTTP server
- Creates a Service to expose that Pod
- Creates an Ingress/Route to make it accessible
- **But Ingress traffic goes to the INGRESS VIP, not the API VIP!**

```
cert-manager HTTP-01 Solver Pod
     ↓
Ingress/Route
     ↓
Accessible at Ingress VIP (192.168.1.101)

BUT:
api.cluster.example.com → API VIP (192.168.1.100)

MISMATCH! ❌
```

### Visual Diagram of the Problem

```
┌─────────────────────────────────────────────────────────────┐
│ ACME Server (Let's Encrypt)                                 │
│                                                              │
│ "Prove you own api.cluster.example.com"                     │
│ GET http://api.cluster.example.com/.well-known/acme-...     │
└─────────────────────────────────────────────────────────────┘
                          │
                          ├── DNS lookup: api.cluster.example.com
                          │   Answer: 192.168.1.100 (API VIP)
                          │
                          ↓
┌─────────────────────────────────────────────────────────────┐
│ API VIP: 192.168.1.100                                      │
│                                                              │
│ ❌ Only port 6443 (Kubernetes API) is open                 │
│ ❌ No HTTP server on port 80                                │
│ ❌ Challenge request fails                                  │
└─────────────────────────────────────────────────────────────┘

Meanwhile, cert-manager's solver is reachable at:
┌─────────────────────────────────────────────────────────────┐
│ Ingress VIP: 192.168.1.101                                  │
│                                                              │
│ ✅ HTTP server on port 80                                   │
│ ✅ cert-manager solver Pod is here                          │
│ ✅ But ACME requests don't come here!                       │
└─────────────────────────────────────────────────────────────┘
```

### Why Can't We Just Change DNS?

**You might think:** "Just make `api.cluster.example.com` point to the Ingress VIP!"

**Why this doesn't work:**
1. **API server needs the API VIP** - the Kubernetes API itself must be accessible there
2. **Ingress VIP is for application traffic** - not for system services
3. **Architectural separation** - mixing API and Ingress traffic violates the design
4. **Other services rely on this setup** - changing it breaks the cluster

---

## The Solution

### High-Level Approach

**The HTTP01 Proxy acts as a traffic router:**

```
HTTP request to API VIP:80
    ↓
Is it /.well-known/acme-challenge/* ?
    ├── YES → Forward to Ingress VIP (cert-manager solver)
    └── NO  → Reject (403 Forbidden)
```

### Architecture Diagram

```
┌────────────────────────────────────────────────────────────────┐
│ ACME Server (Let's Encrypt)                                    │
│ GET http://api.cluster.example.com/.well-known/acme-...        │
└────────────────────────────────────────────────────────────────┘
                          │
                          ↓
┌────────────────────────────────────────────────────────────────┐
│ API VIP: 192.168.1.100:80                                      │
│                                                                 │
│         ┌────────────────────────────────────┐                 │
│         │  HTTP01 Proxy (nftables + Go app)  │                 │
│         │                                     │                 │
│         │  1. nftables redirects :80 → :8888 │                 │
│         │  2. Go app checks path              │                 │
│         │  3. If ACME → forward to Ingress    │                 │
│         │  4. Else → reject                   │                 │
│         └────────────────────────────────────┘                 │
│                            │                                    │
│         /.well-known/acme-challenge/*                           │
│                            ↓                                    │
└────────────────────────────────────────────────────────────────┘
                             │
                             ↓
┌────────────────────────────────────────────────────────────────┐
│ Ingress VIP: 192.168.1.101:80                                  │
│                                                                 │
│         ┌────────────────────────────────────┐                 │
│         │  OpenShift Router / Ingress        │                 │
│         │           ↓                        │                 │
│         │  cert-manager HTTP-01 Solver Pod   │                 │
│         │  Returns: TOKEN.SIGNATURE          │                 │
│         └────────────────────────────────────┘                 │
└────────────────────────────────────────────────────────────────┘
                             │
                             ↓
                   ✅ Challenge succeeds!
                   ✅ Certificate issued!
```

### How It Works - Step by Step

**1. User Creates HTTP01Proxy Resource**
```yaml
apiVersion: operator.openshift.io/v1alpha1
kind: HTTP01Proxy
metadata:
  name: default
  namespace: cert-manager-operator
spec:
  mode: DefaultDeployment
```

**2. Controller Validates Platform**
- Checks if platform is BareMetal
- Retrieves API VIP and Ingress VIP from Infrastructure/cluster
- Validates they are different
- If wrong platform → sets Degraded=True, does nothing

**3. Controller Deploys Resources**
- **ServiceAccount:** `cert-manager-http01-proxy`
- **ClusterRole:** Permissions needed (empty in this case)
- **ClusterRoleBinding:** Binds ServiceAccount to ClusterRole
- **ClusterRoleBinding:** Binds to `privileged` SCC (Security Context Constraint)
- **DaemonSet:** Runs proxy on all control plane nodes
- **NetworkPolicies:** Restricts traffic (deny-all ingress, allow egress on 80/443/6443)

**4. DaemonSet Runs Proxy Container**
- Runs on `hostNetwork: true` (shares node's network namespace)
- Has `NET_ADMIN` capability (can modify network rules)
- Uses node selector: `node-role.kubernetes.io/master`
- Image: `quay.io/bapalm/cert-mgr-http01-proxy:latest`

**5. Proxy Container Starts**

**a) Sets up nftables rules:**
```bash
# Redirect all TCP traffic on port 80 to local port 8888
nft add table ip http01proxy
nft add chain ip http01proxy prerouting { type nat hook prerouting priority dstnat\; }
nft add rule ip http01proxy prerouting tcp dport 80 dnat to :8888
```

**b) Runs Go HTTP server on port 8888:**
```go
// Pseudo-code of what the proxy does
func handleRequest(req *http.Request) {
    if strings.HasPrefix(req.URL.Path, "/.well-known/acme-challenge/") {
        // Forward to Ingress VIP
        proxyToIngress(req)
    } else {
        // Reject
        http.Error(w, "Forbidden", 403)
    }
}
```

**6. ACME Challenge Flow**

```
Let's Encrypt → api.cluster.example.com:80/.well-known/acme-challenge/ABC
                      ↓
                 API VIP (192.168.1.100)
                      ↓
                 nftables: port 80 → 8888
                      ↓
                 Go proxy on :8888
                      ↓
                 Check path: /.well-known/acme-challenge/ABC
                      ↓
                 Match! Forward to http://192.168.1.101:80/.well-known/...
                      ↓
                 Ingress VIP
                      ↓
                 cert-manager solver Pod
                      ↓
                 Return TOKEN
                      ↓
                 Let's Encrypt ✅
                      ↓
                 Certificate issued!
```

### Key Design Decisions

**1. Why DaemonSet?**
- Need proxy on EVERY control plane node
- API VIP can fail over to any master
- DaemonSet ensures proxy runs on all masters

**2. Why nftables + Go app?**
- **nftables:** Low-level packet redirection (port 80 → 8888)
- **Go app:** Higher-level path checking and proxying
- This separation keeps the logic clean

**3. Why hostNetwork?**
- Proxy needs to intercept traffic destined for the node's IP (the VIP)
- Can only do this in the host's network namespace

**4. Why NET_ADMIN capability?**
- Required to modify nftables/iptables rules
- Needed to set up the port redirect

**5. Why privileged SCC?**
- OpenShift's Security Context Constraint
- hostNetwork + NET_ADMIN requires elevated privileges
- Binding to `privileged` SCC allows this

**6. Why NetworkPolicies?**
- Defense in depth
- Deny all ingress (proxy doesn't need incoming connections)
- Allow egress only to necessary ports (80, 443 for Ingress, 6443 for API)

---

## Code Implementation

### File Structure Overview

```
api/operator/v1alpha1/
├── http01proxy_types.go          # API type definitions
├── features.go                    # Feature gate definition
└── zz_generated.deepcopy.go       # Generated deepcopy methods

pkg/controller/http01proxy/
├── controller.go                  # Main controller logic
├── constants.go                   # Constants (labels, annotations, etc.)
├── infrastructure.go              # Platform detection & VIP retrieval
├── daemonsets.go                  # DaemonSet reconciliation
├── rbacs.go                       # RBAC reconciliation
├── serviceaccounts.go             # ServiceAccount reconciliation
├── networkpolicies.go             # NetworkPolicy reconciliation
├── install_http01proxy.go         # Main install logic
└── utils.go                       # Status updates, helpers

bindata/http01-proxy/
├── cert-manager-http01-proxy-daemonset.yaml
├── cert-manager-http01-proxy-clusterrole.yaml
├── cert-manager-http01-proxy-clusterrolebinding.yaml
├── cert-manager-http01-proxy-scc-rolebinding.yaml
└── cert-manager-http01-proxy-serviceaccount.yaml

bindata/networkpolicies/
├── http01-proxy-allow-egress-networkpolicy.yaml
└── http01-proxy-deny-all-networkpolicy.yaml

config/crd/bases/
└── operator.openshift.io_http01proxies.yaml    # CRD definition

pkg/operator/
├── setup_manager.go               # Wire controller into manager
├── starter.go                     # Add to cached resources
└── clientset/typed/operator/v1alpha1/  # Generated clients
    └── http01proxy.go
```

### Deep Dive: API Types

**File: `api/operator/v1alpha1/http01proxy_types.go`**

```go
// HTTP01ProxySpec defines the desired state of HTTP01Proxy
type HTTP01ProxySpec struct {
    // Mode determines how the HTTP01 proxy deployment is configured.
    // Valid values are:
    // - "DefaultDeployment" (default): Automatically deployed DaemonSet
    // - "CustomDeployment": User manages deployment, must specify customDeployment
    //
    // +kubebuilder:validation:Enum=DefaultDeployment;CustomDeployment
    // +kubebuilder:default=DefaultDeployment
    Mode HTTP01ProxyMode `json:"mode,omitempty"`

    // CustomDeployment specifies custom deployment configuration.
    // Only valid when mode is "CustomDeployment".
    //
    // +optional
    CustomDeployment *HTTP01ProxyCustomDeploymentSpec `json:"customDeployment,omitempty"`
}
```

**Key Points:**
- `mode`: Two options
  - `DefaultDeployment`: Operator manages everything (this is what the PR implements)
  - `CustomDeployment`: User brings their own proxy (future extension point)
- Validation enforced by kubebuilder markers
- Default is `DefaultDeployment`

```go
// HTTP01ProxyMode defines the deployment mode for HTTP01 proxy
type HTTP01ProxyMode string

const (
    // HTTP01ProxyModeDefaultDeployment - operator deploys and manages DaemonSet
    HTTP01ProxyModeDefaultDeployment HTTP01ProxyMode = "DefaultDeployment"
    
    // HTTP01ProxyModeCustomDeployment - user manages deployment
    HTTP01ProxyModeCustomDeployment HTTP01ProxyMode = "CustomDeployment"
)
```

```go
// HTTP01ProxyStatus defines the observed state of HTTP01Proxy
type HTTP01ProxyStatus struct {
    // ProxyImage is the image used for the HTTP01 proxy DaemonSet
    //
    // +optional
    ProxyImage string `json:"proxyImage,omitempty"`

    // Conditions represent the latest available observations of the HTTP01Proxy state
    //
    // +listType=map
    // +listMapKey=type
    // +optional
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

**Status Tracking:**
- `ProxyImage`: Which image is deployed
- `Conditions`: Standard Kubernetes conditions pattern
  - Available, Degraded, Progressing

```go
// HTTP01Proxy is the Schema for the http01proxies API
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default'",message="HTTP01Proxy resource name must be 'default'"
type HTTP01Proxy struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   HTTP01ProxySpec   `json:"spec,omitempty"`
    Status HTTP01ProxyStatus `json:"status,omitempty"`
}
```

**Important Constraint:**
- `+kubebuilder:validation:XValidation:rule="self.metadata.name == 'default'"`
- **ONLY ONE INSTANCE ALLOWED** - must be named "default"
- This is a **singleton** resource
- Prevents confusion from multiple instances

### Deep Dive: Controller

**File: `pkg/controller/http01proxy/controller.go`**

```go
type HTTP01ProxyReconciler struct {
    client.Client
    Scheme   *runtime.Scheme
    Recorder record.EventRecorder
}
```

**Reconciler Structure:**
- Embeds `client.Client` (for K8s API operations)
- `Scheme` for type conversion
- `Recorder` for emitting events

```go
func (r *HTTP01ProxyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := ctrl.LoggerFrom(ctx)
    
    // 1. Fetch the HTTP01Proxy instance
    http01Proxy := &operatorv1alpha1.HTTP01Proxy{}
    if err := r.Get(ctx, req.NamespacedName, http01Proxy); err != nil {
        if apierrors.IsNotFound(err) {
            // Resource deleted, nothing to do
            return ctrl.Result{}, nil
        }
        return ctrl.Result{}, err
    }

    // 2. Handle deletion (finalizer pattern)
    if !http01Proxy.DeletionTimestamp.IsZero() {
        return r.reconcileDelete(ctx, http01Proxy)
    }

    // 3. Ensure finalizer is present
    if !controllerutil.ContainsFinalizer(http01Proxy, http01ProxyFinalizer) {
        controllerutil.AddFinalizer(http01Proxy, http01ProxyFinalizer)
        if err := r.Update(ctx, http01Proxy); err != nil {
            return ctrl.Result{}, err
        }
        // Requeue to continue reconciliation
        return ctrl.Result{Requeue: true}, nil
    }

    // 4. Validate platform and get VIPs
    infraConfig, apiVIP, ingressVIP, degradedCondition := getPlatformAndVIPs(ctx, r.Client)
    if degradedCondition != nil {
        // Platform not supported or VIPs invalid
        updateStatusCondition(http01Proxy, *degradedCondition)
        if err := r.Status().Update(ctx, http01Proxy); err != nil {
            return ctrl.Result{}, err
        }
        // Don't deploy anything, but don't error
        return ctrl.Result{}, nil
    }

    // 5. Install/update HTTP01 proxy resources
    if err := r.installHTTP01Proxy(ctx, http01Proxy, infraConfig, apiVIP, ingressVIP); err != nil {
        updateStatusDegraded(http01Proxy, "DeploymentFailed", err.Error())
        r.Status().Update(ctx, http01Proxy)
        return ctrl.Result{}, err
    }

    // 6. Update status to Available
    updateStatusAvailable(http01Proxy)
    if err := r.Status().Update(ctx, http01Proxy); err != nil {
        return ctrl.Result{}, err
    }

    return ctrl.Result{}, nil
}
```

**Reconciliation Flow:**

1. **Fetch resource** - get the HTTP01Proxy object
2. **Check deletion** - if being deleted, clean up
3. **Add finalizer** - ensures cleanup happens before deletion
4. **Validate platform** - check if bare-metal with distinct VIPs
5. **Install resources** - deploy DaemonSet, RBAC, NetworkPolicies
6. **Update status** - set conditions

**The Finalizer Pattern:**
```go
const http01ProxyFinalizer = "operator.openshift.io/http01proxy-finalizer"

func (r *HTTP01ProxyReconciler) reconcileDelete(ctx context.Context, http01Proxy *operatorv1alpha1.HTTP01Proxy) (ctrl.Result, error) {
    // Delete all resources we created
    deleteHTTP01ProxyResources(ctx, r.Client, http01Proxy)
    
    // Remove finalizer
    controllerutil.RemoveFinalizer(http01Proxy, http01ProxyFinalizer)
    if err := r.Update(ctx, http01Proxy); err != nil {
        return ctrl.Result{}, err
    }
    
    return ctrl.Result{}, nil
}
```

**Why finalizers?**
- When user deletes HTTP01Proxy, Kubernetes marks it for deletion
- But doesn't actually delete it yet if finalizer is present
- Controller gets a chance to clean up DaemonSet, RBAC, etc.
- Once cleanup done, remove finalizer
- Then Kubernetes actually deletes the object

### Deep Dive: Platform Detection

**File: `pkg/controller/http01proxy/infrastructure.go`**

```go
func getPlatformAndVIPs(ctx context.Context, client client.Client) (
    *configv1.Infrastructure,
    string,
    string,
    *metav1.Condition,
) {
    // 1. Get Infrastructure/cluster object
    infraConfig := &configv1.Infrastructure{}
    if err := client.Get(ctx, types.NamespacedName{Name: "cluster"}, infraConfig); err != nil {
        return nil, "", "", &metav1.Condition{
            Type:    "Degraded",
            Status:  metav1.ConditionTrue,
            Reason:  "InfrastructureNotFound",
            Message: fmt.Sprintf("Failed to get Infrastructure/cluster: %v", err),
        }
    }

    // 2. Check platform type
    platformType := infraConfig.Status.PlatformStatus.Type
    if platformType != configv1.BareMetalPlatformType {
        return nil, "", "", &metav1.Condition{
            Type:    "Degraded",
            Status:  metav1.ConditionTrue,
            Reason:  "UnsupportedPlatform",
            Message: fmt.Sprintf("Platform type %s is not supported. HTTP01Proxy requires BareMetal platform.", platformType),
        }
    }

    // 3. Get API VIP
    apiVIP := infraConfig.Status.PlatformStatus.BareMetal.APIServerInternalIP
    if apiVIP == "" {
        return nil, "", "", &metav1.Condition{
            Type:    "Degraded",
            Status:  metav1.ConditionTrue,
            Reason:  "MissingAPIVIP",
            Message: "API VIP not found in Infrastructure/cluster",
        }
    }

    // 4. Get Ingress VIP
    ingressVIP := infraConfig.Status.PlatformStatus.BareMetal.IngressIP
    if ingressVIP == "" {
        return nil, "", "", &metav1.Condition{
            Type:    "Degraded",
            Status:  metav1.ConditionTrue,
            Reason:  "MissingIngressVIP",
            Message: "Ingress VIP not found in Infrastructure/cluster",
        }
    }

    // 5. Ensure VIPs are different
    if apiVIP == ingressVIP {
        return nil, "", "", &metav1.Condition{
            Type:    "Degraded",
            Status:  metav1.ConditionTrue,
            Reason:  "IdenticalVIPs",
            Message: "API VIP and Ingress VIP are the same. HTTP01Proxy requires distinct VIPs.",
        }
    }

    // All validations passed
    return infraConfig, apiVIP, ingressVIP, nil
}
```

**What this checks:**
1. Can we read Infrastructure/cluster?
2. Is platform type BareMetal?
3. Do we have an API VIP?
4. Do we have an Ingress VIP?
5. Are they different?

**If any check fails:**
- Return a Degraded condition
- Controller sets this condition on HTTP01Proxy status
- Does NOT deploy any resources
- User sees clear error message

### Deep Dive: Resource Deployment

**File: `pkg/controller/http01proxy/install_http01proxy.go`**

```go
func (r *HTTP01ProxyReconciler) installHTTP01Proxy(
    ctx context.Context,
    http01Proxy *operatorv1alpha1.HTTP01Proxy,
    infraConfig *configv1.Infrastructure,
    apiVIP string,
    ingressVIP string,
) error {
    // 1. Reconcile ServiceAccount
    if err := r.reconcileServiceAccount(ctx, http01Proxy); err != nil {
        return fmt.Errorf("failed to reconcile ServiceAccount: %w", err)
    }

    // 2. Reconcile RBAC (ClusterRole, ClusterRoleBindings)
    if err := r.reconcileRBAC(ctx, http01Proxy); err != nil {
        return fmt.Errorf("failed to reconcile RBAC: %w", err)
    }

    // 3. Reconcile NetworkPolicies
    if err := r.reconcileNetworkPolicies(ctx, http01Proxy); err != nil {
        return fmt.Errorf("failed to reconcile NetworkPolicies: %w", err)
    }

    // 4. Reconcile DaemonSet
    if err := r.reconcileDaemonSet(ctx, http01Proxy, apiVIP, ingressVIP); err != nil {
        return fmt.Errorf("failed to reconcile DaemonSet: %w", err)
    }

    return nil
}
```

**Reconcile pattern:**
- Each resource type has its own reconcile function
- Reconcile = "make reality match desired state"
- If resource doesn't exist → create it
- If resource exists but differs → update it
- If resource matches → do nothing

**Example: DaemonSet Reconciliation**

**File: `pkg/controller/http01proxy/daemonsets.go`**

```go
func (r *HTTP01ProxyReconciler) reconcileDaemonSet(
    ctx context.Context,
    http01Proxy *operatorv1alpha1.HTTP01Proxy,
    apiVIP string,
    ingressVIP string,
) error {
    // 1. Load DaemonSet template from embedded assets
    dsBytes, err := assets.ReadFile("http01-proxy/cert-manager-http01-proxy-daemonset.yaml")
    if err != nil {
        return err
    }

    // 2. Parse YAML into DaemonSet object
    desired := &appsv1.DaemonSet{}
    if err := yaml.Unmarshal(dsBytes, desired); err != nil {
        return err
    }

    // 3. Set namespace
    desired.Namespace = http01Proxy.Namespace

    // 4. Inject VIPs as environment variables
    for i := range desired.Spec.Template.Spec.Containers {
        container := &desired.Spec.Template.Spec.Containers[i]
        container.Env = append(container.Env,
            corev1.EnvVar{Name: "API_VIP", Value: apiVIP},
            corev1.EnvVar{Name: "INGRESS_VIP", Value: ingressVIP},
        )
    }

    // 5. Set owner reference (for garbage collection)
    if err := controllerutil.SetControllerReference(http01Proxy, desired, r.Scheme); err != nil {
        return err
    }

    // 6. Get existing DaemonSet (if any)
    existing := &appsv1.DaemonSet{}
    err = r.Get(ctx, types.NamespacedName{
        Name:      desired.Name,
        Namespace: desired.Namespace,
    }, existing)

    if err != nil {
        if apierrors.IsNotFound(err) {
            // DaemonSet doesn't exist, create it
            return r.Create(ctx, desired)
        }
        return err
    }

    // 7. DaemonSet exists, update it if needed
    existing.Spec = desired.Spec
    return r.Update(ctx, existing)
}
```

**Key Concepts:**

**Embedded Assets:**
```go
//go:embed http01-proxy/*.yaml networkpolicies/*.yaml
var assets embed.FS
```
- YAML manifests embedded in Go binary
- No need to ship separate files
- Parsed at runtime

**Owner References:**
- Sets HTTP01Proxy as "owner" of DaemonSet
- When HTTP01Proxy is deleted, DaemonSet is garbage-collected automatically
- Also shows in `kubectl` relationships

**Server-Side Apply vs Update:**
- This PR uses `Update()`
- Could also use Server-Side Apply (SSA) for better conflict resolution
- SSA is more advanced, Update() is simpler

### Deep Dive: Status Management

**File: `pkg/controller/http01proxy/utils.go`**

```go
func updateStatusAvailable(http01Proxy *operatorv1alpha1.HTTP01Proxy) {
    setCondition(http01Proxy, metav1.Condition{
        Type:    "Available",
        Status:  metav1.ConditionTrue,
        Reason:  "HTTP01ProxyDeployed",
        Message: "HTTP01 proxy DaemonSet is deployed and running",
    })
    setCondition(http01Proxy, metav1.Condition{
        Type:    "Degraded",
        Status:  metav1.ConditionFalse,
        Reason:  "AsExpected",
        Message: "",
    })
    setCondition(http01Proxy, metav1.Condition{
        Type:    "Progressing",
        Status:  metav1.ConditionFalse,
        Reason:  "AsExpected",
        Message: "",
    })
}

func updateStatusDegraded(http01Proxy *operatorv1alpha1.HTTP01Proxy, reason, message string) {
    setCondition(http01Proxy, metav1.Condition{
        Type:    "Degraded",
        Status:  metav1.ConditionTrue,
        Reason:  reason,
        Message: message,
    })
    setCondition(http01Proxy, metav1.Condition{
        Type:    "Available",
        Status:  metav1.ConditionFalse,
        Reason:  "Degraded",
        Message: "",
    })
}

func setCondition(http01Proxy *operatorv1alpha1.HTTP01Proxy, newCondition metav1.Condition) {
    // Find existing condition of same type
    for i, condition := range http01Proxy.Status.Conditions {
        if condition.Type == newCondition.Type {
            // Update if different
            if condition.Status != newCondition.Status ||
               condition.Reason != newCondition.Reason ||
               condition.Message != newCondition.Message {
                http01Proxy.Status.Conditions[i] = newCondition
                http01Proxy.Status.Conditions[i].LastTransitionTime = metav1.Now()
            }
            return
        }
    }
    
    // Condition doesn't exist, add it
    newCondition.LastTransitionTime = metav1.Now()
    http01Proxy.Status.Conditions = append(http01Proxy.Status.Conditions, newCondition)
}
```

**Condition Types:**
- **Available:** Is the feature working?
- **Degraded:** Is something wrong?
- **Progressing:** Is a change in progress?

**User Experience:**
```bash
$ kubectl get http01proxy default -o yaml

status:
  conditions:
  - type: Available
    status: "True"
    reason: HTTP01ProxyDeployed
    message: HTTP01 proxy DaemonSet is deployed and running
  - type: Degraded
    status: "False"
    reason: AsExpected
  - type: Progressing
    status: "False"
    reason: AsExpected
  proxyImage: quay.io/bapalm/cert-mgr-http01-proxy:v0.2.0
```

### Deep Dive: Feature Gate

**File: `api/operator/v1alpha1/features.go`**

```go
const (
    // FeatureHTTP01Proxy enables HTTP01 proxy for API endpoint certificate challenges
    //
    // Alpha: disabled by default
    FeatureHTTP01Proxy featuregate.Feature = "HTTP01Proxy"
)

var defaultOperatorFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
    FeatureHTTP01Proxy: {
        Default:       false,  // Disabled by default
        PreRelease:    featuregate.Alpha,  // Alpha quality
        LockToDefault: false,  // Can be enabled via flag
    },
}
```

**How to enable:**
```bash
# Start operator with feature gate
./cert-manager-operator \
    --unsupported-addon-features=HTTP01Proxy=true
```

**File: `pkg/operator/setup_manager.go`**

```go
func SetupWithManager(mgr ctrl.Manager, enableHTTP01Proxy bool) error {
    // ... other controllers ...

    // Conditionally register HTTP01Proxy controller
    if enableHTTP01Proxy {
        if err := (&http01proxycontroller.HTTP01ProxyReconciler{
            Client:   mgr.GetClient(),
            Scheme:   mgr.GetScheme(),
            Recorder: mgr.GetEventRecorderFor("http01proxy-controller"),
        }).SetupWithManager(mgr); err != nil {
            return fmt.Errorf("unable to create HTTP01Proxy controller: %w", err)
        }
    }

    return nil
}
```

**Why feature gates?**
- Allows shipping code that's not ready for production
- Users can opt-in to test
- Can be graduated: Alpha → Beta → GA
- Can be deprecated/removed if needed

---

## Testing

### Unit Tests

**File: `pkg/controller/http01proxy/infrastructure_test.go` (not in PR, but would look like this)**

```go
func TestGetPlatformAndVIPs(t *testing.T) {
    tests := []struct {
        name           string
        infraConfig    *configv1.Infrastructure
        expectDegraded bool
        expectReason   string
    }{
        {
            name: "valid bare-metal with distinct VIPs",
            infraConfig: &configv1.Infrastructure{
                Status: configv1.InfrastructureStatus{
                    PlatformStatus: &configv1.PlatformStatus{
                        Type: configv1.BareMetalPlatformType,
                        BareMetal: &configv1.BareMetalPlatformStatus{
                            APIServerInternalIP: "192.168.1.100",
                            IngressIP:          "192.168.1.101",
                        },
                    },
                },
            },
            expectDegraded: false,
        },
        {
            name: "non-bare-metal platform",
            infraConfig: &configv1.Infrastructure{
                Status: configv1.InfrastructureStatus{
                    PlatformStatus: &configv1.PlatformStatus{
                        Type: configv1.AWSPlatformType,
                    },
                },
            },
            expectDegraded: true,
            expectReason:   "UnsupportedPlatform",
        },
        {
            name: "identical VIPs",
            infraConfig: &configv1.Infrastructure{
                Status: configv1.InfrastructureStatus{
                    PlatformStatus: &configv1.PlatformStatus{
                        Type: configv1.BareMetalPlatformType,
                        BareMetal: &configv1.BareMetalPlatformStatus{
                            APIServerInternalIP: "192.168.1.100",
                            IngressIP:          "192.168.1.100",
                        },
                    },
                },
            },
            expectDegraded: true,
            expectReason:   "IdenticalVIPs",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            client := fake.NewClientBuilder().
                WithObjects(tt.infraConfig).
                Build()
            
            _, _, _, condition := getPlatformAndVIPs(context.Background(), client)
            
            if tt.expectDegraded {
                if condition == nil {
                    t.Error("expected degraded condition, got nil")
                }
                if condition.Reason != tt.expectReason {
                    t.Errorf("expected reason %s, got %s", tt.expectReason, condition.Reason)
                }
            } else {
                if condition != nil {
                    t.Errorf("expected no degraded condition, got: %v", condition)
                }
            }
        })
    }
}
```

### Manual Testing (from PR Description)

**Verification Script: `hack/verify-http01proxy.sh`**

This script:
1. Checks if HTTP01Proxy resource exists
2. Verifies DaemonSet is running on all masters
3. Checks nftables rules are in place
4. Tests ACME challenge path forwarding
5. Verifies non-ACME requests are rejected

**Example usage:**
```bash
$ ./hack/verify-http01proxy.sh

✅ HTTP01Proxy resource exists
✅ DaemonSet running on 3/3 master nodes
✅ nftables rules configured on all nodes
✅ ACME challenge path forwarded correctly
✅ Non-ACME requests rejected (403)

HTTP01 Proxy is working correctly!
```

### End-to-End Testing

**Real certificate issuance flow:**

1. **Create Issuer** (Let's Encrypt staging):
```yaml
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: letsencrypt-staging
spec:
  acme:
    server: https://acme-staging-v02.api.letsencrypt.org/directory
    email: admin@example.com
    privateKeySecretRef:
      name: letsencrypt-staging-key
    solvers:
    - http01:
        ingress:
          class: openshift-default
```

2. **Create Certificate**:
```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: api-cert
spec:
  secretName: api-cert-tls
  dnsNames:
  - api.cluster.example.com
  issuerRef:
    name: letsencrypt-staging
```

3. **Watch the magic**:
```bash
# cert-manager creates a CertificateRequest
$ kubectl get certificaterequest

# cert-manager creates a Challenge
$ kubectl get challenge

# cert-manager creates solver Pod + Ingress
$ kubectl get pods -n cert-manager
$ kubectl get ingress -n cert-manager

# Let's Encrypt makes HTTP request
# Request hits API VIP:80
# HTTP01 proxy intercepts and forwards
# Challenge succeeds

# Certificate is issued!
$ kubectl get certificate
NAME       READY   SECRET         AGE
api-cert   True    api-cert-tls   2m
```

### Tested Platforms (from PR)

**✅ CRC (CodeReady Containers - Single Node OpenShift):**
- Platform type: None
- Expected: Controller sets Degraded=True with "platform type None is not supported"
- Actual: ✅ Correct behavior

**✅ Bare-metal MNO (Multi-Node OpenShift):**
- Cluster: cnfdt16 (OCP 5.0)
- 3 master nodes
- Distinct API and Ingress VIPs
- Expected: DaemonSet deployed, proxy running, challenges forwarded
- Actual: ✅ All verification checks pass

**✅ Unit Tests:**
- 654 tests in 28 packages
- All pass

**✅ Static Analysis:**
- `make verify` passes
- golangci-lint clean

---

## Presentation Guide

### Structure for Your Team Presentation

**Duration: 20-30 minutes**

---

### **Slide 1: Title**
**"HTTP01 Proxy for cert-manager on Bare-Metal OpenShift"**
- PR #398
- Your Name
- Date

---

### **Slide 2: The Problem (5 mins)**

**Show this diagram:**
```
ACME Server wants to verify api.cluster.example.com
         ↓
    DNS lookup → 192.168.1.100 (API VIP)
         ↓
    Request to http://192.168.1.100/.well-known/acme-challenge/TOKEN
         ↓
    ❌ API VIP only has port 6443 (Kubernetes API)
    ❌ No HTTP server on port 80
    ❌ Challenge FAILS
```

**Key Points:**
- On bare-metal, API and Ingress use different VIPs
- ACME HTTP-01 needs to reach port 80 on the domain's IP
- cert-manager's solver is behind Ingress VIP
- API VIP doesn't have an HTTP server
- **Result: Can't get automated certificates for API endpoint**

---

### **Slide 3: Why This Matters (2 mins)**

**Without this PR:**
- Manual certificate management for API endpoints
- Risk of certificate expiry
- No automation

**With this PR:**
- Fully automated certificate lifecycle
- cert-manager handles renewals
- Uses Let's Encrypt (free, trusted)
- Follows Kubernetes-native patterns

---

### **Slide 4: The Solution Overview (3 mins)**

**HTTP01 Proxy = Smart Traffic Router**

```
Request to API VIP:80
    ↓
Is it /.well-known/acme-challenge/* ?
    ├── YES → Forward to Ingress VIP (cert-manager)
    └── NO  → Reject (403)
```

**Deployed as:**
- DaemonSet on all control plane nodes
- Uses nftables for packet redirection
- Go application for path-based routing
- Secured with NetworkPolicies

---

### **Slide 5: Architecture Diagram (3 mins)**

**Show the full diagram from earlier:**

```
ACME Server
    ↓
API VIP:80
    ↓
nftables: redirect :80 → :8888
    ↓
Go proxy: check path
    ├── /.well-known/acme-challenge/* → forward to Ingress VIP
    └── other → 403
    ↓
Ingress VIP:80
    ↓
cert-manager solver Pod
    ↓
✅ Challenge succeeds!
```

---

### **Slide 6: New API Resource (2 mins)**

**Introduced Custom Resource Definition: `HTTP01Proxy`**

```yaml
apiVersion: operator.openshift.io/v1alpha1
kind: HTTP01Proxy
metadata:
  name: default  # Must be named "default" (singleton)
  namespace: cert-manager-operator
spec:
  mode: DefaultDeployment  # Operator manages everything
```

**Design Decisions:**
- Singleton resource (only one allowed)
- Simple spec (one field)
- Extensible (mode: CustomDeployment for future)

---

### **Slide 7: Controller Logic (3 mins)**

**Reconciliation Loop:**

1. **Validate platform** - Is it bare-metal with distinct VIPs?
2. **Deploy resources**:
   - ServiceAccount
   - RBAC (ClusterRole, Bindings)
   - NetworkPolicies
   - DaemonSet (proxy)
3. **Update status** - Available or Degraded

**Smart Degradation:**
- If wrong platform → Set Degraded, don't deploy
- User sees clear error message
- No unnecessary resources created

---

### **Slide 8: Security Considerations (3 mins)**

**Why is this secure?**

1. **Minimal Attack Surface**
   - Only ACME challenge paths forwarded
   - All other requests rejected

2. **Network Policies**
   - Deny all ingress
   - Allow only necessary egress (80, 443, 6443)

3. **Privileged Access (necessary)**
   - Needs hostNetwork (to intercept VIP traffic)
   - Needs NET_ADMIN (to set nftables rules)
   - Bound to privileged SCC

4. **Defense in Depth**
   - Path validation in Go code
   - nftables rules at kernel level
   - NetworkPolicies at pod level

---

### **Slide 9: Code Changes Breakdown (2 mins)**

**Statistics:**
- 52 files changed
- ~3,200 lines added
- 0 lines removed (pure addition)

**Key Components:**
1. API types (CRD definition)
2. Controller implementation
3. Generated clients/informers/listers
4. Embedded YAML manifests
5. Feature gate integration
6. Testing scripts

---

### **Slide 10: Testing & Validation (2 mins)**

**Tested on:**
- ✅ CRC (SNO) - correctly degraded (unsupported platform)
- ✅ Bare-metal cluster (3 masters) - full deployment success
- ✅ 654 unit tests passing
- ✅ Static analysis (`make verify`) passing

**Verification:**
- Custom script checks DaemonSet, nftables, forwarding
- End-to-end certificate issuance with Let's Encrypt staging
- All checks pass

---

### **Slide 11: Feature Maturity (2 mins)**

**Alpha Feature (disabled by default)**

**To enable:**
```bash
--unsupported-addon-features=HTTP01Proxy=true
```

**Why Alpha?**
- First version
- Needs real-world testing
- May need adjustments
- Breaking changes possible

**Graduation Path:**
- Alpha → Beta → GA
- Each stage requires testing, feedback, stability

---

### **Slide 12: Future Enhancements (1 min)**

**Potential improvements:**

1. **CustomDeployment mode** - let users bring their own proxy
2. **Metrics & observability** - Prometheus metrics for proxy
3. **IPv6 support** - currently IPv4 only
4. **Alternative proxy implementations** - allow different proxy images
5. **Status enhancements** - report Pod ready count, errors

---

### **Slide 13: How to Use It (2 mins)**

**Prerequisites:**
- OpenShift bare-metal cluster
- Distinct API and Ingress VIPs
- cert-manager installed
- HTTP01Proxy feature enabled

**Steps:**
1. Enable feature gate on operator
2. Create HTTP01Proxy resource
3. Verify DaemonSet deployed
4. Create cert-manager Issuer (ACME)
5. Create Certificate for API endpoint
6. Watch certificate get issued automatically!

---

### **Slide 14: Key Takeaways (1 min)**

1. **Solves real problem** - automated certs for bare-metal API
2. **Clean architecture** - follows Kubernetes patterns
3. **Secure by default** - minimal attack surface
4. **Well-tested** - multiple platforms validated
5. **Alpha quality** - ready for early adopters

---

### **Slide 15: Q&A**

**Common Questions to Prepare For:**

**Q: Why not just change DNS to point to Ingress VIP?**
A: API server itself needs the API VIP. Changing DNS breaks cluster.

**Q: Why DaemonSet instead of Deployment?**
A: API VIP can fail over to any master. Need proxy on ALL masters.

**Q: Is this a security risk?**
A: Minimal - only ACME paths forwarded, NetworkPolicies restrict traffic, runs in privileged context but with clear justification.

**Q: What happens on non-bare-metal?**
A: Controller detects platform, sets Degraded status, doesn't deploy anything.

**Q: Can this break the API server?**
A: No - proxy only touches port 80. API server is on port 6443.

**Q: Why nftables instead of iptables?**
A: nftables is the modern replacement, better performance, clearer syntax.

---

### Presentation Tips

**Do's:**
- ✅ Use diagrams - visual learners need them
- ✅ Live demo if possible (show DaemonSet, nftables rules)
- ✅ Walk through one reconciliation loop step-by-step
- ✅ Explain WHY decisions were made, not just WHAT
- ✅ Connect to broader Kubernetes/OpenShift concepts

**Don'ts:**
- ❌ Don't dive into every line of code
- ❌ Don't assume everyone knows ACME/cert-manager
- ❌ Don't skip the problem explanation
- ❌ Don't ignore security questions
- ❌ Don't oversimplify the complexity

**Time Management:**
- Problem: 5 mins
- Solution: 5 mins
- Architecture: 5 mins
- Code: 5 mins
- Testing/Security: 5 mins
- Q&A: 5-10 mins

---

## Additional Study Resources

### To Deepen Your Understanding

**1. Read the Enhancement Proposal:**
- https://github.com/openshift/enhancements/pull/1929
- This explains the original design decisions

**2. Study cert-manager docs:**
- https://cert-manager.io/docs/
- Focus on ACME issuer and HTTP-01 challenges

**3. Explore the proxy image source:**
- https://github.com/sebrandon1/cert-mgr-http01-proxy
- See the actual Go code that does the proxying

**4. Understand nftables:**
- https://wiki.nftables.org/
- Learn about packet filtering and NAT

**5. Kubernetes Operator Pattern:**
- https://kubernetes.io/docs/concepts/extend-kubernetes/operator/
- Understand controller reconciliation

### Practice Questions for Yourself

**Before your presentation, ensure you can answer:**

1. What is the ACME protocol?
2. Why do bare-metal clusters have separate VIPs?
3. What does the reconciliation loop do?
4. How does the finalizer pattern work?
5. Why is this a feature gate?
6. What happens if someone tries to create two HTTP01Proxy resources?
7. How does nftables redirect traffic?
8. What is a DaemonSet and why use it here?
9. What are NetworkPolicies and why include them?
10. How would you debug this if it's not working?

---

## Debugging Guide (Bonus)

### If HTTP01 Proxy Isn't Working

**1. Check the HTTP01Proxy resource:**
```bash
kubectl get http01proxy default -n cert-manager-operator -o yaml
```
Look at `status.conditions` - is it Degraded?

**2. Check DaemonSet:**
```bash
kubectl get daemonset cert-manager-http01-proxy -n cert-manager-operator
```
Are all desired pods running?

**3. Check proxy pods:**
```bash
kubectl get pods -n cert-manager-operator -l app=cert-manager-http01-proxy
```

**4. Check logs:**
```bash
kubectl logs -n cert-manager-operator <pod-name>
```

**5. Check nftables rules (on a master node):**
```bash
oc debug node/<master-node>
chroot /host
nft list ruleset | grep http01proxy
```

**6. Test manually:**
```bash
# From outside the cluster
curl -v http://<API-VIP>/.well-known/acme-challenge/test
# Should get forwarded (might 404 if no solver running, but shouldn't be rejected)

curl -v http://<API-VIP>/something-else
# Should get 403 Forbidden
```

**7. Check cert-manager:**
```bash
kubectl get challenge
kubectl get certificaterequest
kubectl describe certificate <cert-name>
```

---

## Conclusion

You now have a comprehensive understanding of PR #398!

**You've learned:**
1. ✅ Background concepts (ACME, HTTP-01, cert-manager, VIPs)
2. ✅ The problem (API VIP can't serve HTTP-01 challenges)
3. ✅ The solution (HTTP01 Proxy as traffic router)
4. ✅ Architecture (DaemonSet with nftables + Go proxy)
5. ✅ Code implementation (API types, controller, reconciliation)
6. ✅ Testing strategy (unit tests, manual verification, e2e)
7. ✅ Security considerations (NetworkPolicies, minimal surface)
8. ✅ How to present this to your team

**Go forth and present with confidence!** 🚀

---

## Quick Reference Card

### One-Page Summary

**What:** HTTP01 proxy for bare-metal OpenShift API certificate challenges

**Why:** API VIP and Ingress VIP are separate; ACME HTTP-01 can't reach cert-manager

**How:** DaemonSet on masters redirects port 80 ACME requests to Ingress VIP

**Key Files:**
- `api/operator/v1alpha1/http01proxy_types.go` - API definition
- `pkg/controller/http01proxy/controller.go` - Main controller
- `pkg/controller/http01proxy/infrastructure.go` - Platform validation
- `bindata/http01-proxy/cert-manager-http01-proxy-daemonset.yaml` - DaemonSet manifest

**Key Concepts:**
- Singleton CRD (only one HTTP01Proxy allowed)
- Platform-aware (only bare-metal with distinct VIPs)
- Reconciliation loop (watches HTTP01Proxy, deploys resources)
- Finalizer pattern (cleanup on deletion)
- Feature gate (Alpha, disabled by default)

**Testing:**
- Unit tests: 654 passing
- Manual: CRC (degraded correctly), bare-metal (working)
- Script: `hack/verify-http01proxy.sh`

**Status:** Alpha, PR open, actively reviewed

---

*End of Learning Guide*

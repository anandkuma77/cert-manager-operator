# PR #398 - Logical Flow Walkthrough (52 Files)

This follows the **runtime execution flow** - the order things actually happen.

---

## PHASE 1: What Can Users Create? (API Definition)

**When:** Before anything runs  
**Purpose:** Define the HTTP01Proxy resource that users will create

**What this phase does:**
This phase defines the data structure and API that users interact with. It creates the HTTP01Proxy Custom Resource Definition (CRD) that allows users to request proxy deployment by creating a simple YAML resource.

**Why we need this:**
Kubernetes doesn't know about "HTTP01Proxy" by default. We must teach Kubernetes what an HTTP01Proxy is, what fields it has, and what validation rules apply. Without this, users couldn't create the resource and the controller would have nothing to watch.

| Step | Files | What It Does | Why It Does It |
|------|-------|--------------|----------------|
| 1a | `api/operator/v1alpha1/http01proxy_types.go` | Defines HTTP01Proxy CRD structure (Spec, Status, validation rules). | Provides the Go type definition that both humans and code generation tools use. Enforces singleton pattern (only one allowed named "default"). |
| 1b | `api/operator/v1alpha1/features.go` | Adds HTTP01Proxy feature gate (Alpha, disabled by default). | Allows shipping the code but keeping it disabled until ready for production. Users must explicitly enable with `--unsupported-addon-features=HTTP01Proxy=true`. |
| 1c | `api/operator/v1alpha1/zz_generated.deepcopy.go` | Auto-generated DeepCopy methods (required by Kubernetes). | Kubernetes requires all CRD types to have DeepCopy methods for safe copying of objects in memory. |
| 1d | `config/crd/bases/operator.openshift.io_http01proxies.yaml` | Generated CRD YAML that gets installed in Kubernetes. | Kubernetes API server needs this YAML to understand the HTTP01Proxy resource schema, validation rules, and enable `kubectl` operations. |
| 1e | `config/samples/operator.openshift.io_v1alpha1_http01proxy.yaml` | Sample HTTP01Proxy resource (shows users how to create one). | Provides documentation by example - users can copy/modify this to create their own HTTP01Proxy. |

**Result:** Users can now run `kubectl apply -f http01proxy.yaml`

---

## PHASE 2: User Creates HTTP01Proxy

**When:** User runs `kubectl create -f http01proxy.yaml`  
**What happens:** HTTP01Proxy resource is created in Kubernetes

---

## PHASE 3: Controller Receives Event

**When:** Controller watches HTTP01Proxy resources  
**Purpose:** Controller needs to react to the create event

**What this phase does:**
When a user creates an HTTP01Proxy resource, the controller must detect this event and begin reconciliation. This phase sets up the watch mechanism and provides the main reconciliation entry point.

**Why we need this:**
Controllers work on a "watch and react" model. The controller must register itself to watch HTTP01Proxy resources. When Kubernetes notifies the controller of a change (create/update/delete), the Reconcile() function is called to make reality match the desired state.

| Step | Files | What It Does | Why It Does It |
|------|-------|--------------|----------------|
| 3a | `pkg/operator/setup_manager.go` | Registers HTTP01Proxy controller with operator (if feature gate enabled). | Wires the controller into the operator's manager. Without this, the controller would never start watching resources. Feature gate check ensures it only runs when enabled. |
| 3b | `pkg/operator/starter.go` | Adds HTTP01Proxy to resources cached by operator. | The operator caches frequently accessed resources for performance. Adding HTTP01Proxy to the cache reduces API calls to Kubernetes. |
| 3c | `pkg/controller/http01proxy/controller.go` | **MAIN RECONCILE LOOP** - Entry point, handles create/update/delete, manages finalizers. | This is the "brain" - the Reconcile() function that gets called whenever HTTP01Proxy changes. It coordinates all other controller functions to achieve desired state. |
| 3d | `pkg/controller/http01proxy/constants.go` | Constants used by controller (names, labels, finalizers). | Centralizes all string constants (resource names, label keys) in one place. Prevents typos and makes it easy to find what labels/names are used. |

**Result:** Controller's Reconcile() function is called

---

## PHASE 4: Validate Platform (The Gatekeeper)

**When:** First thing controller does  
**Purpose:** Check if platform supports HTTP01 proxy

**What this phase does:**
Before deploying anything, the controller reads the cluster's Infrastructure configuration to determine the platform type (BareMetal, AWS, Azure, etc.) and extract VIP addresses. It then validates that the platform can support the HTTP01 proxy.

**Why we need this:**
The HTTP01 proxy only makes sense on BareMetal with separate API and Ingress VIPs. On AWS/cloud, both use the same load balancer. Deploying on the wrong platform wastes resources and could cause issues. This "gatekeeper" stops deployment early with a clear error message if requirements aren't met.

| Step | Files | What It Does | Why It Does It |
|------|-------|--------------|----------------|
| 4a | `pkg/controller/http01proxy/infrastructure.go` | **PLATFORM VALIDATION** - Fetches Infrastructure/cluster, checks if BareMetal, extracts API VIPs and Ingress VIPs, validates they're different. | Prevents deploying on unsuitable platforms. Extracts the VIP addresses that will later be injected into the proxy. Caches result for performance (platform doesn't change). |
| 4b | `pkg/controller/http01proxy/utils.go` | Status management - sets Available or Degraded conditions based on validation result. | Updates HTTP01Proxy status so users can see if validation passed or why it failed. Degraded=True with clear message helps users troubleshoot (e.g., "platform AWS not supported"). |

**Decision Point:**
- ✅ Valid (BareMetal with distinct VIPs) → Continue to Phase 5
- ❌ Invalid (wrong platform or same VIPs) → Set Degraded status, **STOP** (no deployment)

---

## PHASE 5: Deploy Resources (Orchestration)

**When:** After validation passes  
**Purpose:** Deploy all necessary resources

**What this phase does:**
Creates all Kubernetes resources needed for the proxy to run: identity (ServiceAccount), permissions (RBAC), security controls (NetworkPolicies), and finally the proxy itself (DaemonSet). Resources are deployed in a specific order to satisfy dependencies.

**Why we need this:**
The proxy pods need proper identity, permissions, and security controls before they can run. The order matters: ServiceAccount must exist before RBAC can reference it, RBAC must exist before DaemonSet uses the ServiceAccount, etc. The orchestrator ensures everything is created in the right sequence.

| Step | Files | What It Does | Why It Does It |
|------|-------|--------------|----------------|
| 5a | `pkg/controller/http01proxy/install_http01proxy.go` | **ORCHESTRATOR** - Calls reconcile functions in order (SA → RBAC → NetworkPolicies → DaemonSet). | Coordinates the deployment sequence. Ensures dependencies are met (e.g., ServiceAccount exists before DaemonSet tries to use it). Returns error immediately if any step fails. |

**Sub-steps (in order):**

### 5.1: Deploy ServiceAccount

**What:** Creates identity for proxy pods  
**Why:** Pods need a ServiceAccount to run. This SA will be bound to RBAC roles and OpenShift SCC in next steps.

| File | What It Does | Why It Does It |
|------|--------------|----------------|
| `pkg/controller/http01proxy/serviceaccounts.go` | Creates ServiceAccount for proxy pods. | Every pod in Kubernetes runs with a ServiceAccount. The proxy needs a dedicated SA so we can bind it to privileged SCC without affecting other pods. |
| `bindata/http01-proxy/cert-manager-http01-proxy-serviceaccount.yaml` | ServiceAccount template. | Template loaded by controller code. Using a template keeps deployment manifests version-controlled and separate from controller logic. |

### 5.2: Deploy RBAC

**What:** Creates permissions for the proxy ServiceAccount  
**Why:** OpenShift requires ServiceAccount to be explicitly bound to privileged SCC to use hostNetwork + NET_ADMIN.

| File | What It Does | Why It Does It |
|------|--------------|----------------|
| `pkg/controller/http01proxy/rbacs.go` | Creates ClusterRole and ClusterRoleBindings. | Manages all RBAC resources in one place. Handles both the ClusterRole binding and the SCC binding. |
| `bindata/http01-proxy/cert-manager-http01-proxy-clusterrole.yaml` | ClusterRole template (empty - proxy doesn't need K8s API). | Even though empty, required for consistent RBAC pattern. Future versions might need K8s API access. |
| `bindata/http01-proxy/cert-manager-http01-proxy-clusterrolebinding.yaml` | Binds SA to ClusterRole. | Links the ServiceAccount to the ClusterRole. Standard Kubernetes RBAC pattern. |
| `bindata/http01-proxy/cert-manager-http01-proxy-scc-rolebinding.yaml` | Binds SA to privileged SCC (needed for hostNetwork + NET_ADMIN). | **CRITICAL** - Without this binding, OpenShift blocks pods from using hostNetwork and NET_ADMIN capability. This enables the privileged features the proxy requires. |

### 5.3: Deploy NetworkPolicies

**What:** Restricts network traffic to/from proxy pods  
**Why:** Defense in depth - even though proxy runs as root with NET_ADMIN, limit its network access to only what's needed.

| File | What It Does | Why It Does It |
|------|--------------|----------------|
| `pkg/controller/http01proxy/networkpolicies.go` | Creates NetworkPolicies for security. | Deploys both ingress deny-all and egress allow policies. Defense in depth security. |
| `bindata/networkpolicies/http01-proxy-deny-all-networkpolicy.yaml` | Denies all ingress to proxy pods. | Proxy doesn't need incoming network connections (nftables redirects traffic locally). Deny all ingress reduces attack surface. |
| `bindata/networkpolicies/http01-proxy-allow-egress-networkpolicy.yaml` | Allows egress on ports 80, 443, 6443. | Proxy needs to forward to Ingress VIP (port 80) and may need HTTPS/K8s API. Allow only necessary ports, block everything else. |

### 5.4: Deploy DaemonSet ⭐ THE KEY STEP

**What:** Deploys proxy pods on all master nodes with VIP addresses injected  
**Why:** This is where the rubber meets the road - VIPs get passed to the proxy container so it knows where to forward traffic.

| File | What It Does | Why It Does It |
|------|--------------|----------------|
| `pkg/controller/http01proxy/daemonsets.go` | **CRITICAL** - Loads DaemonSet template, **injects API_VIP and INGRESS_VIP as environment variables**, creates/updates DaemonSet. | **THE MOST IMPORTANT FILE** - This is where the controller tells the proxy which VIPs to use. Without this injection, the proxy wouldn't know where to forward requests. Decouples operator code from proxy container code. |
| `bindata/http01-proxy/cert-manager-http01-proxy-daemonset.yaml` | DaemonSet template (hostNetwork, NET_ADMIN, runs on masters). | Defines how proxy pods run: on masters (VIP floats between them), with hostNetwork (to intercept VIP traffic), with NET_ADMIN (to modify nftables). Template is modified by controller to add VIP env vars. |

**Result:** DaemonSet created with VIPs injected as env vars

---

## PHASE 6: Kubernetes Schedules Pods

**When:** After DaemonSet is created  
**What happens:** Kubernetes schedules proxy pods on all master nodes  
**Files involved:** None (Kubernetes does this)

**Result:** Proxy pods running on each master with environment variables:
- `API_VIP=192.168.1.100`
- `INGRESS_VIP=192.168.1.101`

---

## PHASE 7: Proxy Container Runs (Different Repo)

**When:** Pods start  
**What happens:** Proxy container (from different repo) reads env vars and starts proxying  
**Files involved:** None (proxy logic is in https://github.com/sebrandon1/cert-mgr-http01-proxy)

**Result:** ACME requests to API VIP get forwarded to Ingress VIP ✅

---

## SUPPORTING FILES (Used by phases above)

### Configuration & Build

| File | What It Does | Why It Does It |
|------|--------------|----------------|
| `config/manager/manager.yaml` | Defines operator deployment with proxy image env vars. | Operator pods need to know which container image to deploy for the proxy. Env vars like `RELATED_IMAGE_CERT_MANAGER_HTTP01PROXY` tell the operator what image to use. |
| `config/rbac/role.yaml` | Grants operator permissions to manage HTTP01Proxies, DaemonSets, etc. | Operator needs K8s RBAC permissions to create/update/delete resources (DaemonSets, ServiceAccounts, etc.). Without these rules, all operations would fail with 403 Forbidden. |
| `Makefile` | Adds proxy image variables for local development. | Developers need to override proxy image during testing/development. Makefile centralizes these variables so they're easy to change without editing YAML. |
| `config/crd/kustomization.yaml` | Includes http01proxies CRD in kustomize build. | Kustomize needs to know which CRDs to include when building deployment manifests. This file adds HTTP01Proxy to the list. |

### Auto-Generated Client Code

**Purpose:** Allows controller to programmatically access HTTP01Proxy resources

| Files (10 total) | What They Do | Why They Do It |
|------------------|--------------|----------------|
| `pkg/operator/clientset/versioned/typed/operator/v1alpha1/http01proxy.go` | Typed client for CRUD operations. | Controller needs to Get/List/Update HTTP01Proxy resources. This provides type-safe Go API instead of raw REST calls. |
| `pkg/operator/clientset/versioned/typed/operator/v1alpha1/fake/fake_http01proxy.go` | Fake client for testing. | Unit tests need to mock K8s API without running a real cluster. Fake client simulates API responses. |
| `pkg/operator/clientset/versioned/typed/operator/v1alpha1/fake/fake_operator_client.go` | Fake operator client. | Part of fake client infrastructure for tests. |
| `pkg/operator/clientset/versioned/typed/operator/v1alpha1/generated_expansion.go` | Expansion hooks. | Allows adding custom client methods beyond standard CRUD if needed. Future extensibility. |
| `pkg/operator/clientset/versioned/typed/operator/v1alpha1/operator_client.go` | Operator client interface. | Defines contract for operator client. Abstraction allows swapping real/fake implementations. |
| `pkg/operator/informers/externalversions/generic.go` | Generic informer. | Controller needs to watch HTTP01Proxy changes. Informer provides efficient caching and event notifications. |
| `pkg/operator/informers/externalversions/operator/v1alpha1/http01proxy.go` | HTTP01Proxy informer (watches changes). | Listens for create/update/delete events on HTTP01Proxy. Controller reconciles when events arrive. |
| `pkg/operator/informers/externalversions/operator/v1alpha1/interface.go` | Informer interface. | Standard interface for informers. Enables dependency injection in tests. |
| `pkg/operator/listers/operator/v1alpha1/expansion_generated.go` | Lister expansion. | Future extensibility for custom list operations. |
| `pkg/operator/listers/operator/v1alpha1/http01proxy.go` | HTTP01Proxy lister (efficient queries). | Informer caches resources in-memory. Lister queries that cache instead of hitting K8s API repeatedly. Performance optimization. |

### Auto-Generated ApplyConfiguration

**Purpose:** Enables server-side apply for HTTP01Proxy

| Files (6 total) | What They Do | Why They Do It |
|-----------------|--------------|----------------|
| `pkg/operator/applyconfigurations/operator/v1alpha1/http01proxy.go` | Apply config for HTTP01Proxy. | Server-side apply needs structured builders for each API type. This generates typed builders for HTTP01Proxy fields. |
| `pkg/operator/applyconfigurations/operator/v1alpha1/http01proxyspec.go` | Apply config for Spec. | Builder for Spec fields. Enables partial updates without overwriting unrelated fields. |
| `pkg/operator/applyconfigurations/operator/v1alpha1/http01proxystatus.go` | Apply config for Status. | Builder for Status fields. Controller uses this to update status subresource. |
| `pkg/operator/applyconfigurations/operator/v1alpha1/http01proxycustomdeploymentspec.go` | Apply config for CustomDeployment. | Builder for CustomDeployment spec (nested in HTTP01ProxySpec). |
| `pkg/operator/applyconfigurations/internal/internal.go` | Internal registry. | Tracks type metadata for apply configurations. Internal plumbing. |
| `pkg/operator/applyconfigurations/utils.go` | Utility functions. | Helper functions for apply config builders. |

### Asset Embedding

| File | What It Does | Why It Does It |
|------|--------------|----------------|
| `pkg/operator/assets/bindata.go` | Embeds all YAML manifests (bindata/*) into Go binary using go:embed. | Controller needs access to YAML templates at runtime. Embedding means single binary deployment - no need to ship separate manifest files. |

### OLM Bundle

**Purpose:** Allows installation via Operator Lifecycle Manager

| Files | What They Do | Why They Do It |
|-------|--------------|----------------|
| `bundle/manifests/operator.openshift.io_http01proxies.yaml` | HTTP01Proxy CRD for OLM. | OLM needs CRD manifests to install them before deploying operator. Users installing via OLM get the CRD automatically. |
| `bundle/manifests/cert-manager-operator.clusterserviceversion.yaml` | ClusterServiceVersion with HTTP01Proxy info, permissions, related images. | OLM uses CSV to understand what the operator does, what permissions it needs, what CRDs it owns. The operator marketplace displays this metadata. |

### Testing & Tools

| Files | What They Do | Why They Do It |
|-------|--------------|----------------|
| `pkg/features/features_test.go` | Tests that HTTP01Proxy feature gate is disabled by default. | Feature is Alpha/TechPreview. Test ensures it's not accidentally enabled by default, which would break upgrades. |
| `hack/verify-http01proxy.sh` | Script to verify deployment (checks DaemonSet, nftables, forwarding). | Manual verification tool for developers/QE. Checks that proxy is actually working end-to-end, not just deployed. |

### Development/Temporary Files

| Files | What They Do | Why They Do It |
|-------|--------------|----------------|
| `CLAUDE.md` | Documentation for Claude Code AI. | Helps AI assistant understand project structure and conventions. Not part of production code. |
| `TODO-cleanup.md` | TODO items. | Developer notes for future cleanup. Temporary file. |
| `.golangci.bck.yaml` | Backup of linter config. | Backup made before modifying linter settings. Temporary file. |
| `rebase_all.sh` | Development script. | Helper script for branch management during development. Not part of production code. |

---

## Summary: The Complete Flow

```
┌─────────────────────────────────────────────────────────────┐
│ PHASE 1: API Definition (5 files)                          │
│ Define HTTP01Proxy CRD structure                           │
└────────────────────────┬────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────────┐
│ PHASE 2: User Creates Resource                             │
│ kubectl apply -f http01proxy.yaml                          │
└────────────────────────┬────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────────┐
│ PHASE 3: Controller Receives Event (4 files)               │
│ Reconcile() function called                                │
└────────────────────────┬────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────────┐
│ PHASE 4: Validate Platform (2 files)                       │
│ Check: BareMetal? Distinct VIPs?                           │
│   ├─ Valid → Continue                                      │
│   └─ Invalid → Set Degraded, STOP                          │
└────────────────────────┬────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────────┐
│ PHASE 5: Deploy Resources (12 files)                       │
│ 5.1 ServiceAccount                                          │
│ 5.2 RBAC (ClusterRole, Bindings, SCC)                      │
│ 5.3 NetworkPolicies                                         │
│ 5.4 DaemonSet ⭐ INJECT VIPs HERE!                         │
└────────────────────────┬────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────────┐
│ PHASE 6: Kubernetes Schedules Pods                         │
│ Pods run on all masters with env vars:                     │
│   - API_VIP=192.168.1.100                                  │
│   - INGRESS_VIP=192.168.1.101                              │
└────────────────────────┬────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────────┐
│ PHASE 7: Proxy Runs (different repo)                       │
│ Forwards ACME requests: API VIP → Ingress VIP ✅           │
└─────────────────────────────────────────────────────────────┘

Supporting: 29 files (clients, configs, OLM, tests, etc.)
```

---

## File Count by Phase

| Phase | Files | Purpose |
|-------|-------|---------|
| **Phase 1: API** | 5 | Define what users create |
| **Phase 3: Controller** | 4 | Receive events |
| **Phase 4: Validate** | 2 | Check platform |
| **Phase 5: Deploy** | 12 | Create resources + inject VIPs |
| **Supporting** | 29 | Clients, configs, OLM, tests |
| **TOTAL** | **52** | |

---

## The Critical Path (7 files to explain the core)

If time is limited, explain these 7 files in order:

1. **`api/operator/v1alpha1/http01proxy_types.go`** - What users create
2. **`pkg/controller/http01proxy/controller.go`** - Reconcile entry point
3. **`pkg/controller/http01proxy/infrastructure.go`** - Validate platform, get VIPs
4. **`pkg/controller/http01proxy/install_http01proxy.go`** - Orchestrate deployment
5. **`pkg/controller/http01proxy/daemonsets.go`** - ⭐ Inject VIPs as env vars
6. **`bindata/http01-proxy/cert-manager-http01-proxy-daemonset.yaml`** - DaemonSet template
7. **`pkg/controller/http01proxy/utils.go`** - Status updates

---

**This order follows the actual runtime flow from user action to running proxy!**

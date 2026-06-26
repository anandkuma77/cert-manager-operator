# PR #398 - Simple Walkthrough Path (52 Files)

This is the logical path to explain all 52 files in this PR to your team.

---

## STEP 1: Define the API (What users create)

**Files: 3**

| File | What It Does |
|------|--------------|
| `api/operator/v1alpha1/http01proxy_types.go` | Defines HTTP01Proxy CRD structure - the resource users create. Contains Spec (user input) and Status (controller output). |
| `api/operator/v1alpha1/features.go` | Adds HTTP01Proxy feature gate (Alpha, disabled by default). Users enable with `--unsupported-addon-features=HTTP01Proxy=true`. |
| `api/operator/v1alpha1/zz_generated.deepcopy.go` | Auto-generated DeepCopy methods for HTTP01Proxy types (required by Kubernetes). |

---

## STEP 2: Generate CRD and samples

**Files: 3**

| File | What It Does |
|------|--------------|
| `config/crd/bases/operator.openshift.io_http01proxies.yaml` | Generated CRD (Custom Resource Definition) that Kubernetes installs. Defines the API schema with validation rules. |
| `config/crd/kustomization.yaml` | Updated to include http01proxies CRD in kustomize build. |
| `config/samples/operator.openshift.io_v1alpha1_http01proxy.yaml` | Sample HTTP01Proxy resource showing users how to create one. |

---

## STEP 3: Generate Kubernetes clients

**Files: 10 (auto-generated)**

These files are auto-generated to allow programmatic access to HTTP01Proxy resources.

| File | What It Does |
|------|--------------|
| `pkg/operator/clientset/versioned/typed/operator/v1alpha1/http01proxy.go` | Typed client for HTTP01Proxy (Get, List, Create, Update, Delete operations). |
| `pkg/operator/clientset/versioned/typed/operator/v1alpha1/fake/fake_http01proxy.go` | Fake client for testing. |
| `pkg/operator/clientset/versioned/typed/operator/v1alpha1/fake/fake_operator_client.go` | Updated fake client to include HTTP01Proxy. |
| `pkg/operator/clientset/versioned/typed/operator/v1alpha1/generated_expansion.go` | Expansion hooks for custom client methods. |
| `pkg/operator/clientset/versioned/typed/operator/v1alpha1/operator_client.go` | Updated operator client interface. |
| `pkg/operator/informers/externalversions/generic.go` | Generic informer updated for HTTP01Proxy. |
| `pkg/operator/informers/externalversions/operator/v1alpha1/http01proxy.go` | Informer for watching HTTP01Proxy changes. |
| `pkg/operator/informers/externalversions/operator/v1alpha1/interface.go` | Informer interface updated. |
| `pkg/operator/listers/operator/v1alpha1/expansion_generated.go` | Lister expansion. |
| `pkg/operator/listers/operator/v1alpha1/http01proxy.go` | Lister for efficient HTTP01Proxy queries. |

---

## STEP 4: Generate ApplyConfigurations

**Files: 5 (auto-generated)**

These enable server-side apply for HTTP01Proxy resources.

| File | What It Does |
|------|--------------|
| `pkg/operator/applyconfigurations/operator/v1alpha1/http01proxy.go` | Apply configuration for HTTP01Proxy. |
| `pkg/operator/applyconfigurations/operator/v1alpha1/http01proxyspec.go` | Apply configuration for Spec. |
| `pkg/operator/applyconfigurations/operator/v1alpha1/http01proxystatus.go` | Apply configuration for Status. |
| `pkg/operator/applyconfigurations/operator/v1alpha1/http01proxycustomdeploymentspec.go` | Apply configuration for CustomDeployment. |
| `pkg/operator/applyconfigurations/internal/internal.go` | Updated internal registry. |
| `pkg/operator/applyconfigurations/utils.go` | Updated utility functions. |

---

## STEP 5: Define deployment manifests (what gets deployed)

**Files: 7**

These YAML files define the resources that get deployed when HTTP01Proxy is created.

| File | What It Does |
|------|--------------|
| `bindata/http01-proxy/cert-manager-http01-proxy-daemonset.yaml` | **KEY FILE** - DaemonSet template for proxy pods. Runs on all master nodes with hostNetwork and NET_ADMIN. |
| `bindata/http01-proxy/cert-manager-http01-proxy-serviceaccount.yaml` | ServiceAccount for proxy pods. |
| `bindata/http01-proxy/cert-manager-http01-proxy-clusterrole.yaml` | ClusterRole (empty - proxy doesn't need K8s API access). |
| `bindata/http01-proxy/cert-manager-http01-proxy-clusterrolebinding.yaml` | ClusterRoleBinding (binds SA to ClusterRole). |
| `bindata/http01-proxy/cert-manager-http01-proxy-scc-rolebinding.yaml` | Binds ServiceAccount to privileged SCC (needed for hostNetwork + NET_ADMIN). |
| `bindata/networkpolicies/http01-proxy-deny-all-networkpolicy.yaml` | Denies all ingress to proxy pods (they don't need incoming connections). |
| `bindata/networkpolicies/http01-proxy-allow-egress-networkpolicy.yaml` | Allows egress on ports 80, 443, 6443 (to reach Ingress VIP and K8s API). |

---

## STEP 6: Embed manifests in binary

**Files: 1**

| File | What It Does |
|------|--------------|
| `pkg/operator/assets/bindata.go` | Embeds YAML manifests into Go binary using go:embed. Controller reads these at runtime. |

---

## STEP 7: Implement the controller

**Files: 7**

These are the core controller files that implement the reconciliation logic.

| File | What It Does |
|------|--------------|
| `pkg/controller/http01proxy/constants.go` | Constants: resource names, labels, finalizers, env var names. |
| `pkg/controller/http01proxy/controller.go` | **Main reconciliation loop** - watches HTTP01Proxy, handles create/update/delete, manages finalizers. |
| `pkg/controller/http01proxy/infrastructure.go` | **Platform validation** - discovers platform type and VIPs from Infrastructure/cluster, validates BareMetal with distinct VIPs. |
| `pkg/controller/http01proxy/install_http01proxy.go` | **Orchestrates deployment** - calls all reconcile functions in order (SA → RBAC → NetworkPolicies → DaemonSet). |
| `pkg/controller/http01proxy/daemonsets.go` | **CRITICAL** - Deploys DaemonSet and **injects VIP addresses as environment variables**. |
| `pkg/controller/http01proxy/serviceaccounts.go` | Creates/updates ServiceAccount. |
| `pkg/controller/http01proxy/rbacs.go` | Creates/updates ClusterRole and ClusterRoleBindings. |
| `pkg/controller/http01proxy/networkpolicies.go` | Creates/updates NetworkPolicies. |
| `pkg/controller/http01proxy/utils.go` | Helper functions for status updates, conditions, finalizers. |

---

## STEP 8: Wire controller into operator

**Files: 2**

| File | What It Does |
|------|--------------|
| `pkg/operator/setup_manager.go` | Registers HTTP01Proxy controller with manager (only if feature gate enabled). |
| `pkg/operator/starter.go` | Adds HTTP01Proxy to list of resources cached by operator. |

---

## STEP 9: Update RBAC permissions

**Files: 1**

| File | What It Does |
|------|--------------|
| `config/rbac/role.yaml` | Grants operator permissions to manage HTTP01Proxies, DaemonSets, ServiceAccounts, ClusterRoles, ClusterRoleBindings, NetworkPolicies, and read Infrastructure. |

---

## STEP 10: Update operator deployment

**Files: 1**

| File | What It Does |
|------|--------------|
| `config/manager/manager.yaml` | Adds environment variables for proxy image (`RELATED_IMAGE_CERT_MANAGER_HTTP01PROXY`) and version. |

---

## STEP 11: Update OLM bundle

**Files: 2**

| File | What It Does |
|------|--------------|
| `bundle/manifests/operator.openshift.io_http01proxies.yaml` | CRD for OLM (Operator Lifecycle Manager). |
| `bundle/manifests/cert-manager-operator.clusterserviceversion.yaml` | Updates CSV with HTTP01Proxy CRD, permissions, and related images. |

---

## STEP 12: Update Makefile

**Files: 1**

| File | What It Does |
|------|--------------|
| `Makefile` | Adds `RELATED_IMAGE_CERT_MANAGER_HTTP01PROXY` and `HTTP01PROXY_OPERAND_IMAGE_VERSION` variables for local development. |

---

## STEP 13: Add tests

**Files: 1**

| File | What It Does |
|------|--------------|
| `pkg/features/features_test.go` | Adds test to verify HTTP01Proxy feature gate is disabled by default. |

---

## STEP 14: Add verification script

**Files: 1**

| File | What It Does |
|------|--------------|
| `hack/verify-http01proxy.sh` | Script to verify HTTP01Proxy deployment (checks DaemonSet, nftables rules, forwarding behavior). |

---

## STEP 15: Development/temporary files

**Files: 4**

These are temporary or development files, not part of the actual feature.

| File | What It Does |
|------|--------------|
| `CLAUDE.md` | Documentation for Claude Code (AI assistant). |
| `TODO-cleanup.md` | TODO items (temporary). |
| `.golangci.bck.yaml` | Backup of linter config (temporary). |
| `rebase_all.sh` | Development script (temporary). |

---

## Summary by Category

| Category | File Count | Purpose |
|----------|------------|---------|
| **API Definition** | 3 | Define HTTP01Proxy CRD structure |
| **CRD & Samples** | 3 | Generate CRD YAML and examples |
| **Generated Clients** | 10 | Auto-generated Kubernetes clients |
| **Generated ApplyConfigs** | 5 | Auto-generated server-side apply support |
| **Deployment Manifests** | 7 | YAML for DaemonSet, RBAC, NetworkPolicies |
| **Embedded Assets** | 1 | Embed manifests in binary |
| **Controller Logic** | 9 | Core reconciliation and deployment code |
| **Operator Integration** | 2 | Wire controller into operator |
| **RBAC & Config** | 2 | Operator permissions and deployment |
| **OLM Bundle** | 2 | Operator Lifecycle Manager support |
| **Build & Test** | 2 | Makefile and tests |
| **Tools** | 1 | Verification script |
| **Temporary/Dev** | 4 | Development files |
| **TOTAL** | **51** | (52 with one miscounted) |

---

## The Most Critical Files (Top 5)

If you only explain 5 files, explain these:

1. **`api/operator/v1alpha1/http01proxy_types.go`** - Defines what users create
2. **`pkg/controller/http01proxy/infrastructure.go`** - Validates platform and gets VIPs
3. **`pkg/controller/http01proxy/daemonsets.go`** - **Injects VIPs as env vars (THE KEY!)**
4. **`bindata/http01-proxy/cert-manager-http01-proxy-daemonset.yaml`** - DaemonSet template
5. **`pkg/controller/http01proxy/controller.go`** - Main reconciliation loop

---

## The Key Insight

**This PR does 3 main things:**

1. **Validates** platform is BareMetal with distinct API and Ingress VIPs
2. **Deploys** DaemonSet on master nodes with privileged access
3. **Injects** VIP addresses as environment variables so proxy knows where to forward

**The actual proxying logic (nftables, Go HTTP server) is in a DIFFERENT repository!**

---

**END OF WALKTHROUGH PATH**

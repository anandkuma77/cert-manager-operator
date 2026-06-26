# HTTP01 Proxy - Presentation Slides
## Visual Aids for Your Team Presentation

---

## Slide 1: Title Slide

```
╔══════════════════════════════════════════════════════════════╗
║                                                              ║
║      HTTP01 Proxy for cert-manager on Bare-Metal            ║
║                  OpenShift Clusters                          ║
║                                                              ║
║                    PR #398 Deep Dive                         ║
║                                                              ║
║                   [Your Name Here]                           ║
║                   [Date]                                     ║
║                                                              ║
╚══════════════════════════════════════════════════════════════╝
```

---

## Slide 2: The Problem - Visual Diagram

```
                    ACME Server (Let's Encrypt)
                            │
                            │ "Prove you own
                            │  api.cluster.example.com"
                            │
                            ↓
                     DNS Lookup
        api.cluster.example.com → 192.168.1.100 (API VIP)
                            │
                            ↓
    ┌───────────────────────────────────────────────────┐
    │        API VIP: 192.168.1.100                     │
    │                                                    │
    │    ╔═══════════════════════════════════╗         │
    │    ║  Kubernetes API Server             ║         │
    │    ║                                    ║         │
    │    ║  ✅ Port 6443: Kubernetes API     ║         │
    │    ║  ❌ Port 80: NOT AVAILABLE        ║         │
    │    ╚═══════════════════════════════════╝         │
    │                                                    │
    │    ACME request to port 80 → FAILS ❌            │
    └───────────────────────────────────────────────────┘

    Meanwhile, cert-manager solver is here:
    ┌───────────────────────────────────────────────────┐
    │     Ingress VIP: 192.168.1.101                    │
    │                                                    │
    │    ╔═══════════════════════════════════╗         │
    │    ║  OpenShift Router                  ║         │
    │    ║                                    ║         │
    │    ║  ✅ Port 80: HTTP                 ║         │
    │    ║  ✅ cert-manager solver Pod       ║         │
    │    ╚═══════════════════════════════════╝         │
    │                                                    │
    │    But ACME requests don't come here! ❌          │
    └───────────────────────────────────────────────────┘

          Problem: VIP mismatch blocks HTTP-01 challenges
```

---

## Slide 3: Why Two VIPs? (Background)

```
          Bare-Metal OpenShift Architecture

    ┌─────────────────────────────────────────────────┐
    │                                                  │
    │         External Network (Internet)              │
    │                                                  │
    └────────────┬─────────────────┬──────────────────┘
                 │                 │
                 │                 │
          ┌──────▼──────┐   ┌──────▼──────┐
          │  API VIP    │   │ Ingress VIP │
          │192.168.1.100│   │192.168.1.101│
          └──────┬──────┘   └──────┬──────┘
                 │                 │
                 │                 │
    ┌────────────┴─────────────────┴──────────────────┐
    │         Control Plane Nodes                      │
    │  ┌─────────┐  ┌─────────┐  ┌─────────┐         │
    │  │Master 1 │  │Master 2 │  │Master 3 │         │
    │  │         │  │         │  │         │         │
    │  │ K8s API │  │ K8s API │  │ K8s API │         │
    │  │ Router  │  │ Router  │  │ Router  │         │
    │  └─────────┘  └─────────┘  └─────────┘         │
    └──────────────────────────────────────────────────┘

    Why Two VIPs?
    • Separation of concerns
      - API VIP → Cluster management (K8s API)
      - Ingress VIP → Application traffic (user apps)
    
    • Different availability requirements
      - API: Always available for cluster operations
      - Ingress: Can scale independently
    
    • Security isolation
      - API: Typically restricted access
      - Ingress: Public-facing
```

---

## Slide 4: What is HTTP-01 Challenge?

```
   ACME HTTP-01 Challenge Flow (Normal Case - Cloud)

   Step 1: Request certificate
   ┌──────┐                           ┌─────────────┐
   │ User │──"Give me cert for"──────→│ ACME Server │
   └──────┘   example.com              │(Let's Encrypt)│
                                       └─────────────┘

   Step 2: ACME issues challenge
   ┌─────────────┐                     ┌──────┐
   │ ACME Server │──"Prove ownership"─→│ User │
   └─────────────┘   Here's TOKEN      └──────┘

   Step 3: User deploys solver
   ┌──────┐                           ┌─────────────────┐
   │ User │────Create HTTP server────→│ example.com:80  │
   └──────┘    Serve TOKEN at:         │                 │
            /.well-known/acme-chall…   │ Returns: TOKEN  │
                                       └─────────────────┘

   Step 4: ACME verifies
   ┌─────────────┐     HTTP GET        ┌─────────────────┐
   │ ACME Server │──────────────────→  │ example.com:80  │
   └─────────────┘  /.well-known/…     └─────────────────┘
                          │
                          ↓
                    Receives TOKEN
                          │
                          ↓
                   ✅ Proof verified!

   Step 5: Certificate issued
   ┌─────────────┐                     ┌──────┐
   │ ACME Server │──Here's your cert──→│ User │
   └─────────────┘                     └──────┘

   Problem on Bare-Metal OpenShift:
   • example.com (api.cluster) → API VIP
   • API VIP has NO HTTP server on port 80
   • Step 4 fails!
```

---

## Slide 5: The Solution - High Level

```
         HTTP01 Proxy = Smart Traffic Router

    ┌──────────────────────────────────────────┐
    │      Incoming request to API VIP:80       │
    └────────────────┬─────────────────────────┘
                     │
                     ↓
          ┌──────────────────────┐
          │   Is the path:        │
          │   /.well-known/       │
          │    acme-challenge/*   │
          │         ?             │
          └─────────┬────────┬────┘
                    │        │
              YES   │        │   NO
                    │        │
                    ↓        ↓
       ┌──────────────┐  ┌───────────────┐
       │   FORWARD     │  │   REJECT      │
       │     to        │  │   with        │
       │ Ingress VIP   │  │   403         │
       └───────┬───────┘  └───────────────┘
               │
               ↓
     ┌──────────────────┐
     │  Ingress VIP:80  │
     │                  │
     │  cert-manager    │
     │  solver Pod      │
     └──────────────────┘
               │
               ↓
        ✅ Returns TOKEN
               │
               ↓
        ✅ Challenge succeeds!
```

---

## Slide 6: The Solution - Detailed Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                   ACME Server (Let's Encrypt)                   │
│  GET http://api.cluster.example.com/.well-known/acme-chall…    │
└───────────────────────────────┬─────────────────────────────────┘
                                │
                                │ DNS: api.cluster → API VIP
                                ↓
┌─────────────────────────────────────────────────────────────────┐
│                  Control Plane Node (Master)                     │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │           API VIP: 192.168.1.100                          │ │
│  │                                                            │ │
│  │  ┌──────────────────────────────────────────────────┐    │ │
│  │  │  Step 1: nftables intercepts port 80            │    │ │
│  │  │                                                   │    │ │
│  │  │  Rule: tcp dport 80 → redirect to :8888         │    │ │
│  │  └──────────────────┬───────────────────────────────┘    │ │
│  │                     │                                     │ │
│  │                     ↓                                     │ │
│  │  ┌──────────────────────────────────────────────────┐    │ │
│  │  │  Step 2: HTTP01 Proxy (Go app on port 8888)     │    │ │
│  │  │                                                   │    │ │
│  │  │  if path.startsWith("/.well-known/acme-")        │    │ │
│  │  │      proxy to http://192.168.1.101:80/...       │    │ │
│  │  │  else                                             │    │ │
│  │  │      return 403 Forbidden                        │    │ │
│  │  └──────────────────┬───────────────────────────────┘    │ │
│  │                     │                                     │ │
│  └─────────────────────┼─────────────────────────────────────┘ │
│                        │                                       │
│                        │ Forward ACME request                  │
└────────────────────────┼───────────────────────────────────────┘
                         │
                         ↓
┌─────────────────────────────────────────────────────────────────┐
│                  Ingress VIP: 192.168.1.101                     │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │              OpenShift Router (HAProxy)                    │ │
│  │                        ↓                                   │ │
│  │  Routes request to cert-manager solver Pod                │ │
│  └────────────────────────────────────────────────────────────┘ │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │           cert-manager HTTP-01 Solver Pod                  │ │
│  │                                                            │ │
│  │  Serves: TOKEN.SIGNATURE                                  │ │
│  └────────────────────────────────────────────────────────────┘ │
└───────────────────────────────┬─────────────────────────────────┘
                                │
                                ↓
                         ✅ ACME Server
                            receives TOKEN
                                ↓
                         ✅ Certificate issued!
```

---

## Slide 7: New API Resource

```yaml
apiVersion: operator.openshift.io/v1alpha1
kind: HTTP01Proxy
metadata:
  name: default  # ← MUST be "default" (singleton)
  namespace: cert-manager-operator
spec:
  # How to deploy the proxy
  mode: DefaultDeployment  # or CustomDeployment
  
  # Only valid when mode=CustomDeployment
  customDeployment:
    internalPort: 8888  # Optional port override

status:
  # Current proxy image in use
  proxyImage: quay.io/bapalm/cert-mgr-http01-proxy:v0.2.0
  
  # Status conditions
  conditions:
  - type: Available
    status: "True"
    reason: ProxyDeployed
    message: "HTTP01 proxy DaemonSet is running"
  - type: Degraded
    status: "False"
    reason: AsExpected
  - type: Progressing
    status: "False"
    reason: AsExpected
```

**Key Features:**
- ✅ Singleton (only one allowed)
- ✅ Simple spec (minimal configuration)
- ✅ Extensible (CustomDeployment for future)
- ✅ Standard Kubernetes conditions

---

## Slide 8: What Gets Deployed

```
When you create HTTP01Proxy, the controller deploys:

┌─────────────────────────────────────────────────────────┐
│  ServiceAccount: cert-manager-http01-proxy              │
│  • Identity for the proxy Pods                          │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│  ClusterRole: cert-manager-http01-proxy                 │
│  • Permissions (currently empty - no K8s API needed)    │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│  ClusterRoleBinding: cert-manager-http01-proxy          │
│  • Binds ServiceAccount to ClusterRole                  │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│  ClusterRoleBinding: cert-manager-http01-proxy-privileged│
│  • Binds ServiceAccount to privileged SCC               │
│  • Required for hostNetwork + NET_ADMIN                 │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│  DaemonSet: cert-manager-http01-proxy                   │
│  • Runs on ALL control plane nodes                      │
│  • hostNetwork: true (share node's network)             │
│  • NET_ADMIN capability (modify nftables)               │
│  • Node selector: node-role.kubernetes.io/master        │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│  NetworkPolicy: http01-proxy-deny-all                   │
│  • Denies all ingress (proxy doesn't need incoming)     │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│  NetworkPolicy: http01-proxy-allow-egress               │
│  • Allows egress on ports 80, 443, 6443                 │
│  • Ingress VIP, K8s API access                          │
└─────────────────────────────────────────────────────────┘

All resources labeled:
  operator.openshift.io/managed-resource: "http01proxy"
```

---

## Slide 9: DaemonSet Deep Dive

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: cert-manager-http01-proxy
spec:
  template:
    spec:
      # ┌──────────────────────────────────────┐
      # │ Critical Security Settings            │
      # └──────────────────────────────────────┘
      
      # Share node's network namespace
      # Required to intercept traffic to VIP
      hostNetwork: true
      
      # Only run on control plane nodes
      nodeSelector:
        node-role.kubernetes.io/master: ""
      
      # Tolerate master taints
      tolerations:
      - key: node-role.kubernetes.io/master
        operator: Exists
        effect: NoSchedule
      
      containers:
      - name: http01-proxy
        image: ${RELATED_IMAGE_CERT_MANAGER_HTTP01PROXY}
        
        # Proxy listens on 8888
        ports:
        - containerPort: 8888
          hostPort: 8888  # Bind to host
        
        securityContext:
          # Can modify network (nftables)
          capabilities:
            add: [NET_ADMIN]
            drop: [ALL]
          
          # Must run as root (for nftables)
          runAsNonRoot: false
          
          # No privilege escalation
          allowPrivilegeEscalation: false
        
        # Minimal resources
        resources:
          requests:
            cpu: 10m
            memory: 32Mi
          limits:
            cpu: 100m
            memory: 64Mi
      
      # High priority (cluster-critical)
      priorityClassName: system-cluster-critical
```

**Why These Settings?**

| Setting | Reason |
|---------|--------|
| `hostNetwork: true` | Must intercept traffic to VIP (node's IP) |
| `NET_ADMIN` | Required to modify nftables/iptables |
| `runAsNonRoot: false` | nftables requires root |
| `nodeSelector: master` | VIP floats between masters, need on all |
| `system-cluster-critical` | Important for cert issuance |

---

## Slide 10: Controller Reconciliation Flow

```
                 Reconciliation Loop

        ┌────────────────────────────────┐
        │  Watch HTTP01Proxy resources   │
        │  Watch child resources          │
        │  (DaemonSet, RBAC, etc.)       │
        └─────────────┬──────────────────┘
                      │
                      ↓
           ┌──────────────────────┐
           │  Trigger received    │
           └──────────┬───────────┘
                      │
                      ↓
           ┌──────────────────────┐
           │  Fetch HTTP01Proxy   │
           └──────────┬───────────┘
                      │
                      ↓
           ┌──────────────────────┐
           │  Deletion timestamp? │
           └──────┬─────────┬─────┘
                  │ YES     │ NO
                  │         │
         ┌────────▼──────┐  │
         │  Clean up:    │  │
         │  • DaemonSet  │  │
         │  • RBAC       │  │
         │  • SA         │  │
         │  • NetPol     │  │
         └────────┬──────┘  │
                  │         │
         ┌────────▼──────┐  │
         │Remove finalizer│  │
         └────────┬──────┘  │
                  │         │
                  ↓         │
            ┌─────────┐    │
            │  Done   │    │
            └─────────┘    │
                           │
                ┌──────────▼──────────┐
                │  Add finalizer if   │
                │  not present        │
                └──────────┬──────────┘
                           │
                           ↓
                ┌──────────────────────┐
                │  Discover platform   │
                │  (cache if first)    │
                └──────────┬───────────┘
                           │
                           ↓
                ┌──────────────────────┐
                │  Validate platform   │
                └──────┬─────────┬─────┘
                       │         │
                 INVALID│         │VALID
                       │         │
         ┌─────────────▼─────┐   │
         │  Set Degraded:    │   │
         │  • Wrong platform │   │
         │  • Missing VIPs   │   │
         │  • Same VIPs      │   │
         └─────────┬─────────┘   │
                   │             │
                   ↓             │
            Update status        │
                   │             │
                   ↓             │
            ┌─────────┐          │
            │  Done   │          │
            └─────────┘          │
                                 │
                    ┌────────────▼────────────┐
                    │  Reconcile resources:   │
                    │  1. ServiceAccount      │
                    │  2. ClusterRole         │
                    │  3. ClusterRoleBindings │
                    │  4. NetworkPolicies     │
                    │  5. DaemonSet           │
                    └────────────┬────────────┘
                                 │
                                 ↓
                    ┌────────────────────────┐
                    │  Update status:        │
                    │  • Available=True      │
                    │  • Degraded=False      │
                    │  • Set proxyImage      │
                    └────────────┬───────────┘
                                 │
                                 ↓
                          ┌─────────┐
                          │  Done   │
                          └─────────┘
```

---

## Slide 11: Platform Validation

```
           Platform Detection & Validation

Step 1: Fetch Infrastructure/cluster
   ┌──────────────────────────────────┐
   │  Read cluster infrastructure CR   │
   │  apiVersion: config.openshift.io │
   │  kind: Infrastructure             │
   │  name: cluster                    │
   └────────────┬─────────────────────┘
                │
                ↓
Step 2: Extract platform type
   ┌──────────────────────────────────┐
   │  platformType = status.          │
   │    platformStatus.type            │
   └────────────┬─────────────────────┘
                │
                ↓
           ┌────────────┐
           │ BareMetal? │
           └─────┬──────┘
                 │
          ┌──────┴───────┐
          │ NO           │ YES
          │              │
          ↓              ↓
   ┌──────────────┐  ┌───────────────────────┐
   │  DEGRADED    │  │ Extract VIPs:         │
   │  "Platform   │  │ • apiServerInternalIPs│
   │   not        │  │ • ingressIPs          │
   │   supported" │  └──────────┬────────────┘
   └──────────────┘             │
                                ↓
                         ┌──────────────┐
                         │ Any VIP      │
                         │ empty?       │
                         └──────┬───────┘
                                │
                         ┌──────┴──────┐
                         │ YES         │ NO
                         │             │
                         ↓             ↓
                  ┌──────────────┐ ┌────────────────┐
                  │  DEGRADED    │ │ Any API VIP == │
                  │  "Missing    │ │ Ingress VIP?   │
                  │   VIPs"      │ └────────┬───────┘
                  └──────────────┘          │
                                     ┌──────┴──────┐
                                     │ YES         │ NO
                                     │             │
                                     ↓             ↓
                              ┌──────────────┐ ┌──────────┐
                              │  DEGRADED    │ │   OK!    │
                              │  "VIPs are   │ │  Deploy  │
                              │   same"      │ │  proxy   │
                              └──────────────┘ └──────────┘

Example Validation Results:

✅ VALID:
   Platform: BareMetal
   API VIPs: [192.168.1.100]
   Ingress VIPs: [192.168.1.101]

❌ INVALID - Wrong platform:
   Platform: AWS
   → "Platform AWS not supported"

❌ INVALID - Same VIPs:
   Platform: BareMetal
   API VIPs: [192.168.1.100]
   Ingress VIPs: [192.168.1.100]
   → "VIPs are the same, proxy not needed"
```

---

## Slide 12: Security Deep Dive

```
           Security Posture Analysis

┌────────────────────────────────────────────────────────┐
│  Attack Surface                                        │
├────────────────────────────────────────────────────────┤
│  ✅ Minimal: Only ACME challenge paths forwarded      │
│  ✅ All other requests rejected (403)                 │
│  ✅ No data stored, stateless proxy                   │
│  ✅ Read-only operations (no writes)                  │
└────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────┐
│  Privileged Requirements (Justified)                   │
├────────────────────────────────────────────────────────┤
│  • hostNetwork: true                                   │
│    Why: Must intercept traffic to VIP (node's IP)     │
│                                                        │
│  • NET_ADMIN capability                                │
│    Why: Modify nftables rules for port redirect       │
│                                                        │
│  • runAsNonRoot: false                                 │
│    Why: nftables modification requires root           │
│                                                        │
│  • Privileged SCC binding                              │
│    Why: OpenShift requires this for above settings    │
└────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────┐
│  Defense in Depth                                      │
├────────────────────────────────────────────────────────┤
│  Layer 1: NetworkPolicy                                │
│    • Deny all ingress (no incoming connections)       │
│    • Allow egress only on 80, 443, 6443               │
│                                                        │
│  Layer 2: Application Logic                            │
│    • Go code validates request path                   │
│    • Strict pattern match: /.well-known/acme-*/       │
│    • Rejects everything else                          │
│                                                        │
│  Layer 3: Kernel (nftables)                            │
│    • Packet redirection at kernel level               │
│    • Only port 80 traffic touched                     │
│    • API server (port 6443) unaffected                │
└────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────┐
│  What Could Go Wrong? (Threat Model)                   │
├────────────────────────────────────────────────────────┤
│  Scenario 1: Proxy exploited                           │
│    • Attacker gains control of proxy Pod              │
│    • Impact: Can't modify proxy logic (immutable)     │
│    • Impact: NetworkPolicy limits egress               │
│    • Mitigation: Pod restart resets state             │
│                                                        │
│  Scenario 2: Path validation bypass                    │
│    • Attacker finds way to forward non-ACME requests  │
│    • Impact: Could access Ingress VIP as API domain   │
│    • Mitigation: Strict regex in Go code              │
│    • Mitigation: Ingress still validates              │
│                                                        │
│  Scenario 3: Resource exhaustion                       │
│    • Flood of requests to port 80                     │
│    • Impact: Proxy Pod overwhelmed                    │
│    • Mitigation: Resource limits (100m CPU, 64Mi RAM) │
│    • Mitigation: Kubernetes restarts unhealthy Pods   │
│                                                        │
│  Scenario 4: Privilege escalation                      │
│    • Attacker tries to use NET_ADMIN for other things │
│    • Impact: Limited by NetworkPolicy egress rules    │
│    • Impact: No access to K8s API (ClusterRole empty) │
│    • Mitigation: allowPrivilegeEscalation: false      │
└────────────────────────────────────────────────────────┘
```

---

## Slide 13: Testing Strategy

```
          Testing Pyramid

               ┌───────┐
               │  E2E  │  End-to-End
               │ Tests │  • Real cluster
               │   1   │  • Real cert issuance
               └───┬───┘  • Let's Encrypt staging
                   │
              ┌────┴─────┐
              │ Manual   │  Manual Verification
              │ Testing  │  • Bare-metal cluster
              │    5     │  • CRC (SNO)
              └────┬─────┘  • Verification script
                   │
             ┌─────┴──────┐
             │ Controller │  Controller Tests
             │   Tests    │  • Reconciliation logic
             │     10     │  • Platform validation
             └─────┬──────┘  • Status updates
                   │
            ┌──────┴───────┐
            │ Unit Tests   │  Unit Tests
            │      654     │  • All packages
            │              │  • Edge cases
            └──────────────┘  • Error handling

Test Environments:

1. CRC (CodeReady Containers - Single Node)
   Platform: None
   Expected: Degraded status
   Result: ✅ Correctly degraded
   Message: "platform type None is not supported"

2. Bare-Metal MNO (Multi-Node OpenShift)
   Cluster: cnfdt16 (OCP 5.0)
   Nodes: 3 masters
   API VIP: 192.168.122.10
   Ingress VIP: 192.168.122.11
   Expected: DaemonSet deployed, proxy running
   Result: ✅ All verification checks pass
   Verified:
     • DaemonSet running on all 3 masters
     • nftables rules configured
     • ACME paths forwarded
     • Non-ACME paths rejected

Verification Script: hack/verify-http01proxy.sh

#!/bin/bash
# 1. Check HTTP01Proxy exists and is Available
# 2. Check DaemonSet has desired number of Pods
# 3. SSH to each master, check nftables rules
# 4. Test ACME path forwarding
# 5. Test non-ACME path rejection
# 6. Check NetworkPolicies applied
```

---

## Slide 14: Feature Gate & Maturity

```
         Alpha Feature Lifecycle

┌──────────────────────────────────────────────────────┐
│  Current Status: Alpha                               │
├──────────────────────────────────────────────────────┤
│  • Disabled by default                               │
│  • No backwards compatibility guarantees             │
│  • API may change                                    │
│  • Early adopter testing phase                       │
└──────────────────────────────────────────────────────┘

How to Enable:

  cert-manager-operator \
    --unsupported-addon-features=HTTP01Proxy=true

Graduation Criteria:

  Alpha → Beta:
    • Real-world testing on multiple clusters
    • Performance benchmarks
    • No known critical bugs
    • Documentation complete
    • E2E tests automated

  Beta → GA:
    • Proven stability (6+ months)
    • Multiple OpenShift versions tested
    • Customer deployments successful
    • API frozen (no breaking changes)
    • Production-ready monitoring/alerts

Timeline:
  ┌─────────┬──────────┬──────────┬──────────┐
  │  Alpha  │   Beta   │    GA    │ Deprecated│
  │ (Now)   │ (4.23?)  │ (4.24?)  │    (?)    │
  └─────────┴──────────┴──────────┴──────────┘
     ^         ^          ^
     │         │          │
   First     Feature   Enabled by
  version    stable    default
```

---

## Slide 15: Code Statistics

```
           PR #398 - By the Numbers

┌─────────────────────────────────────────────────────┐
│  Files Changed                                      │
├─────────────────────────────────────────────────────┤
│  Total: 52 files                                    │
│                                                     │
│  Breakdown:                                         │
│    • API types: 3 files                             │
│    • Controller code: 10 files                      │
│    • Generated code: 25 files                       │
│    • YAML manifests: 8 files                        │
│    • Config/RBAC: 4 files                           │
│    • Tests: 2 files                                 │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│  Lines of Code                                      │
├─────────────────────────────────────────────────────┤
│  Total: ~3,200 lines added                          │
│                                                     │
│  Breakdown:                                         │
│    • API types: ~100 lines                          │
│    • Controller logic: ~600 lines                   │
│    • Generated code: ~2,000 lines                   │
│    • YAML/config: ~500 lines                        │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│  Key Files to Review                                │
├─────────────────────────────────────────────────────┤
│  1. api/operator/v1alpha1/http01proxy_types.go      │
│     • API definition (109 lines)                    │
│                                                     │
│  2. pkg/controller/http01proxy/controller.go        │
│     • Main controller (193 lines)                   │
│                                                     │
│  3. pkg/controller/http01proxy/infrastructure.go    │
│     • Platform detection (102 lines)                │
│                                                     │
│  4. bindata/http01-proxy/*.yaml                     │
│     • Deployment manifests                          │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│  Test Coverage                                      │
├─────────────────────────────────────────────────────┤
│  • 654 unit tests in 28 packages: PASS ✅           │
│  • make verify: PASS ✅                             │
│  • golangci-lint: PASS ✅                           │
│  • Manual testing: PASS ✅                          │
└─────────────────────────────────────────────────────┘
```

---

## Slide 16: How to Use (Demo Script)

```
         Step-by-Step: Getting a Certificate

Prerequisites:
  • OpenShift bare-metal cluster
  • Distinct API and Ingress VIPs
  • cert-manager installed
  • HTTP01Proxy feature gate enabled

Step 1: Enable Feature Gate
  $ oc edit deployment cert-manager-operator-controller-manager \
      -n cert-manager-operator
  
  # Add to args:
  - --unsupported-addon-features=HTTP01Proxy=true

Step 2: Create HTTP01Proxy
  $ cat <<EOF | oc apply -f -
  apiVersion: operator.openshift.io/v1alpha1
  kind: HTTP01Proxy
  metadata:
    name: default
    namespace: cert-manager-operator
  spec:
    mode: DefaultDeployment
  EOF

Step 3: Verify Deployment
  $ oc get http01proxy default -o yaml
  # Check status.conditions for Available=True

  $ oc get daemonset cert-manager-http01-proxy
  # Should show DESIRED=CURRENT=READY

Step 4: Create ACME Issuer
  $ cat <<EOF | oc apply -f -
  apiVersion: cert-manager.io/v1
  kind: Issuer
  metadata:
    name: letsencrypt-staging
    namespace: default
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
  EOF

Step 5: Request Certificate
  $ cat <<EOF | oc apply -f -
  apiVersion: cert-manager.io/v1
  kind: Certificate
  metadata:
    name: api-cert
    namespace: default
  spec:
    secretName: api-cert-tls
    dnsNames:
    - api.cluster.example.com
    issuerRef:
      name: letsencrypt-staging
  EOF

Step 6: Watch the Magic
  $ oc get certificate -w
  NAME       READY   SECRET         AGE
  api-cert   False   api-cert-tls   5s
  api-cert   False   api-cert-tls   10s
  api-cert   True    api-cert-tls   45s  ← Success!

  $ oc get challenge
  # Shows HTTP-01 challenge completed

  $ oc get secret api-cert-tls -o yaml
  # Contains the certificate!
```

---

## Slide 17: Future Enhancements

```
           Roadmap & Future Work

┌─────────────────────────────────────────────────────┐
│  Short Term (Alpha → Beta)                          │
├─────────────────────────────────────────────────────┤
│  • IPv6 support (currently IPv4 only)               │
│  • Metrics & monitoring                             │
│    - Prometheus metrics                             │
│    - Request counters                               │
│    - Error rates                                    │
│  • Enhanced status reporting                        │
│    - Pod readiness count                            │
│    - Last challenge timestamp                       │
│  • Automated E2E tests in CI                        │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│  Medium Term (Beta → GA)                            │
├─────────────────────────────────────────────────────┤
│  • Official proxy image (not dev image)             │
│  • CustomDeployment mode implementation             │
│  • Support for alternative proxy implementations    │
│  • Performance optimization                         │
│  • Multi-cluster support                            │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│  Long Term (Post-GA)                                │
├─────────────────────────────────────────────────────┤
│  • Support for other platforms (Nutanix, etc.)      │
│  • Integration with cert-manager UI                 │
│  • Advanced routing rules                           │
│  • Rate limiting                                    │
│  • Certificate rotation automation                  │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│  Potential Extensions                               │
├─────────────────────────────────────────────────────┤
│  • DNS-01 challenge support (different problem)     │
│  • TLS-ALPN-01 challenge support                    │
│  • Multi-VIP configurations                         │
│  • Proxy HA improvements                            │
└─────────────────────────────────────────────────────┘
```

---

## Slide 18: Key Takeaways

```
╔══════════════════════════════════════════════════════╗
║                   KEY TAKEAWAYS                      ║
╚══════════════════════════════════════════════════════╝

1. THE PROBLEM
   • Bare-metal OpenShift has separate API and Ingress VIPs
   • ACME HTTP-01 challenges can't reach cert-manager
   • Manual certificate management is painful

2. THE SOLUTION
   • HTTP01 Proxy acts as smart traffic router
   • Forwards ACME challenges, rejects everything else
   • Minimal attack surface, secure by default

3. IMPLEMENTATION
   • New CRD: HTTP01Proxy (singleton)
   • Controller reconciles DaemonSet + RBAC + NetworkPolicies
   • Platform-aware: only deploys on bare-metal

4. ARCHITECTURE
   • DaemonSet on all masters (HA)
   • nftables for port redirect (80 → 8888)
   • Go app for path-based routing

5. SECURITY
   • Justified privileged access
   • Defense in depth (NetworkPolicy + app logic + kernel)
   • Minimal blast radius

6. MATURITY
   • Alpha feature (disabled by default)
   • Well-tested (654 unit tests, manual verification)
   • Clear graduation path

7. IMPACT
   • Enables automated certificate issuance for API
   • Uses Let's Encrypt (free, trusted)
   • Reduces operational burden

╔══════════════════════════════════════════════════════╗
║  This is a REAL problem with a CLEAN solution       ║
║  Following Kubernetes patterns and best practices   ║
╚══════════════════════════════════════════════════════╝
```

---

## Slide 19: Q&A Preparation

```
Common Questions & Answers:

Q: Why not just use a LoadBalancer?
A: Bare-metal doesn't have cloud LoadBalancers.
   VIPs are the bare-metal equivalent.

Q: Can this break my API server?
A: No. Proxy only touches port 80.
   API server runs on port 6443 (unaffected).

Q: What if someone DDoSes port 80?
A: Resource limits prevent Pod exhaustion.
   NetworkPolicy restricts egress.
   Kubernetes restarts unhealthy Pods.

Q: Why DaemonSet instead of Deployment?
A: VIP can fail over to any master.
   Need proxy on ALL masters, not just 1-2.

Q: Is this OpenShift-specific?
A: Implementation is, but pattern isn't.
   Upstream Kubernetes could adapt this.

Q: What about IPv6?
A: Current implementation is IPv4.
   IPv6 support is planned for Beta.

Q: Can I use my own proxy?
A: Not yet. CustomDeployment mode is
   future extension point.

Q: Why Alpha? Seems production-ready.
A: First version needs real-world testing.
   API may need adjustments.
   Beta = stable API, GA = default-enabled.

Q: What's the performance impact?
A: Minimal. Only ACME challenges affected.
   Regular API traffic unaffected.
   Proxy uses 10m CPU, 32Mi RAM.

Q: Can I disable it after enabling?
A: Yes. Delete the HTTP01Proxy resource.
   All child resources cleaned up automatically.
```

---

## Slide 20: Resources & References

```
┌─────────────────────────────────────────────────────┐
│  Links & Documentation                              │
├─────────────────────────────────────────────────────┤
│  • PR: github.com/openshift/cert-manager-operator   │
│         /pull/398                                   │
│                                                     │
│  • Enhancement: github.com/openshift/enhancements   │
│                 /pull/1929                          │
│                                                     │
│  • Jira: issues.redhat.com/browse/CM-716            │
│                                                     │
│  • Deployment Guide:                                │
│    gist.github.com/sebrandon1/                      │
│    457d18741c33a5eef5adf77a0c973106                │
│                                                     │
│  • Proxy Image Source:                              │
│    github.com/sebrandon1/cert-mgr-http01-proxy      │
│                                                     │
│  • cert-manager Docs:                               │
│    cert-manager.io/docs                             │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│  Learning Resources                                 │
├─────────────────────────────────────────────────────┤
│  • ACME Protocol: tools.ietf.org/html/rfc8555       │
│  • HTTP-01 Challenge:                               │
│    letsencrypt.org/docs/challenge-types             │
│  • Kubernetes Operators:                            │
│    kubernetes.io/docs/concepts/extend-kubernetes/   │
│    operator                                         │
│  • Controller Runtime:                              │
│    github.com/kubernetes-sigs/controller-runtime    │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│  Contact                                            │
├─────────────────────────────────────────────────────┤
│  • PR Author: Brandon Palm (@sebrandon1)            │
│  • Team: Red Hat cert-manager-operator team         │
│  • Slack: #forum-cert-manager (CoreOS workspace)    │
└─────────────────────────────────────────────────────┘

Thank you!
Questions?
```

---

## BONUS: Quick Reference Card

```
╔══════════════════════════════════════════════════════╗
║         HTTP01 Proxy - Quick Reference               ║
╚══════════════════════════════════════════════════════╝

WHAT:   HTTP01 proxy for bare-metal OpenShift API
        certificate challenges

WHY:    API VIP and Ingress VIP are separate;
        ACME HTTP-01 can't reach cert-manager

HOW:    DaemonSet on masters redirects port 80
        ACME requests to Ingress VIP

┌──────────────────────────────────────────────────────┐
│  Key Commands                                        │
├──────────────────────────────────────────────────────┤
│  # Create HTTP01Proxy                                │
│  $ oc apply -f http01proxy.yaml                      │
│                                                      │
│  # Check status                                      │
│  $ oc get http01proxy default -o yaml                │
│                                                      │
│  # Verify DaemonSet                                  │
│  $ oc get ds cert-manager-http01-proxy               │
│                                                      │
│  # Check logs                                        │
│  $ oc logs -l app=cert-manager-http01-proxy          │
│                                                      │
│  # Run verification script                           │
│  $ ./hack/verify-http01proxy.sh                      │
└──────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────┐
│  Troubleshooting                                     │
├──────────────────────────────────────────────────────┤
│  Problem: DaemonSet not deploying                    │
│  → Check platform: oc get infrastructure cluster     │
│  → Check status: oc get http01proxy default -o yaml  │
│                                                      │
│  Problem: Challenges failing                         │
│  → Check proxy logs                                  │
│  → Verify nftables: oc debug node/<master>           │
│  → Test manually: curl http://<API-VIP>/.well-known/ │
│                                                      │
│  Problem: "Degraded" status                          │
│  → Read condition message for reason                 │
│  → Likely wrong platform or same VIPs                │
└──────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────┐
│  Key Files                                           │
├──────────────────────────────────────────────────────┤
│  API: api/operator/v1alpha1/http01proxy_types.go     │
│  Controller: pkg/controller/http01proxy/*.go         │
│  Manifests: bindata/http01-proxy/*.yaml              │
│  CRD: config/crd/bases/operator.openshift.io_*.yaml  │
└──────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────┐
│  Metrics                                             │
├──────────────────────────────────────────────────────┤
│  • 52 files changed                                  │
│  • ~3,200 lines added                                │
│  • 654 unit tests passing                            │
│  • Alpha feature (disabled by default)               │
│  • Tested on CRC and bare-metal                      │
└──────────────────────────────────────────────────────┘
```

---

*End of Presentation Slides*

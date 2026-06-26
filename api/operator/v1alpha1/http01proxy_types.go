// FILE: api/operator/v1alpha1/http01proxy_types.go
//
// WHAT IT DOES (max 5 lines):
// Defines the HTTP01Proxy Custom Resource Definition (CRD) structure for Kubernetes.
// This is the API contract - it specifies what fields users can set when creating
// an HTTP01Proxy resource and what status the controller reports back. Includes
// validation rules (singleton enforcement, mode constraints) and struct tags for
// code generation (deepcopy, clients, CRD YAML).
//
// HOW IT DOES IT (max 5 lines):
// Uses Go structs with kubebuilder markers (comments starting with +kubebuilder:)
// that code-generator tools read to produce CRD YAML, client code, and validation.
// The init() function registers types with the Scheme so Kubernetes can serialize/
// deserialize HTTP01Proxy objects. Validation happens via CEL rules embedded in
// markers (XValidation) which get compiled into the CRD's OpenAPI schema.

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1" // Kubernetes metadata types (ObjectMeta, TypeMeta, etc.)
)

// init runs when package is first imported - registers HTTP01Proxy types with the API scheme
func init() {
	// SchemeBuilder.Register adds HTTP01Proxy and HTTP01ProxyList to the scheme
	// This allows K8s runtime to recognize these types when encoding/decoding API objects
	SchemeBuilder.Register(&HTTP01Proxy{}, &HTTP01ProxyList{})
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// ↑ Tells deepcopy-gen to make HTTP01ProxyList implement runtime.Object interface
// +kubebuilder:object:root=true
// ↑ Tells kubebuilder this is a root API object (not nested)

// HTTP01ProxyList is a list of HTTP01Proxy objects.
// Used by K8s when returning multiple items from List() API calls
type HTTP01ProxyList struct {
	metav1.TypeMeta `json:",inline"` // Embedded: provides Kind and APIVersion fields

	// metadata is the standard list's metadata.
	// Contains list-level metadata like resourceVersion, continue token for pagination
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ListMeta `json:"metadata"`
	Items           []HTTP01Proxy `json:"items"` // Array of actual HTTP01Proxy objects
}

// +genclient
// ↑ Tells client-gen to generate typed client for this type
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// ↑ Generate DeepCopy method implementing runtime.Object
// +kubebuilder:object:root=true
// ↑ This is a root API object
// +kubebuilder:subresource:status
// ↑ Status field should be a separate subresource (allows separate RBAC, optimistic locking)
// +kubebuilder:resource:path=http01proxies,scope=Namespaced,categories={cert-manager-operator},shortName=http01proxy
// ↑ Defines resource path in K8s API: /apis/operator.openshift.io/v1alpha1/namespaces/{ns}/http01proxies
// +kubebuilder:printcolumn:name="Mode",type="string",JSONPath=".spec.mode"
// ↑ When user runs `kubectl get http01proxy`, show Mode column from spec.mode field
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// ↑ Show Ready status (True/False/Unknown) from conditions array in kubectl output
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].message"
// ↑ Show human-readable message explaining Ready condition status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// ↑ Show how long ago this resource was created
// +kubebuilder:metadata:labels={"app.kubernetes.io/name=http01proxy", "app.kubernetes.io/part-of=cert-manager-operator"}
// ↑ Automatically add these labels to HTTP01Proxy resources (helps with queries/organization)

// HTTP01Proxy describes the configuration for the HTTP01 challenge proxy
// that redirects traffic from the API endpoint on port 80 to ingress routers.
// This enables cert-manager to perform HTTP01 ACME challenges for API endpoint certificates.
// The name must be `default` to make HTTP01Proxy a singleton.
//
// When an HTTP01Proxy is created, the proxy DaemonSet is deployed on control plane nodes.
//
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default'",message="http01proxy is a singleton, .metadata.name must be 'default'"
// ↑ CEL validation rule: enforces singleton pattern by requiring name to be exactly "default"
// This prevents users from creating multiple HTTP01Proxy objects (only one allowed per cluster)
// +operator-sdk:csv:customresourcedefinitions:displayName="HTTP01Proxy"
// ↑ Display name shown in OLM/OperatorHub UI
type HTTP01Proxy struct {
	metav1.TypeMeta `json:",inline"` // Embedded: provides Kind="HTTP01Proxy", APIVersion="operator.openshift.io/v1alpha1"

	// metadata is the standard object's metadata.
	// Contains name (must be "default"), namespace, labels, annotations, etc.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec is the specification of the desired behavior of the HTTP01Proxy.
	// This is what the user configures (input to the controller)
	// +kubebuilder:validation:Required
	// ↑ OpenAPI schema marks this field as required - API server rejects objects without it
	// +required
	// ↑ Code-generator marker: generate Required validation in code
	Spec HTTP01ProxySpec `json:"spec"`

	// status is the most recently observed status of the HTTP01Proxy.
	// This is what the controller writes back (output, read-only for users)
	// +kubebuilder:validation:Optional
	// ↑ Status is optional (not required when creating resource)
	// +optional
	// ↑ Code-generator marker: this field can be nil
	Status HTTP01ProxyStatus `json:"status,omitempty"` // omitempty: don't serialize if empty
}

// HTTP01ProxyMode controls how the HTTP01 challenge proxy is deployed.
// +kubebuilder:validation:Enum=DefaultDeployment;CustomDeployment
// ↑ OpenAPI enum validation: only these two string values are allowed
// API server rejects any other value (e.g., "InvalidMode" would be rejected)
type HTTP01ProxyMode string

const (
	// HTTP01ProxyModeDefault enables the proxy with default configuration.
	// Uses hardcoded settings (port 8888, standard DaemonSet spec)
	HTTP01ProxyModeDefault HTTP01ProxyMode = "DefaultDeployment"

	// HTTP01ProxyModeCustom enables the proxy with user-specified configuration.
	// Requires customDeployment field to be set
	HTTP01ProxyModeCustom HTTP01ProxyMode = "CustomDeployment"
)

// HTTP01ProxySpec is the specification of the desired behavior of the HTTP01Proxy.
// +kubebuilder:validation:XValidation:rule="self.mode == 'CustomDeployment' ? has(self.customDeployment) : !has(self.customDeployment)",message="customDeployment is required when mode is CustomDeployment and forbidden otherwise"
// ↑ CEL validation rule: Cross-field validation enforcing logical consistency
// Reads as: IF mode is CustomDeployment THEN customDeployment must exist ELSE customDeployment must not exist
// Prevents invalid states like mode=DefaultDeployment with customDeployment set (would be ignored/confusing)
type HTTP01ProxySpec struct {
	// mode controls whether the HTTP01 challenge proxy is active and how it should be deployed.
	// DefaultDeployment enables the proxy with default configuration.
	// CustomDeployment enables the proxy with user-specified configuration.
	// +kubebuilder:validation:Required
	// ↑ This field is mandatory
	// +required
	Mode HTTP01ProxyMode `json:"mode"` // Enum: "DefaultDeployment" or "CustomDeployment"

	// customDeployment contains configuration options when mode is CustomDeployment.
	// This field is only valid when mode is CustomDeployment.
	// +kubebuilder:validation:Optional
	// ↑ Field is optional (can be nil)
	// +optional
	CustomDeployment *HTTP01ProxyCustomDeploymentSpec `json:"customDeployment,omitempty"` // Pointer: nil when not used
}

// HTTP01ProxyCustomDeploymentSpec contains configuration for custom proxy deployment.
// Currently only supports customizing the internal port - future expansion point
type HTTP01ProxyCustomDeploymentSpec struct {
	// internalPort specifies the internal port used by the proxy service.
	// Valid values are 1024-65535.
	// +kubebuilder:validation:Minimum=1024
	// ↑ OpenAPI validation: value must be >= 1024 (unprivileged ports)
	// +kubebuilder:validation:Maximum=65535
	// ↑ OpenAPI validation: value must be <= 65535 (max TCP port number)
	// +kubebuilder:default=8888
	// ↑ If user doesn't specify this field, default to 8888
	// +optional
	InternalPort int32 `json:"internalPort,omitempty"` // int32: matches K8s port type
}

// HTTP01ProxyStatus is the most recently observed status of the HTTP01Proxy.
// Written by controller, read-only for users (managed via status subresource)
type HTTP01ProxyStatus struct {
	// conditions holds information about the current state of the HTTP01 proxy deployment.
	// Contains Ready, Degraded, Available conditions with reasons and messages
	ConditionalStatus `json:",inline,omitempty"` // Embedded: pulls in Conditions []metav1.Condition field

	// proxyImage is the name of the image and the tag used for deploying the proxy.
	// Informational: tells users which image version is deployed (e.g., "quay.io/org/proxy:v1.2.3")
	ProxyImage string `json:"proxyImage,omitempty"` // omitempty: don't serialize empty string
}

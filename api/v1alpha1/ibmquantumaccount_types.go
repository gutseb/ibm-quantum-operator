/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IBMQuantumAccountSpec defines the desired state of IBMQuantumAccount.
//
// Field design ported from the sketch at qiskit-openshift-ai/operator/config/crd/future/
// quantum.example.com_ibmquantumaccounts.yaml (this project's own more-developed CRD design,
// predating this Go scaffold) -- see MODERNIZATION-PLAN.md for the full rationale.
type IBMQuantumAccountSpec struct {
	// Suspend stops the controller from taking any reconcile action (Secret sync, validation) beyond
	// reporting a Suspended status condition. Set this while manually troubleshooting a notebook or
	// pipeline so the controller doesn't race a hand-edit -- RHOAI's own workflow routinely involves a
	// human taking manual control before pushing a fix back to automation, and this operator must not
	// fight that. Mirrors CronJob.spec.suspend's semantics deliberately.
	// See MODERNIZATION-PLAN.md, "Design requirement: must not fight manual troubleshooting".
	// +kubebuilder:default=false
	Suspend bool `json:"suspend,omitempty"`

	// Channel selects how this account connects. "local" (the default) needs no token or instance at
	// all and runs entirely against local simulators -- see qiskit-openshift-ai/automation/ansible/
	// roles/quantum_user_secret's already-implemented "local by default" logic, which this field's
	// default mirrors. Only "ibm_quantum_platform" and "ibm_cloud" require Instance/TokenSecretRef.
	// +kubebuilder:validation:Enum=local;ibm_quantum_platform;ibm_cloud
	// +kubebuilder:default=local
	Channel string `json:"channel,omitempty"`

	// Instance is required only when Channel != "local". Enforced at reconcile time (a status
	// condition, not a webhook or OpenAPI schema rule) -- a plain CRD schema can't express "required
	// unless channel == local"; see the ported sketch's own comment on this same limitation.
	// +optional
	Instance string `json:"instance,omitempty"`

	// TokenSecretRef points at a Secret (key "token") holding the IBM Quantum API token. Required only
	// when Channel != "local". The token itself must never appear in this spec, only a reference to it.
	// +optional
	TokenSecretRef *corev1.LocalObjectReference `json:"tokenSecretRef,omitempty"`

	// BackendPolicy controls backend selection when Channel != "local".
	// +optional
	BackendPolicy BackendPolicy `json:"backendPolicy,omitempty"`
}

// BackendPolicy controls how a real backend is chosen when this account isn't local-only.
type BackendPolicy struct {
	// +kubebuilder:validation:Enum=simulatorOnly;leastBusy
	// +kubebuilder:default=simulatorOnly
	Mode string `json:"mode,omitempty"`

	// +kubebuilder:validation:Minimum=1
	MinQubits int `json:"minQubits,omitempty"`

	OperationalOnly bool `json:"operationalOnly,omitempty"`
}

// IBMQuantumAccountStatus defines the observed state of IBMQuantumAccount.
type IBMQuantumAccountStatus struct {
	// Conditions report what's actually true, including a Suspended condition distinguishing "user
	// paused this" from other states, and a SecretNotFound reason for a missing/hand-edited Secret --
	// neither is ever treated as a reconcile error requiring backoff. See MODERNIZATION-PLAN.md.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Channel",type="string",JSONPath=".spec.channel"
// +kubebuilder:printcolumn:name="Suspended",type="boolean",JSONPath=".spec.suspend"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status"

// IBMQuantumAccount is the Schema for the ibmquantumaccounts API.
type IBMQuantumAccount struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IBMQuantumAccountSpec   `json:"spec,omitempty"`
	Status IBMQuantumAccountStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// IBMQuantumAccountList contains a list of IBMQuantumAccount.
type IBMQuantumAccountList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IBMQuantumAccount `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IBMQuantumAccount{}, &IBMQuantumAccountList{})
}

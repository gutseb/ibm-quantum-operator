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

package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	quantumv1alpha1 "github.com/redhat-ai-services/ibm-quantum-operator/api/v1alpha1"
)

// IBMQuantumAccountReconciler reconciles a IBMQuantumAccount object
type IBMQuantumAccountReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	conditionTypeSuspended = "Suspended"
	conditionTypeReady     = "Ready"
)

// +kubebuilder:rbac:groups=quantum.example.com,resources=ibmquantumaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=quantum.example.com,resources=ibmquantumaccounts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=quantum.example.com,resources=ibmquantumaccounts/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile implements Phase 1 of MODERNIZATION-PLAN.md: pattern only, no real job submission or
// backend scheduling yet (that's Phase 2). Two rules from the plan's "Design requirement: must not
// fight manual troubleshooting" section are load-bearing here, not incidental:
//
//  1. spec.Suspend is checked first, before anything else. When true, this function does nothing but
//     report a Suspended status condition and return -- no Secret reads, no further validation. This
//     lets a human take manual control of a notebook/pipeline without the controller racing them.
//  2. A missing or hand-edited downstream Secret is reported in .status (reason "SecretNotFound"), it
//     is never returned as a Go error. Returning an error here would make controller-runtime requeue
//     with exponential backoff as if this were a transient fault -- but a human deleting/replacing that
//     Secret mid-troubleshooting is an expected state in this project's workflow, not an exceptional
//     one. Only genuinely unexpected API errors (permissions, connectivity) are returned as errors, so
//     only those get backoff-and-retry treatment.
func (r *IBMQuantumAccountReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var account quantumv1alpha1.IBMQuantumAccount
	if err := r.Get(ctx, req.NamespacedName, &account); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Rule 1: suspend check first, before any other reconcile action.
	if account.Spec.Suspend {
		meta.SetStatusCondition(&account.Status.Conditions, metav1.Condition{
			Type:    conditionTypeSuspended,
			Status:  metav1.ConditionTrue,
			Reason:  "SpecSuspended",
			Message: "spec.suspend is true; the controller is not taking any reconcile action. Set it back to false to resume.",
		})
		if err := r.Status().Update(ctx, &account); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	meta.SetStatusCondition(&account.Status.Conditions, metav1.Condition{
		Type:    conditionTypeSuspended,
		Status:  metav1.ConditionFalse,
		Reason:  "NotSuspended",
		Message: "spec.suspend is false; the controller is reconciling normally.",
	})

	channel := account.Spec.Channel
	if channel == "" {
		// Defensive: the CRD's own +kubebuilder:default=local should mean the apiserver already filled
		// this in, but don't assume every caller (e.g. envtest, or an object built directly in Go
		// without going through the apiserver) went through that path.
		channel = "local"
	}

	if channel == "local" {
		meta.SetStatusCondition(&account.Status.Conditions, metav1.Condition{
			Type:    conditionTypeReady,
			Status:  metav1.ConditionTrue,
			Reason:  "LocalChannel",
			Message: "channel is local; no token or Secret is required.",
		})
		if err := r.Status().Update(ctx, &account); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// channel != "local": a real account needs Instance and TokenSecretRef. Mirrors the existing
	// Ansible role's assert task (qiskit-openshift-ai/automation/ansible/roles/quantum_user_secret/
	// tasks/main.yml). A missing required field is a spec problem, not a transient fault -- report it
	// and stop; there's nothing to retry until the user edits the spec, which itself triggers a new
	// reconcile.
	if account.Spec.Instance == "" || account.Spec.TokenSecretRef == nil {
		meta.SetStatusCondition(&account.Status.Conditions, metav1.Condition{
			Type:    conditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  "MissingRequiredFields",
			Message: "channel is not local, so spec.instance and spec.tokenSecretRef are both required.",
		})
		if err := r.Status().Update(ctx, &account); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	var secret corev1.Secret
	secretKey := types.NamespacedName{Namespace: account.Namespace, Name: account.Spec.TokenSecretRef.Name}
	if err := r.Get(ctx, secretKey, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			// Rule 2: expected, not exceptional -- report status, do not error/backoff.
			log.Info("token Secret not found, reporting status without requeue", "secret", secretKey)
			meta.SetStatusCondition(&account.Status.Conditions, metav1.Condition{
				Type:    conditionTypeReady,
				Status:  metav1.ConditionFalse,
				Reason:  "SecretNotFound",
				Message: "spec.tokenSecretRef does not resolve to an existing Secret.",
			})
			if statusErr := r.Status().Update(ctx, &account); statusErr != nil {
				return ctrl.Result{}, statusErr
			}
			return ctrl.Result{}, nil
		}
		// Anything other than NotFound (permissions, connectivity) is a genuine operational error --
		// let controller-runtime requeue with backoff.
		return ctrl.Result{}, err
	}

	// Phase 2 (not yet implemented): validate the token against the real IBM Quantum service, resolve
	// a backend per spec.backendPolicy. For now, presence of the Secret is enough to report Ready.
	meta.SetStatusCondition(&account.Status.Conditions, metav1.Condition{
		Type:    conditionTypeReady,
		Status:  metav1.ConditionTrue,
		Reason:  "SecretFound",
		Message: "token Secret found. Token validation and backend resolution are Phase 2 work, not yet implemented.",
	})
	if err := r.Status().Update(ctx, &account); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *IBMQuantumAccountReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&quantumv1alpha1.IBMQuantumAccount{}).
		Named("ibmquantumaccount").
		Complete(r)
}

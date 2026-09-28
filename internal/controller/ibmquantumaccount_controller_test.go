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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	quantumv1alpha1 "github.com/redhat-ai-services/ibm-quantum-operator/api/v1alpha1"
)

// These tests exist specifically to verify the two load-bearing rules from MODERNIZATION-PLAN.md's
// "Design requirement: must not fight manual troubleshooting" section -- not just that the generic
// scaffolded smoke test passes. Each test creates a real object against envtest's real (ephemeral)
// apiserver, reconciles it for real, and checks the resulting status -- not a mock.
var _ = Describe("IBMQuantumAccount Controller", func() {
	ctx := context.Background()
	var reconciler *IBMQuantumAccountReconciler

	BeforeEach(func() {
		reconciler = &IBMQuantumAccountReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
	})

	reconcileNamed := func(name string) reconcile.Result {
		result, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: name, Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())
		return result
	}

	fetch := func(name string) *quantumv1alpha1.IBMQuantumAccount {
		var acct quantumv1alpha1.IBMQuantumAccount
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &acct)).To(Succeed())
		return &acct
	}

	It("defaults to channel=local and reaches Ready with no Secret required", func() {
		name := "acct-default-local"
		acct := &quantumv1alpha1.IBMQuantumAccount{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		}
		Expect(k8sClient.Create(ctx, acct)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, acct) })

		reconcileNamed(name)

		got := fetch(name)
		ready := meta.FindStatusCondition(got.Status.Conditions, conditionTypeReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		Expect(ready.Reason).To(Equal("LocalChannel"))
	})

	It("honors spec.suspend and takes no other reconcile action (must-not-fight-manual-troubleshooting rule)", func() {
		name := "acct-suspended"
		acct := &quantumv1alpha1.IBMQuantumAccount{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: quantumv1alpha1.IBMQuantumAccountSpec{
				Suspend: true,
				Channel: "ibm_quantum_platform", // deliberately invalid/incomplete otherwise --
				// if suspend didn't short-circuit first, this would produce a MissingRequiredFields
				// Ready=False condition instead. Asserting Ready is untouched proves the suspend check
				// really does run first and does nothing else, not just that it happens to also set
				// Suspended=True alongside the rest of the logic.
			},
		}
		Expect(k8sClient.Create(ctx, acct)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, acct) })

		reconcileNamed(name)

		got := fetch(name)
		suspended := meta.FindStatusCondition(got.Status.Conditions, conditionTypeSuspended)
		Expect(suspended).NotTo(BeNil())
		Expect(suspended.Status).To(Equal(metav1.ConditionTrue))
		Expect(suspended.Reason).To(Equal("SpecSuspended"))

		ready := meta.FindStatusCondition(got.Status.Conditions, conditionTypeReady)
		Expect(ready).To(BeNil(), "Ready should never be set while suspended -- reconcile must return before reaching that logic")
	})

	It("reports MissingRequiredFields (not an error) for a real channel with no instance/tokenSecretRef", func() {
		name := "acct-missing-fields"
		acct := &quantumv1alpha1.IBMQuantumAccount{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       quantumv1alpha1.IBMQuantumAccountSpec{Channel: "ibm_quantum_platform"},
		}
		Expect(k8sClient.Create(ctx, acct)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, acct) })

		reconcileNamed(name) // reconcileNamed itself already asserts err is nil

		ready := meta.FindStatusCondition(fetch(name).Status.Conditions, conditionTypeReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal("MissingRequiredFields"))
	})

	It("reports SecretNotFound (not an error, no requeue-with-backoff) when tokenSecretRef doesn't resolve", func() {
		name := "acct-missing-secret"
		acct := &quantumv1alpha1.IBMQuantumAccount{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: quantumv1alpha1.IBMQuantumAccountSpec{
				Channel:        "ibm_quantum_platform",
				Instance:       "crn:v1:bluemix:public:quantum-computing:us-east:a/fake:fake:instance:fake",
				TokenSecretRef: &corev1.LocalObjectReference{Name: "does-not-exist"},
			},
		}
		Expect(k8sClient.Create(ctx, acct)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, acct) })

		reconcileNamed(name) // asserts err is nil -- the whole point of this test

		ready := meta.FindStatusCondition(fetch(name).Status.Conditions, conditionTypeReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal("SecretNotFound"))
	})

	It("reaches Ready once the referenced Secret actually exists", func() {
		name := "acct-with-secret"
		secretName := fmt.Sprintf("%s-token", name)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: "default"},
			StringData: map[string]string{"token": "fake-token-value"},
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, secret) })

		acct := &quantumv1alpha1.IBMQuantumAccount{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: quantumv1alpha1.IBMQuantumAccountSpec{
				Channel:        "ibm_quantum_platform",
				Instance:       "crn:v1:bluemix:public:quantum-computing:us-east:a/fake:fake:instance:fake",
				TokenSecretRef: &corev1.LocalObjectReference{Name: secretName},
			},
		}
		Expect(k8sClient.Create(ctx, acct)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, acct) })

		reconcileNamed(name)

		ready := meta.FindStatusCondition(fetch(name).Status.Conditions, conditionTypeReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		Expect(ready.Reason).To(Equal("SecretFound"))
	})
})

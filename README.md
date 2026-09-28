# ibm-quantum-operator — what this repository actually contains

**Read this before anything else.** This repository is a full, non-shallow clone of
`https://github.com/redhat-et/ibm-quantum-operator` (verified directly against the live GitHub repo, not
assumed from the local clone alone). Its own `git log --all` shows exactly 3 commits, the last from
2020-09-09, authored by `husky-parul <parsingh@redhat.com>` ("IBM quantum operator for running qiskit
based circuits on IBM quantum systems"), with a maintainer contact of `Ismael Faro <ismael.faro1@ibm.com>`
(IBM Quantum) listed in the CSV.

## What is here

```
ibm-quantum-operator.package.yaml                              # OLM "package manifest" (legacy format)
0.1.0/
  ibm-quantum-operator.v0.1.0.clusterserviceversion.yaml        # the OLM ClusterServiceVersion (CSV)
  singhp11.io_ibmqs_crd.yaml                                    # the CustomResourceDefinition
```

## What is **not** here, confirmed, not assumed

There is **no operator source code in this repository** — no Go source, no Ansible roles/playbooks, no
Helm chart, no Dockerfile, no Makefile, no `PROJECT` file, no `deploy/` or `config/` directory. The
repository's very first commit (`cd77be5`, "Initial commit") added only `LICENSE` and a 1-line
`README.md`; the second commit (`eaf2920`) **deleted both of those** and replaced them with the three
files listed above. That's the entire history. This was independently confirmed against the live GitHub
repository (its root listing shows the same two entries: `0.1.0/` and
`ibm-quantum-operator.package.yaml`) — this is not an artifact of an incomplete local clone.

**What this means practically:** there is no existing implementation to read through for "syntax and
pattern changes" in the way that phrase normally implies (there's no code whose syntax could be
outdated). What *can* be assessed, and is below, is the **packaging format and metadata itself** — which
is genuinely outdated in several concrete, checkable ways — and the **intended design**, as described in
the CSV's own text, which is useful context for a from-scratch rebuild even though no implementation of
that design survives.

## What the CSV tells us about the intended design

From the CSV's `spec.description` (verbatim intent, not paraphrased beyond trimming):

- "Flexible REST endpoints for running quantum workloads in a Kubernetes cluster" — circuits are
  submitted via a REST API, executed as Kubernetes Pods orchestrated by the operator.
- "The operator authenticated the caller and mount[ed] the equivalent secrets on the quantum workloads
  pods." — per-caller secret mounting, i.e. multi-tenant credential handling was an intended feature.
- "The default scheduler picks the least busy backend tha[t] satisfies number of qubits requested for
  the workload." — automatic backend selection by qubit count + availability (conceptually close to
  `QiskitRuntimeService.least_busy(...)`, which our own tutorial-set work has direct hands-on experience
  with — see `qiskit-openshift-ai/RHOAIAWS1/ISSUES.md` #9).
- "High Availability via Horizontal Pod Autoscaling" — the server component was meant to run as multiple
  replicas behind an HPA.
- Targets "IBM Quantum Experience" (the product's pre-2021 name; now IBM Quantum Platform /
  `qiskit-ibm-runtime`'s `ibm_quantum_platform` channel).

None of this is implemented in what's committed here — it's the stated goal, not a description of working
code.

## The CRD as published

`0.1.0/singhp11.io_ibmqs_crd.yaml`:

- `apiVersion: apiextensions.k8s.io/v1beta1` — **this API was removed in Kubernetes 1.22** (deprecated
  since 1.16). A cluster running anything remotely current (including every OpenShift/RHOAI version this
  project has touched — see `qiskit-openshift-ai/RHOAIAWS1/ISSUES.md` #0: OpenShift 4.18) cannot install
  this CRD as written; the API group simply doesn't exist anymore.
- `spec.group: singhp11.io` — a personal domain (presumably the original author's), not
  `ibm.com`/`quantum.example.com`/anything org-owned. Not suitable to keep as-is for a maintained fork.
- `kind: Ibmq` / `plural: ibmqs` — both `spec` and `status` are declared as bare `type: object` with
  **no properties at all**. The schema is a completely empty placeholder; no field of the intended
  design (REST endpoint config, backend/qubit selection, secret references, HPA settings) was ever
  encoded into the API.

## The CSV's install strategy, as published

- OLM **install strategy is `deployment`**, a single `Deployment` (`replicas: 1` — despite the
  description's own claim of "High Availability via Horizontal Pod Autoscaling"; no `HorizontalPodAutoscaler`
  resource is defined anywhere in this bundle).
- Container image: `quay.io/husky_parul/ibm-q:v0.1.0`. **Still pullable** (confirmed via `skopeo
  inspect`, 2026-09-28) — built 2020-09-21 on `ubi8-minimal`, `DockerVersion: "1.13.1"`. The container's
  own command is a custom binary named `ibm-quantum-operator` (not `ansible-operator` or `helm-operator`,
  which are the fixed entrypoint binary names for those two operator-sdk plugin kinds) — this is
  consistent with a **Go-based** operator-sdk scaffold, not Ansible or Helm. (This clue is why the
  modernization plan below proceeds Go-first — see `MODERNIZATION-PLAN.md`.) The image itself was not
  further inspected beyond its metadata/labels — decompiling a 5+ year old opaque binary to recover
  design intent is not a productive use of time versus building fresh from the CSV's stated intent and
  our own project's already-more-developed CRD design (see below).
- Env vars injected — `WATCH_NAMESPACE` (from `olm.targetNamespaces` annotation), `POD_NAME`,
  `OPERATOR_NAME` — this exact trio is the fingerprint of **pre-v1.0.0 operator-sdk** (roughly
  2018–2019-era SDK, before the Kubebuilder-style layout was adopted in SDK `1.0.0`, released
  2020-08-26). SDK `1.0.0`+ scaffolds no longer use `OPERATOR_NAME`/`POD_NAME` at all (leader election
  is handled by `controller-runtime`'s own `LeaderElectionID` instead) — see
  `MODERNIZATION-PLAN.md`'s syntax-diff section, sourced directly from the operator-sdk project's own
  migration guide.
- RBAC is hand-listed directly in the CSV's `install.spec.permissions` (routes, pods, services,
  deployments/daemonsets/replicasets/statefulsets, `servicemonitors`, and `singhp11.io/*` — a wildcard
  grant on its own CRD group). Modern scaffolds generate this file from `+kubebuilder:rbac:` markers on
  the reconciler source via `make manifests` — another artifact of this being pre-Kubebuilder-integration
  vintage.
- Package format is the legacy **"package manifests"** layout (`packageName`/`channels`/`currentCSV` in
  a bare `.package.yaml`, one CSV per version directory) — OLM's current format is the **Bundle** format
  (`bundle.Dockerfile`, `metadata/annotations.yaml`, `manifests/`), which package manifests were
  deprecated in favor of years ago. Our own project's existing `operator/bundle/` already uses the
  current Bundle format, for reference.

## How this compares to what `qiskit-openshift-ai/operator/` already has

Useful context: this project's own existing operator work (`qiskit-openshift-ai/operator/`, a separate,
unrelated-to-this-repo Helm-based operator-sdk scaffold reconciling a `QiskitPlatform` CR, plus a
CRD *sketch* for a related `IBMQuantumAccount` kind) is **already meaningfully more developed** than
what's in this repository:

- Already on `apiextensions.k8s.io/v1` (not `v1beta1`).
- Already uses the current Bundle format for OLM (`operator/bundle/`, not package manifests).
- Already has a real (if empty-of-implementation) `IBMQuantumAccount` schema with `channel`
  (`local`/`ibm_quantum_platform`/`ibm_cloud`), `tokenSecretRef`, `instance`, and `backendPolicy.mode`
  fields sketched out — see
  `qiskit-openshift-ai/operator/config/crd/future/quantum.example.com_ibmquantumaccounts.yaml` — versus
  this repo's completely empty `spec`/`status`.

This repo's CSV description is still useful as an independent, differently-worded statement of the same
underlying problem (run Qiskit circuits against IBM Quantum from a cluster, least-busy backend selection,
per-caller credentials) — worth keeping as a cross-check that our own CRD design covers the same ground,
not as source to port.

## See also

`MODERNIZATION-PLAN.md` in this same branch (`modernization-plan`) for the actual plan and runbook —
Go-based operator-sdk pattern first (per this repo's own apparent original architecture and current
default SDK guidance), Ansible and Helm as secondary/educational passes afterward.

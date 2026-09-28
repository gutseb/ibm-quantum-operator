# Modernization plan and runbook — ibm-quantum-operator

**Status: planning only, nothing scaffolded yet.** Read `README.md` first — this repo has no operator
source code to migrate, only a 2020 OLM package-manifest bundle. This plan is therefore "build fresh,
informed by the old bundle's stated intent and our own project's already-more-developed CRD sketch," not
"port existing code."

## Decision record

- **Pattern: Go-based operator-sdk scaffold, first.** Originally planned Ansible-first (per initial
  instruction), reversed after finding that the original 2020 bundle's CSV runs a custom-named binary
  (`ibm-quantum-operator`, not the fixed `ansible-operator`/`helm-operator` entrypoint names those two
  SDK plugin kinds always use) — strong evidence the original was Go-based. Continuing in Go is the
  simpler path given that, per the user's own call. Ansible and Helm are now **secondary, educational
  passes**, done after the Go pattern is working, not dropped.
- **Domain/group: reuse `quantum.example.com`, not `singhp11.io` or a new one.** The old CRD's
  `singhp11.io` group was a personal domain, not suitable to keep. `qiskit-openshift-ai/operator/`
  already has a real, more-developed `quantum.example.com/v1alpha1` `IBMQuantumAccount` CRD *sketch*
  (channel/tokenSecretRef/instance/backendPolicy fields already designed — see `README.md`'s comparison
  section). Building a second, competing CRD group here would fragment the design across two repos for
  no benefit. **Recommendation, not yet applied:** this repo scaffolds the Go implementation of that
  *same* `IBMQuantumAccount` kind (not a new kind), and the end goal is to port the working Go
  implementation back into `qiskit-openshift-ai/operator/`, replacing its current Helm-operator
  scaffold, rather than maintaining two separate operators long-term. Confirm this before scaffolding —
  it's a real decision, flagged here rather than assumed silently.

## What changed, concretely, since the original 2020 bundle (sourced from `operator-sdk`'s own docs, this
machine's local clone, `v1.42.3`, checked current 2026-09-28)

| Old (2020, this repo) | Current (SDK 1.0.0+, confirmed v1.42.3) |
|---|---|
| `apiextensions.k8s.io/v1beta1` CRDs | `apiextensions.k8s.io/v1` (v1beta1 removed in k8s 1.22) |
| OLM "package manifests" (`*.package.yaml` + per-version dir) | OLM **Bundle** format (`bundle.Dockerfile` + `metadata/annotations.yaml` + `manifests/`) |
| `deploy/` layout (`deploy/crds`, `deploy/operator.yaml`, `deploy/role.yaml`) | `config/` kustomize layout (`config/crd/bases`, `config/manager`, `config/rbac`, `config/samples`) |
| `pkg/apis/<group>/<version>`, `pkg/controller/<kind>` | `api/<version>/<kind>_types.go`, `controllers/<kind>_controller.go` (root of repo) |
| RBAC hand-listed in the CSV's `install.spec.permissions` | Generated from `+kubebuilder:rbac:` markers on the reconciler via `make manifests` |
| `WATCH_NAMESPACE` / `POD_NAME` / `OPERATOR_NAME` env vars, `leader.Become()` | `controller-runtime`'s own `LeaderElectionID` in `ctrl.Options{}`; those three env vars are unused by current scaffolds |
| `operator-sdk generate crds` / `operator-sdk build` | `make manifests` / `make docker-build` |
| Metrics on `:8383`, `operator-sdk generate openapi` | Metrics on `:8080` (kube-auth-proxy fronted); `+kubebuilder:object:root=true` markers instead of openapi-gen |
| No `PROJECT` file | `PROJECT` file records domain/repo/GVKs/plugin chain (`go.kubebuilder.io/v4`), read by every `operator-sdk`/`make` command |

Sourced from: `operator-sdk/website/content/en/docs/building-operators/golang/migration.md` and
`quickstart.md` in the locally-cloned `operator-sdk` repo (branch tracking `v1.42.3`, last commit
2026-07-16 — checked current, not assumed).

## Toolchain prerequisites (from `operator-sdk`'s own installation guide)

- `go` 1.22+
- `docker` 17.03+ (or podman with the docker CLI shim)
- `kubectl` matching a compatible cluster version
- `operator-sdk` CLI — install fresh; do not reuse whatever produced the 2020 bundle
- Access to an image registry for the operator image + bundle image (we already have
  `quay.io/rhn_gps_segutier/quantum` set up and working — see the `qiskit-openshift-ai-quay-registry`
  memory note — reuse that account/repo rather than provisioning a new one)

## Runbook — scaffolding (not yet executed)

```sh
# From this repo's root, on the modernization-plan branch
operator-sdk init --domain example.com --repo github.com/redhat-et/ibm-quantum-operator
# ^ --domain and --repo are placeholders pending the domain decision above; if we proceed with
#   quantum.example.com (recommended), use that here instead, and confirm --repo against wherever
#   this ends up actually being pushed (this local repo's origin is still the read-only upstream
#   redhat-et/ibm-quantum-operator.git -- a real push destination needs deciding first).

operator-sdk create api \
  --group quantum \
  --version v1alpha1 \
  --kind IBMQuantumAccount \
  --resource --controller
# Mirrors qiskit-openshift-ai/operator's existing IBMQuantumAccount design (group `quantum`, matching
# quantum.example.com) rather than inventing new naming.

make manifests generate
```

At this point `api/v1alpha1/ibmquantumaccount_types.go` exists with empty `Spec`/`Status` structs to
fill in, and `controllers/ibmquantumaccount_controller.go` exists with an empty `Reconcile()`.

## Design requirement: must not fight manual troubleshooting (added 2026-09-28, per explicit instruction)

RHOAI/OpenShift AI's own workflow deliberately supports both automated pipelines (Tekton/Data Science
Pipelines) *and* a human going into a Workbench to manually troubleshoot or modify a notebook before
pushing a fix back into the automated path — the bootcamp material the user took covers exactly this
mode. Any CRD this operator reconciles that touches pipeline/job/notebook state must be designed so a
human taking manual control never produces a reconcile error, a fight-with-the-controller loop, or a
`Requeue` storm. This needs to be baked into the reconciler shape now, in Phase 1, not bolted on later —
it changes the fundamental control-flow of every `Reconcile()` function this project writes.

**The established Kubernetes pattern for this, already used by resources we work with directly:**
`CronJob.spec.suspend` and Argo Rollouts' `spec.paused` both give a human an explicit, declarative way to
say "stop actively managing this, I'm taking over" without deleting anything or erroring — the controller
checks the flag first thing in `Reconcile()` and returns cleanly if set, doing nothing further that run.
RHOAI's own `Notebook` CR has its own version of this same idea: stopping a Workbench from the dashboard
does not delete the `Notebook` CR, it scales its pod to zero via an annotation-driven mechanism (verified
directly on our own live RHOAIAWS1 cluster that the CR is never deleted on stop, though the exact
annotation key was not re-derived this session — not worth restating from memory without re-verifying;
grep the installed `odh-notebook-controller` deployment/CSV directly when this matters for real).

**Concrete design decisions for this project's CRDs:**

1. **`IBMQuantumAccount.spec.suspend`** (`bool`, default `false`). When `true`, the reconciler performs
   no Secret sync, no token/instance validation, no status-affecting side effects — it only refreshes a
   `Suspended` status condition and returns `ctrl.Result{}, nil`. This matters even though Phase 1's
   account CR barely does anything actively yet: it establishes the pattern before Phase 2 adds real
   validation calls that could otherwise race with someone hand-editing a Secret mid-troubleshooting.
2. **Any future `QuantumFunction`/`QuantumJob`-shaped CRD must have the same `spec.suspend` field**, and
   it matters far more there: a scheduled function re-run or a git-source redeploy triggering while
   someone has manually `oc exec`'d into the running Pod/Workbench to fix something would silently
   clobber their in-progress work. `spec.suspend: true` must stop the controller from touching the
   downstream Pod/Job/Notebook at all, not just stop new triggers.
3. **General reconciler rule, not just a suspend check:** any downstream resource the controller doesn't
   currently manage the existence of (because a human created, stopped, or hand-edited it) must be
   treated as an observable fact to report in `.status`, never as an error condition that forces
   `Requeue` with backoff. A missing/modified/stopped downstream resource is business-as-usual in this
   project's workflow, not a fault.
4. **Status conditions should say what's actually true**, including `Suspended: True` with a `reason`
   distinguishing "user paused this" from other states — so `kubectl get ibmquantumaccount` (or whatever
   future kind) is itself useful for a human deciding whether it's safe to resume automation, without
   needing to check for a stray annotation elsewhere.

This is a Phase 1 scaffolding requirement (the field and the early-return check go in from the start),
not deferred feature work.

## Runbook — porting the existing CRD design into Go types

Port field-for-field from `qiskit-openshift-ai/operator/config/crd/future/
quantum.example.com_ibmquantumaccounts.yaml` (the existing sketch) into
`api/v1alpha1/ibmquantumaccount_types.go`:

```go
type IBMQuantumAccountSpec struct {
    // Suspend stops the controller from taking any reconcile action (Secret sync, validation) beyond
    // reporting a Suspended status condition -- set this while manually troubleshooting so the
    // controller doesn't race a hand-edit. See "Design requirement: must not fight manual
    // troubleshooting" above. Mirrors CronJob.spec.suspend's semantics deliberately.
    // +kubebuilder:default=false
    Suspend bool `json:"suspend,omitempty"`

    // +kubebuilder:validation:Enum=local;ibm_quantum_platform;ibm_cloud
    // +kubebuilder:default=local
    Channel string `json:"channel,omitempty"`

    // Required only when Channel != "local" -- enforce in the reconciler (webhook or reconcile-time
    // status condition), not the OpenAPI schema; see the existing sketch's own comment on why a plain
    // schema can't express "required unless channel == local".
    Instance string `json:"instance,omitempty"`

    TokenSecretRef *corev1.LocalObjectReference `json:"tokenSecretRef,omitempty"`

    BackendPolicy BackendPolicy `json:"backendPolicy,omitempty"`
}

type BackendPolicy struct {
    // +kubebuilder:validation:Enum=simulatorOnly;leastBusy
    // +kubebuilder:default=simulatorOnly
    Mode string `json:"mode,omitempty"`
    MinQubits int `json:"minQubits,omitempty"`
    OperationalOnly bool `json:"operationalOnly,omitempty"`
}

type IBMQuantumAccountStatus struct {
    // Conditions, resolved backend, last-verified-at, etc. -- design during Phase 2 (feature work),
    // not part of this pattern-modernization pass.
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

This is a starting sketch for this planning pass, not final — refine once scaffolding actually starts.

## Runbook — reconciler skeleton, RBAC markers

```go
//+kubebuilder:rbac:groups=quantum.example.com,resources=ibmquantumaccounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=quantum.example.com,resources=ibmquantumaccounts/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
```

Reconcile-loop shape for Phase 1 (pattern only — actual local-sim/real-job logic is Phase 2, per the
user's own stated priority: "first i want to get it converted to the latest pattern in the SDK then we
can plan features next"):

1. Fetch the `IBMQuantumAccount` CR.
2. **Check `spec.suspend` first, before anything else.** If `true`: set a `Suspended` status condition
   (`reason: "SpecSuspended"`) and return `ctrl.Result{}, nil` immediately — no Secret sync, no
   validation, nothing further this reconcile. See "Design requirement: must not fight manual
   troubleshooting" above; this check must be the first thing in the function body, not interleaved
   with the rest of the logic.
3. If `spec.channel == "local"`: set a `Ready` status condition, no Secret/token validation needed
   (mirrors the Ansible role's already-implemented `_using_real_account` branch in
   `qiskit-openshift-ai/automation/ansible/roles/quantum_user_secret/tasks/main.yml` — same logic,
   promoted from Ansible into the operator).
4. If `spec.channel != "local"`: require `spec.instance` and `spec.tokenSecretRef` are set (mirrors the
   existing Ansible `assert` task); fetch the referenced Secret; set status accordingly. **If the
   referenced Secret is missing or was hand-edited to something the controller didn't write, that is
   never treated as a reconcile error requiring backoff** — reflect it in `.status` (e.g. `reason:
   "SecretNotFound"`) and return `ctrl.Result{}, nil`, not a non-nil error, since a human mid-
   troubleshooting deleting/editing that Secret is an expected, not exceptional, state in this project's
   workflow (see the design requirement above).
5. Real job-submission / backend-scheduling logic (the old CSV's "least busy backend" feature) is
   explicitly deferred to Phase 2 per the user's stated sequencing.

Run `make manifests` after any type/marker change to regenerate `config/crd/bases/` and
`config/rbac/role.yaml`.

## Runbook — build, deploy, verify (once code exists)

```sh
make docker-build docker-push IMG=quay.io/rhn_gps_segutier/quantum:ibm-quantum-operator-dev
make install run          # local run against a real cluster's API, fastest iteration loop
# then, once stable:
make deploy IMG=quay.io/rhn_gps_segutier/quantum:ibm-quantum-operator-dev
kubectl apply -f config/samples/quantum_v1alpha1_ibmquantumaccount.yaml
```

Bundle/OLM packaging (replacing the old package-manifests format) once the direct-deploy path works:

```sh
make bundle IMG=quay.io/rhn_gps_segutier/quantum:ibm-quantum-operator-dev
make bundle-build bundle-push BUNDLE_IMG=quay.io/rhn_gps_segutier/quantum:ibm-quantum-operator-bundle-dev
```

## Verification standard (per this project's own established practice)

Consistent with every other build in this project's history (`qiskit-openshift-ai/RHOAIAWS1/ISSUES.md`
#6's lesson): a green `make deploy`/`Succeeded` status is not sufficient proof by itself. Verify directly:
`kubectl get ibmquantumaccount -o yaml` shows the expected status condition, `kubectl logs
deployment/<name>-controller-manager -c manager` shows real reconcile activity (not just "started"), and
a `channel: local` CR actually reaches `Ready` without any Secret existing.

## Phasing

**Phase 0 (this document): plan only.** No code scaffolded yet — waiting for the domain/group decision
above to be confirmed before running `operator-sdk init`.

**Phase 1 — Go pattern modernization** (current priority, per explicit instruction: "first i want to get
it converted to the latest pattern in the SDK then we can plan features next"):
- Scaffold via `operator-sdk init` + `create api` as above.
- Port the existing `IBMQuantumAccount` CRD field design into Go types.
- Reconciler skeleton only: local-vs-real-account branching (mirroring the already-working Ansible role
  logic), no real job submission yet.
- `apiextensions.k8s.io/v1` CRD, Bundle-format OLM packaging, `config/` kustomize layout throughout —
  closes every gap in the "what changed" table above.
- Verify with the standard above, on a real cluster.

**Phase 2 — feature work (after Phase 1 is verified working, not before):**
- Real IBM Quantum job submission support (the capability this whole modernization effort is ultimately
  for, per `qiskit-openshift-ai`'s standing goal).
- Least-busy backend scheduling (the old CSV's stated design; now something to actually implement).
- Per-caller Secret handling / multi-tenancy, if still wanted (also from the old CSV's stated design).

**Explicitly excluded from Phase 2 (decided 2026-09-28, see `qiskit-openshift-ai/artifacts-local/
FINDINGS.md`'s local-simulation-tunables design decision):** fake-backend selection, noise-model
construction, Aer simulation `method`/`seed`/parallelism tuning. These have nothing for a controller to
reconcile (no Secret, no external resource, no drift) and belong in a code-layer helper module
(`quantum_local`, planned for `container-images/profiles/*/`) plus an optional plain `ConfigMap` for
tenant-wide defaults — not on `IBMQuantumAccount` or any CRD. Only reconsider a CRD for these
(a `QuantumJob`-shaped one, distinct from `QuantumFunction`) if the operator ever grows to orchestrate
job execution directly (spawning a Pod per submitted circuit) — not part of this plan's current scope.
- Only after Phase 2 is stable: begin `qiskit-ai-accelerator-alignment` memory's Phase 1 (reconciling
  `qiskit-openshift-ai/RHOAIAWS1`'s manifests into the `tenants/quantum-example/` GitOps scaffold) — that
  work is explicitly gated behind this operator modernization, per standing instruction.

**Phase 3 — Ansible pattern (secondary, educational)**, once Go is working: scaffold a parallel
`operator-sdk init --plugins ansible` project implementing the *same* `IBMQuantumAccount` CR, to compare
approaches directly. Sourced from `operator-sdk/website/content/en/docs/building-operators/ansible/
quickstart.md` (already reviewed): `operator-sdk init --domain quantum.example.com --plugins ansible`,
`operator-sdk create api --group quantum --version v1alpha1 --kind IBMQuantumAccount --generate-role`,
then implement the same local/real-account branching as an Ansible role instead of Go — this is largely
a re-expression of the Ansible role logic that already exists in
`qiskit-openshift-ai/automation/ansible/roles/quantum_user_secret/`, so most of the hard thinking is
already done; this phase is mostly a translation exercise.

**Phase 4 — Helm pattern (secondary, educational)**, same idea, using `operator-sdk init --plugins helm`
against the existing `qiskit-openshift-ai/operator/helm-charts/qiskit-platform` chart as a worked example
of what's already been built this way, for comparison purposes.

## Open questions to confirm before Phase 1 scaffolding starts

1. **Domain/group confirmation** — proceed with `quantum.example.com` / kind `IBMQuantumAccount`,
   consolidating with `qiskit-openshift-ai/operator/`'s existing sketch? (Recommended above; not yet
   confirmed.)
2. **Push destination** — this local repo's `origin` remote is still the read-only upstream
   `redhat-et/ibm-quantum-operator.git`. Needs a real destination (a fork, or a new repo, or folding
   directly into `qiskit-openshift-ai/operator/` as a branch there instead of a separate repo) before
   any commit here can be pushed.
3. **Relationship to `qiskit-openshift-ai/operator/`** — build here and port back later (this plan's
   working assumption), or should this repo directly replace that directory's contents via a subtree
   merge/copy once Phase 1 is verified? Either works; deciding now avoids redoing the integration step
   twice.

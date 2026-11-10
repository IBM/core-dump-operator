# Validation record — 2026-11-09

Base revision: `a19cc8ebbb3001ec85dcfd0f32edfb4e7b0870ca`.
Changes are organized into three local commits; no PR, merge, image publication,
release, or GitHub issue closure was performed.

## Scope and implementation

- [Issue #14](https://github.com/IBM/core-dump-operator/issues/14): update
  `golang.org/x/net` from v0.33.0 to v0.62.0, above all three reported fix thresholds
  (v0.36.0, v0.38.0, v0.55.0). Set explicit `contents: read` permissions in both
  Actions workflows, covering all five jobs. Update their Go and action versions,
  correct image-build path filters, and align operator-sdk versions.
- [Issue #9](https://github.com/IBM/core-dump-operator/issues/9): use the upstream
  `quay.io/brancz/kube-rbac-proxy:v0.23.1` image. Remove unsupported logging flags.
  The registry manifest includes linux/amd64, arm/v7, arm64, ppc64le and s390x.
- SCC lifecycle: derive a cluster-unique name from the source namespace/name/UID;
  require matching management markers before any update/delete; use atomic Create,
  resource-version checked Update and UID/resource-version checked Delete; remove
  cross-scope owner references and group grants. Unrelated legacy SCCs remain
  untouched. Restrict privileged CR configuration to the trusted operator namespace,
  validate the host directory, and require explicit all-node opt-in. Protect foreign
  DaemonSets from adoption/deletion too.

## Environment

Go 1.27.3 was downloaded from go.dev, verified using its published SHA-256, and
extracted into `/home/agent/go`. `GOPATH=/home/agent/go-work` keeps module downloads
separate from GOROOT. The application module requires Go 1.26.0; CI and container
builds use 1.27.3. Tool versions: controller-gen v0.23.0, setup-envtest v0.24.1,
kube-apiserver/etcd assets 1.28.3, Kustomize v3.8.7, operator-sdk v1.31.0,
actionlint v1.7.12 and govulncheck v1.9.0.

## Results

| Check | Result |
| --- | --- |
| `make operator-test` | Passed; 79.6% controller statement coverage |
| `make uploader-test` | Passed; 63.6% uploader statement coverage |
| `KUBEBUILDER_ASSETS=... go test ./... -count=1 -json` | Passed; 15/15 envtest specs, both additional controller unit tests (including six validation subcases), 10 uploader tests passed; five external-fixture tests skipped |
| `make build` and `CGO_ENABLED=0 GOOS=linux go build` for manager/uploader | Passed; both production binaries are statically linked ELF amd64 executables |
| `go vet ./...`, `go mod verify`, `git diff --check` | Passed |
| `actionlint` | Passed for both workflows |
| `kustomize build config/default` | Passed; includes namespace downward API, new proxy and CRD validations |
| `make bundle` with operator-sdk v1.31.0, in an isolated copy | Bundle generation and `operator-sdk bundle validate ./bundle` passed; OwnNamespace mode and namespace environment preserved |
| `govulncheck ./...` | Zero reachable vulnerabilities and zero vulnerable imported packages; two module-only AWS S3 Crypto SDK advisories remain, outside the imported packages, with no fixed version reported |
| Manager startup with neither namespace flag nor environment | Exits before attempting cluster access |

The API tests run real local kube-apiserver and etcd processes, with an SCC CRD
standing in for the OpenShift SCC resource. They cover creation/update/deletion,
idempotency, manual resource deletion, operator restart, disabling OpenShift mode,
foreign policies and DaemonSets, changed ownership, retained SCCs after CR recreation,
namespace isolation with a running manager, invalid host paths, all-node opt-in,
name derivation and concurrent create/update/delete conflicts. No fake apply-upsert
shim is needed.

## Remaining verification and deployment work

No OpenShift cluster is available. Native SCC admission, scheduling/running the
privileged agent pods, and an actual OLM installation remain untested. Local bundle
validation does not establish that a live OLM install succeeds. Docker API access
is denied in this environment, so Docker image builds and container execution were
not run. The five skipped uploader tests require a live Kubernetes fixture or S3
credentials; neither was available.

The security fix changes the CR trust boundary and placement configuration. Follow
[the migration procedure](security-and-upgrade.md) before deployment. In particular,
review old SCCs manually, move CR configuration to the trusted operator namespace,
and update host paths and node targeting. Public issue alerts still need the fixes
to be merged and the repository's hosted scans to rerun; no scan closure is inferred
from local validation.

## Severity assessment — follow-up on 2026-11-09

The bare-name SCC update/deletion is a valid ownership and scope defect. A High
rating is reasonable when a non-administrator can create a matching CR (or has
other sufficient access to a colliding CR) while the manager has SCC privileges.
The optional editor role is not installed by the default RBAC kustomization, so
this is not an unauthenticated or default-permission attack. Multi-tenant dump
collection alone does not establish that tenants should control privileged agent
configuration. In an administrator-only installation, the additional privilege
escalation impact is substantially reduced, though accidental modification of an
unrelated cluster policy remains a defect.

The submitted fake-client proof and our SCC-CRD envtest coverage do not establish
native OpenShift Pod admission or worker-node compromise. Treat that end-to-end
impact as unverified. Kubernetes documents that a cluster-scoped dependent cannot
have a namespaced owner, so the old owner reference does not protect ownership.
Red Hat also documents that deleted default SCCs are regenerated by the Cluster
Version Operator; permanent cluster-wide admission failure should not be inferred
from deletion alone.

Sources: [Kubernetes owner references](https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/),
[Red Hat SCC management](https://docs.redhat.com/en/documentation/openshift_container_platform/4.13/html/authentication_and_authorization/managing-pod-security-policies).

## Commit organization — 2026-11-10

- `0f7f2be`: Issue #9, metrics proxy image and compatible arguments.
- `b4ff473`: Issue #14, dependency/workflow fixes and their build-tool updates.
- The following SCC commit contains ownership, trusted-namespace and spec-validation
  changes, regression tests, migration documentation, and removal of the now-unused
  OpenShift apply-client dependency.

Issue #14 was also tested in isolation from the SCC changes: manifests and deepcopy
code were regenerated, then `make operator-test uploader-test` passed with the
existing controller suite (78.4% controller coverage; 63.6% uploader coverage).
The final implementation files match the previously tested working tree; splitting
only changes commit grouping and this bookkeeping record.

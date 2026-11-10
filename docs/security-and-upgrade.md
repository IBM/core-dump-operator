# Security model and upgrade notes

CoreDumpHandler is an administrator configuration resource. It creates privileged
host agents, selects their images and service account, and configures host dump
collection. Permission to write these resources is equivalent to permission to
configure privileged node workloads. Bind the optional editor role only to trusted
cluster administrators in the operator's namespace. Workload tenants configure
upload secrets in their own namespaces; they do not need CoreDumpHandler access.

The manager requires one trusted namespace, supplied by `--namespace` or
`POD_NAMESPACE`. The deployment uses its own namespace through the downward API.
The cache and reconciler both restrict namespaced resources to that namespace.
Keep this namespace and the manager deployment inaccessible to tenants. Namespace
restriction does not remove the cluster permissions required to manage SCCs.

`hostDir` must be `/mnt/core-dump-handler` or a subdirectory with ordinary path
components. Host administrators must keep this tree free of symlinks to other host
paths. Configure a nonempty `nodeSelector` to select collection nodes, or explicitly
set `allowAllNodes: true` to deploy on all eligible nodes. Affinity and tolerations
still affect scheduling. Existing resources with other host paths or implicit
all-node targeting must be updated before normal reconciliation resumes. The
controller skips spec validation during deletion, but API admission may still
require normalizing an old spec before its finalizer can be removed.

## SCC lifecycle

The operator creates a dedicated cluster-scoped SCC with a deterministic name
`core-dump-operator-<SHA-256(namespace, name, UID)>`. Including the CR UID isolates
resources recreated with the same name. The SCC records the source namespace,
name and UID in annotations and the manager name in a label. Every update and
delete verifies all four markers. A pre-existing object without matching markers
is never adopted. The SCC has no namespaced owner reference, since that reference
cannot own a cluster-scoped resource.

SCC creation uses Create, updates use the checked resource version, and deletions
use both UID and resource-version preconditions. Conflicts are retried by normal
reconciliation. The SCC grants access only to the configured service account
(`default` when omitted), has no group grants, and permits the hostPath and projected
volumes needed by the agent pods. Disabling `openShift` removes the dedicated SCC.
The operator also refuses to adopt or delete a DaemonSet owned by another object.

## Upgrading an existing installation

1. Review and revoke tenant CoreDumpHandler write bindings. Restrict write access
   to the operator namespace and its deployment to trusted cluster administrators.
2. Inventory existing CRs, DaemonSets and SCCs. Review any old SCC named after a CR,
   especially built-in OpenShift policies. Older versions could have changed a
   policy they did not create. Restore built-in policies from your supported
   OpenShift configuration through your cluster's normal recovery procedure.
3. Pause the old manager and retire its agent pods during the migration window.
   Inventory configuration that needs to move into the operator namespace. Do not
   run old and new agents together on the same hosts. Upgrade the CRD, then update
   retained CRs to an allowed `hostDir` and a nonempty node selector or explicit
   `allowAllNodes: true`. The old CRD does not recognize `allowAllNodes`. On clusters
   without validation ratcheting, normalize legacy specs even when deleting them,
   since the API may reject finalizer updates against an invalid spec. For instances
   outside the new managed namespace, an administrator must retire old DaemonSets
   and remove old CR finalizers after reviewing their SCCs.
4. Deploy the matching RBAC and manager after completing the CRD and configuration
   migration. Confirm creation of the dedicated SCC and replacement pods. Review
   service-account SCC grants carefully: privileged pods can use that account's
   permissions.
5. Manually review and remove obsolete legacy SCCs only after confirming they were
   created exclusively for retired agents and are unused. New versions deliberately
   leave every old bare-name SCC untouched, including ones bearing old owner
   references. Those references cannot prove the SCC was originally operator-created.

This changes the OLM install mode to OwnNamespace. Per-namespace upload-secret
collection remains available across workload namespaces; CR configuration is
restricted to the trusted operator namespace.

## Local verification

Use Go 1.27.3 (the container and CI use the same compiler), with a separate GOPATH
if Go itself is installed at `/home/agent/go`:

```sh
export PATH=/home/agent/go/bin:$PATH
export GOPATH=/home/agent/go-work
make operator-test
make uploader-test
make build
```

`make operator-test` downloads pinned envtest assets and starts local etcd and
kube-apiserver processes. No existing Kubernetes cluster is needed. These tests
exercise real API creation, server-side apply for DaemonSets, CRD validation,
resource-version conflicts and delete preconditions. SCCs use the test CRD in
`hack/`; this does not implement OpenShift SCC admission. Running and admitting the
privileged pods and validating the OLM installation still require a real OpenShift
cluster. S3 and uploader live-cluster tests skip when their external fixtures are
unavailable.

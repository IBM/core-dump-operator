/*
 * Copyright 2026 IBM Inc. All rights reserved
 * SPDX-License-Identifier: Apache-2.0
 */

package controllers

import (
	"context"
	"crypto/sha256"
	"fmt"

	chartsv1alpha1 "github.com/IBM/core-dump-operator/api/v1alpha1"
	"github.com/go-logr/logr"
	securityv1 "github.com/openshift/api/security/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	sccNamespaceAnnotation = "charts.ibm.com/source-namespace"
	sccNameAnnotation      = "charts.ibm.com/source-name"
	sccUIDAnnotation       = "charts.ibm.com/source-uid"
	sccManagedByLabel      = "app.kubernetes.io/managed-by"
)

// SCCs are cluster scoped: neither a bare CR name nor a namespaced owner reference
// identifies their owner. Include the CR UID to isolate deleted/recreated CRs too.
func sccName(cdu *chartsv1alpha1.CoreDumpHandler) string {
	identity := cdu.Namespace + "\x00" + cdu.Name + "\x00" + string(cdu.UID)
	return fmt.Sprintf("core-dump-operator-%x", sha256.Sum256([]byte(identity)))
}

func ownsSCC(scc *securityv1.SecurityContextConstraints, cdu *chartsv1alpha1.CoreDumpHandler) bool {
	return cdu.UID != "" && scc.Labels[sccManagedByLabel] == fieldManager &&
		scc.Annotations[sccNamespaceAnnotation] == cdu.Namespace &&
		scc.Annotations[sccNameAnnotation] == cdu.Name &&
		scc.Annotations[sccUIDAnnotation] == string(cdu.UID)
}

func (r *CoreDumpHandlerReconciler) UpdateScc(ctx context.Context, cdu *chartsv1alpha1.CoreDumpHandler, l logr.Logger) (bool, error) {
	if !cdu.Spec.OpenShift {
		return r.DeleteScc(ctx, cdu, l)
	}
	if cdu.UID == "" {
		return false, fmt.Errorf("cannot manage SCC for a CoreDumpHandler without a UID")
	}
	var existing securityv1.SecurityContextConstraints
	err := r.Get(ctx, client.ObjectKey{Name: sccName(cdu)}, &existing)
	if err != nil && !errors.IsNotFound(err) {
		return false, err
	}
	create := errors.IsNotFound(err)
	if !create && !ownsSCC(&existing, cdu) {
		return false, fmt.Errorf("refusing to modify SCC %q: ownership does not match CoreDumpHandler %s/%s", existing.Name, cdu.Namespace, cdu.Name)
	}
	metadata := *existing.ObjectMeta.DeepCopy()
	metadata.Name = sccName(cdu)
	// A cluster-scoped SCC cannot have a namespaced owner reference. The finalizer
	// deletes it using the explicit identity annotations instead.
	metadata.OwnerReferences = nil
	if metadata.Annotations == nil {
		metadata.Annotations = make(map[string]string)
	}
	metadata.Annotations[sccNamespaceAnnotation] = cdu.Namespace
	metadata.Annotations[sccNameAnnotation] = cdu.Name
	metadata.Annotations[sccUIDAnnotation] = string(cdu.UID)
	if metadata.Labels == nil {
		metadata.Labels = make(map[string]string)
	}
	metadata.Labels[sccManagedByLabel] = fieldManager
	serviceAccount := cdu.Spec.ServiceAccount
	if serviceAccount == "" {
		serviceAccount = "default"
	}
	desired := &securityv1.SecurityContextConstraints{
		TypeMeta:                        existing.TypeMeta,
		ObjectMeta:                      metadata,
		AllowHostDirVolumePlugin:        true,
		AllowPrivilegedContainer:        true,
		AllowPrivilegeEscalation:        pointer.Bool(true),
		DefaultAllowPrivilegeEscalation: pointer.Bool(true),
		ForbiddenSysctls:                []string{"*"},
		FSGroup:                         securityv1.FSGroupStrategyOptions{Type: securityv1.FSGroupStrategyRunAsAny},
		RunAsUser:                       securityv1.RunAsUserStrategyOptions{Type: securityv1.RunAsUserStrategyRunAsAny},
		SELinuxContext:                  securityv1.SELinuxContextStrategyOptions{Type: securityv1.SELinuxStrategyRunAsAny},
		SupplementalGroups:              securityv1.SupplementalGroupsStrategyOptions{Type: securityv1.SupplementalGroupsStrategyRunAsAny},
		Volumes:                         []securityv1.FSType{securityv1.FSTypeHostPath, securityv1.FSTypeSecret, securityv1.FSTypePersistentVolumeClaim, securityv1.FSProjected},
		Priority:                        pointer.Int32(10),
		Users:                           []string{fmt.Sprintf("system:serviceaccount:%s:%s", cdu.Namespace, serviceAccount)},
		// Do not retain group bindings or other security fields added by others.
	}
	if create {
		// Create (not apply-upsert) prevents adopting an SCC created after the Get.
		return false, r.Create(ctx, desired)
	}
	if equality.Semantic.DeepEqual(&existing, desired) {
		return false, nil
	}
	// ResourceVersion from the verified object prevents a concurrent replacement
	// or ownership change from being overwritten. No forced SSA is used for SCCs.
	return false, r.Update(ctx, desired)
}

func (r *CoreDumpHandlerReconciler) DeleteScc(ctx context.Context, cdu *chartsv1alpha1.CoreDumpHandler, l logr.Logger) (bool, error) {
	var found securityv1.SecurityContextConstraints
	err := r.Get(ctx, client.ObjectKey{Name: sccName(cdu)}, &found)
	if errors.IsNotFound(err) || (!cdu.Spec.OpenShift && meta.IsNoMatchError(err)) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !ownsSCC(&found, cdu) {
		l.Info("Leaving SCC with different ownership untouched", "name", found.Name)
		return false, nil
	}
	// Check both identity and version at deletion, not just at the preceding Get.
	err = r.Delete(ctx, &found, &client.DeleteOptions{Preconditions: &metav1.Preconditions{
		UID: &found.UID, ResourceVersion: &found.ResourceVersion,
	}})
	return false, client.IgnoreNotFound(err)
}

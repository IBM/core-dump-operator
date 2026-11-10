/*
 * Copyright 2026 IBM Inc. All rights reserved
 * SPDX-License-Identifier: Apache-2.0
 */

package controllers

import (
	"context"
	"strings"
	"time"

	chartsv1alpha1 "github.com/IBM/core-dump-operator/api/v1alpha1"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	securityv1 "github.com/openshift/api/security/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("CoreDumpHandler controller", func() {
	var ctx context.Context
	var r *CoreDumpHandlerReconciler
	var cr *chartsv1alpha1.CoreDumpHandler
	var key client.ObjectKey
	var sccKey client.ObjectKey

	BeforeEach(func() {
		ctx = context.Background()
		r = &CoreDumpHandlerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), ManagedNamespace: namespaceName}
		cr = &chartsv1alpha1.CoreDumpHandler{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "handler-", Namespace: namespaceName},
			Spec: chartsv1alpha1.CoreDumpHandlerSpec{
				OpenShift: true, ServiceAccount: "uploader", NodeSelector: map[string]string{"core-dump-handler": "enabled"},
			},
		}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		key = client.ObjectKeyFromObject(cr)
		sccKey = client.ObjectKey{Name: sccName(cr)}
		DeferCleanup(func() {
			// envtest has no garbage collector. Clean fixtures, including finalizers,
			// explicitly so tests remain independent even after an assertion failure.
			_ = k8sClient.Delete(ctx, &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}})
			_ = k8sClient.Delete(ctx, &securityv1.SecurityContextConstraints{ObjectMeta: metav1.ObjectMeta{Name: sccKey.Name}})
			current := &chartsv1alpha1.CoreDumpHandler{}
			if k8sClient.Get(ctx, key, current) == nil {
				current.Finalizers = nil
				Expect(k8sClient.Update(ctx, current)).To(Succeed())
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, current))).To(Succeed())
			}
		})
	})

	reconcile := func() error {
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		return err
	}
	getSCC := func() *securityv1.SecurityContextConstraints {
		found := &securityv1.SecurityContextConstraints{}
		Expect(k8sClient.Get(ctx, sccKey, found)).To(Succeed())
		return found
	}
	getDS := func() *appsv1.DaemonSet {
		found := &appsv1.DaemonSet{}
		Expect(k8sClient.Get(ctx, key, found)).To(Succeed())
		return found
	}

	It("creates, updates and deletes only its dedicated resources", func() {
		Expect(reconcile()).To(Succeed())
		scc := getSCC()
		Expect(ownsSCC(scc, cr)).To(BeTrue())
		Expect(scc.OwnerReferences).To(BeEmpty())
		Expect(scc.Users).To(Equal([]string{"system:serviceaccount:" + namespaceName + ":uploader"}))
		Expect(scc.Groups).To(BeEmpty())
		Expect(scc.Volumes).To(ContainElements(securityv1.FSTypeHostPath, securityv1.FSProjected))
		ds := getDS()
		Expect(metav1.IsControlledBy(ds, cr)).To(BeTrue())
		Expect(ds.Spec.Template.Spec.NodeSelector).To(Equal(cr.Spec.NodeSelector))
		Expect(ds.Spec.Template.Spec.Volumes[0].HostPath.Path).To(Equal("/mnt/core-dump-handler"))
		Expect(reconcile()).To(Succeed())
		Expect(getSCC().ResourceVersion).To(Equal(scc.ResourceVersion), "unchanged SCC must not be written again")
		Expect(k8sClient.Get(ctx, key, cr)).To(Succeed())
		cr.Spec.ServiceAccount = "replacement"
		cr.Spec.HandlerImage = "example.com/handler:v2"
		Expect(k8sClient.Update(ctx, cr)).To(Succeed())
		Expect(reconcile()).To(Succeed())
		Expect(getSCC().Users).To(Equal([]string{"system:serviceaccount:" + namespaceName + ":replacement"}))
		Expect(getDS().Spec.Template.Spec.Containers[0].Image).To(Equal("example.com/handler:v2"))
		Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
		Expect(reconcile()).To(Succeed())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, sccKey, &securityv1.SecurityContextConstraints{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, key, &appsv1.DaemonSet{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, key, &chartsv1alpha1.CoreDumpHandler{}))).To(BeTrue())
	})

	It("recreates removed resources and restores edited security fields", func() {
		Expect(reconcile()).To(Succeed())
		scc := getSCC()
		scc.Groups = []string{"system:authenticated"}
		scc.Users = append(scc.Users, "unrelated-user")
		scc.AllowHostNetwork = true
		Expect(k8sClient.Update(ctx, scc)).To(Succeed())
		Expect(reconcile()).To(Succeed())
		Expect(getSCC().Groups).To(BeEmpty())
		Expect(getSCC().Users).To(HaveLen(1))
		Expect(getSCC().AllowHostNetwork).To(BeFalse())
		Expect(k8sClient.Delete(ctx, getSCC())).To(Succeed())
		Expect(k8sClient.Delete(ctx, getDS())).To(Succeed())
		Expect(reconcile()).To(Succeed())
		Expect(ownsSCC(getSCC(), cr)).To(BeTrue())
		Expect(metav1.IsControlledBy(getDS(), cr)).To(BeTrue())
	})

	It("finalizes after an operator restart when resources are already absent", func() {
		Expect(reconcile()).To(Succeed())
		Expect(k8sClient.Delete(ctx, getSCC())).To(Succeed())
		Expect(k8sClient.Delete(ctx, getDS())).To(Succeed())
		Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
		r = &CoreDumpHandlerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), ManagedNamespace: namespaceName}
		Expect(reconcile()).To(Succeed())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, key, &chartsv1alpha1.CoreDumpHandler{}))).To(BeTrue())
		Expect(reconcile()).To(Succeed())
	})

	It("removes its SCC when OpenShift support is disabled and still finalizes", func() {
		Expect(reconcile()).To(Succeed())
		Expect(k8sClient.Get(ctx, key, cr)).To(Succeed())
		cr.Spec.OpenShift = false
		Expect(k8sClient.Update(ctx, cr)).To(Succeed())
		Expect(reconcile()).To(Succeed())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, sccKey, &securityv1.SecurityContextConstraints{}))).To(BeTrue())
		Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
		Expect(reconcile()).To(Succeed())
	})

	It("never changes or deletes an existing SCC with the same bare CR name", func() {
		legacy := &securityv1.SecurityContextConstraints{ObjectMeta: metav1.ObjectMeta{Name: cr.Name}, Groups: []string{"system:authenticated"}}
		Expect(k8sClient.Create(ctx, legacy)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, legacy)).To(Succeed()) })
		// Include the invalid legacy owner reference: it must not authorize adoption.
		legacy.OwnerReferences = []metav1.OwnerReference{{APIVersion: chartsv1alpha1.GroupVersion.String(), Kind: "CoreDumpHandler", Name: cr.Name, UID: cr.UID}}
		Expect(k8sClient.Update(ctx, legacy)).To(Succeed())
		before := legacy.DeepCopy()
		Expect(reconcile()).To(Succeed())
		Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
		Expect(reconcile()).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: legacy.Name}, legacy)).To(Succeed())
		Expect(legacy).To(Equal(before))
	})

	It("refuses to adopt a collision at the generated SCC name", func() {
		foreign := &securityv1.SecurityContextConstraints{ObjectMeta: metav1.ObjectMeta{Name: sccKey.Name}, Groups: []string{"system:authenticated"}}
		Expect(k8sClient.Create(ctx, foreign)).To(Succeed())
		Expect(reconcile()).To(MatchError(ContainSubstring("ownership does not match")))
		current := &securityv1.SecurityContextConstraints{}
		Expect(k8sClient.Get(ctx, sccKey, current)).To(Succeed())
		Expect(current).To(Equal(foreign))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, key, &appsv1.DaemonSet{}))).To(BeTrue())
		Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
		Expect(reconcile()).To(Succeed())
		Expect(k8sClient.Get(ctx, sccKey, current)).To(Succeed())
		Expect(current).To(Equal(foreign))
	})

	It("leaves SCCs with a changed source UID untouched during reconciliation and deletion", func() {
		Expect(reconcile()).To(Succeed())
		scc := getSCC()
		scc.Annotations[sccUIDAnnotation] = "another-cr"
		Expect(k8sClient.Update(ctx, scc)).To(Succeed())
		Expect(reconcile()).To(MatchError(ContainSubstring("ownership does not match")))
		Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
		Expect(reconcile()).To(Succeed())
		Expect(getSCC()).To(Equal(scc))
	})

	It("does not adopt or delete a foreign DaemonSet", func() {
		foreign := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
			Spec: appsv1.DaemonSetSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"test": "foreign"}},
				Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"test": "foreign"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "foreign", Image: "example.com/foreign:v1"}}}},
			},
		}
		Expect(k8sClient.Create(ctx, foreign)).To(Succeed())
		Expect(reconcile()).To(MatchError(ContainSubstring("ownership does not match")))
		Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
		Expect(reconcile()).To(Succeed())
		Expect(getDS()).To(Equal(foreign))
	})

	It("ignores tenant CRs even when deletion is pending", func() {
		r.ManagedNamespace = "another-trusted-namespace"
		Expect(reconcile()).To(Succeed())
		Expect(k8sClient.Get(ctx, key, cr)).To(Succeed())
		Expect(cr.Finalizers).To(BeEmpty())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, sccKey, &securityv1.SecurityContextConstraints{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, key, &appsv1.DaemonSet{}))).To(BeTrue())
		cr.Finalizers = []string{coredumpHandlerFinalizer}
		Expect(k8sClient.Update(ctx, cr)).To(Succeed())
		Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
		Expect(reconcile()).To(Succeed())
		Expect(k8sClient.Get(ctx, key, cr)).To(Succeed())
		Expect(cr.Finalizers).To(ContainElement(coredumpHandlerFinalizer))
	})

	It("runs the manager only for the trusted namespace", func() {
		tenantNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "tenant-"}}
		Expect(k8sClient.Create(ctx, tenantNS)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, tenantNS)).To(Succeed()) })
		tenantCR := &chartsv1alpha1.CoreDumpHandler{
			ObjectMeta: metav1.ObjectMeta{Name: "restricted-v2", Namespace: tenantNS.Name},
			Spec:       chartsv1alpha1.CoreDumpHandlerSpec{OpenShift: true, AllowAllNodes: true},
		}
		Expect(k8sClient.Create(ctx, tenantCR)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, tenantCR)).To(Succeed()) })
		managerCtx, cancel := context.WithCancel(ctx)
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme: k8sClient.Scheme(), Cache: cache.Options{Namespaces: []string{namespaceName}},
			MetricsBindAddress: "0", HealthProbeBindAddress: "0",
		})
		Expect(err).NotTo(HaveOccurred())
		managed := &CoreDumpHandlerReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), ManagedNamespace: namespaceName}
		Expect(managed.SetupWithManager(mgr)).To(Succeed())
		done := make(chan error, 1)
		go func() { done <- mgr.Start(managerCtx) }()
		DeferCleanup(func() {
			cancel()
			Eventually(done, 10*time.Second).Should(Receive(BeNil()))
		})
		Eventually(func() error { return k8sClient.Get(ctx, key, &appsv1.DaemonSet{}) }, 10*time.Second, 100*time.Millisecond).Should(Succeed())
		Expect(ownsSCC(getSCC(), cr)).To(BeTrue())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenantCR), tenantCR)).To(Succeed())
		Expect(tenantCR.Finalizers).To(BeEmpty())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenantCR), &appsv1.DaemonSet{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Name: sccName(tenantCR)}, &securityv1.SecurityContextConstraints{}))).To(BeTrue())
	})

	It("requires a configured trusted namespace", func() {
		r.ManagedNamespace = ""
		Expect(reconcile()).To(MatchError(ContainSubstring("managed namespace must be configured")))
	})

	It("rejects host root paths and implicit all-node targeting at admission", func() {
		for _, path := range []string{"/", "/etc", "/mnt/core-dump-handler/../etc", "/mnt/core-dump-handler/.", "/mnt/core-dump-handler-evil", "/mnt/core-dump-handler//child"} {
			candidate := cr.DeepCopy()
			candidate.Spec.HostDir = path
			Expect(apierrors.IsInvalid(k8sClient.Update(ctx, candidate))).To(BeTrue(), path)
		}
		cr.Spec.HostDir = "/mnt/core-dump-handler/tenant-a"
		cr.Spec.NodeSelector = nil
		Expect(apierrors.IsInvalid(k8sClient.Update(ctx, cr))).To(BeTrue())
		cr.Spec.AllowAllNodes = true
		Expect(k8sClient.Update(ctx, cr)).To(Succeed())
		Expect(reconcile()).To(Succeed())
		Expect(getDS().Spec.Template.Spec.NodeSelector).To(BeEmpty())
	})

	It("has collision-resistant SCC identities across namespaces, UIDs and long names", func() {
		a := cr.DeepCopy()
		b := cr.DeepCopy()
		a.Namespace, a.Name = "a-b", "c"
		b.Namespace, b.Name = "a", "b-c"
		Expect(sccName(a)).NotTo(Equal(sccName(b)))
		b = a.DeepCopy()
		b.UID = types.UID("recreated")
		Expect(sccName(a)).NotTo(Equal(sccName(b)))
		a.Name = strings.Repeat("a", 253)
		Expect(sccName(a)).To(HaveLen(83))
		scc := &securityv1.SecurityContextConstraints{}
		Expect(ownsSCC(scc, a)).To(BeFalse())
	})

	It("isolates a recreated CR from the previous CR's SCC", func() {
		Expect(reconcile()).To(Succeed())
		oldSCC := getSCC()
		oldUID := cr.UID
		// Simulate a retained SCC from a previous installation. A newly created CR
		// with the same namespace/name must not modify or delete that policy.
		Expect(k8sClient.Get(ctx, key, cr)).To(Succeed())
		cr.Finalizers = nil
		Expect(k8sClient.Update(ctx, cr)).To(Succeed())
		Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
		Expect(k8sClient.Delete(ctx, getDS())).To(Succeed())
		cr = &chartsv1alpha1.CoreDumpHandler{
			ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
			Spec:       chartsv1alpha1.CoreDumpHandlerSpec{OpenShift: true, AllowAllNodes: true},
		}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		Expect(cr.UID).NotTo(Equal(oldUID))
		newSCCKey := client.ObjectKey{Name: sccName(cr)}
		Expect(newSCCKey).NotTo(Equal(sccKey))
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &securityv1.SecurityContextConstraints{ObjectMeta: metav1.ObjectMeta{Name: newSCCKey.Name}}))).To(Succeed())
		})
		Expect(reconcile()).To(Succeed())
		newSCC := &securityv1.SecurityContextConstraints{}
		Expect(k8sClient.Get(ctx, newSCCKey, newSCC)).To(Succeed())
		Expect(newSCC.Users).To(Equal([]string{"system:serviceaccount:" + namespaceName + ":default"}))
		Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
		Expect(reconcile()).To(Succeed())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, newSCCKey, newSCC))).To(BeTrue())
		Expect(getSCC()).To(Equal(oldSCC))
	})

	It("uses create and resource-version preconditions to fail safely on concurrent changes", func() {
		// Simulate another actor creating the target between Get and Create.
		r.Client = &racingClient{Client: k8sClient, beforeCreate: func(obj client.Object) {
			if obj.GetName() == sccKey.Name {
				Expect(k8sClient.Create(ctx, &securityv1.SecurityContextConstraints{ObjectMeta: metav1.ObjectMeta{Name: sccKey.Name}})).To(Succeed())
			}
		}}
		_, err := r.UpdateScc(ctx, cr, logr.Discard())
		Expect(apierrors.IsAlreadyExists(err)).To(BeTrue())
		Expect(k8sClient.Delete(ctx, getSCC())).To(Succeed())
		r.Client = k8sClient
		Expect(reconcile()).To(Succeed())
		changeOwner := func(client.Object) {
			scc := getSCC()
			scc.Annotations[sccUIDAnnotation] = "concurrent-owner"
			Expect(k8sClient.Update(ctx, scc)).To(Succeed())
		}
		cr.Spec.ServiceAccount = "changed"
		r.Client = &racingClient{Client: k8sClient, beforeUpdate: changeOwner}
		_, err = r.UpdateScc(ctx, cr, logr.Discard())
		Expect(apierrors.IsConflict(err)).To(BeTrue())
		scc := getSCC()
		Expect(scc.Users).NotTo(ContainElement("system:serviceaccount:" + namespaceName + ":changed"))
		scc.Annotations[sccUIDAnnotation] = string(cr.UID)
		Expect(k8sClient.Update(ctx, scc)).To(Succeed())
		r.Client = &racingClient{Client: k8sClient, beforeDelete: changeOwner}
		_, err = r.DeleteScc(ctx, cr, logr.Discard())
		Expect(apierrors.IsConflict(err)).To(BeTrue())
		Expect(getSCC().Annotations[sccUIDAnnotation]).To(Equal("concurrent-owner"))
	})
})

// These hooks run against a real envtest API server; only the timing is injected.
type racingClient struct {
	client.Client
	beforeCreate func(client.Object)
	beforeUpdate func(client.Object)
	beforeDelete func(client.Object)
}

func (c *racingClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if c.beforeCreate != nil {
		c.beforeCreate(obj)
	}
	return c.Client.Create(ctx, obj, opts...)
}
func (c *racingClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if c.beforeUpdate != nil {
		c.beforeUpdate(obj)
	}
	return c.Client.Update(ctx, obj, opts...)
}
func (c *racingClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if c.beforeDelete != nil {
		c.beforeDelete(obj)
	}
	return c.Client.Delete(ctx, obj, opts...)
}

package controllers

import (
	"context"
	"time"

	chartsv1alpha1 "github.com/IBM/core-dump-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	securityv1 "github.com/openshift/api/security/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func createCoreDumpHandler(ctx context.Context, cdhName string, namespaceName string, openShift bool) error {
	typedNamespaceName := types.NamespacedName{Name: cdhName, Namespace: namespaceName}
	err := k8sClient.Get(ctx, typedNamespaceName, &chartsv1alpha1.CoreDumpHandler{})
	if err != nil && errors.IsNotFound(err) {
		// Let's mock our custom resource at the same way that we would
		// apply on the cluster the manifest under config/samples
		cdh := &chartsv1alpha1.CoreDumpHandler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      typedNamespaceName.Name,
				Namespace: typedNamespaceName.Namespace,
			},
			Spec: chartsv1alpha1.CoreDumpHandlerSpec{
				OpenShift:              openShift,
				ImagePullSecret:        "secret",
				ServiceAccount:         "sa",
				NodeSelector:           map[string]string{"node": "selector"},
				Tolerations:            []corev1.Toleration{{Key: "key", Operator: "Exists", Effect: "NoSchedule"}},
				Affinity:               &chartsv1alpha1.AffinityApplyConfiguration{},
				NamespaceLabelSelector: map[string]string{"cdh": "enabled"},
				Resource: &corev1.ResourceRequirements{
					Limits:   corev1.ResourceList{"cpu": *resource.NewQuantity(1, resource.DecimalSI)},
					Requests: corev1.ResourceList{"cpu": *resource.NewQuantity(1, resource.DecimalSI)},
				},
			},
		}

		err = k8sClient.Create(ctx, cdh)
	}
	return err
}

func deleteCoreDumpHandler(ctx context.Context, cdhName string, namespaceName string) error {
	typedNamespaceName := types.NamespacedName{Name: cdhName, Namespace: namespaceName}
	err := k8sClient.Get(ctx, typedNamespaceName, &chartsv1alpha1.CoreDumpHandler{})
	if err == nil {
		cdh := &chartsv1alpha1.CoreDumpHandler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      typedNamespaceName.Name,
				Namespace: typedNamespaceName.Namespace,
			},
		}
		err = k8sClient.Delete(ctx, cdh)
	}
	return err
}

func testCreateDeleteCoreDumpHandler(cdhName string, namespaceName string, openShift bool) {
	It("should successfully reconcile creating and deleting a custom resource for CoreDumpHandler", func() {
		By("Creating the custom resource for the Kind CoreDumpHandler")
		ctx := context.Background()
		typedNamespaceName := types.NamespacedName{Name: cdhName, Namespace: namespaceName}
		err := createCoreDumpHandler(ctx, cdhName, namespaceName, openShift)
		Expect(err).To(Not(HaveOccurred()))

		By("Checking if the custom resource was successfully created")
		Eventually(func() error {
			found := &chartsv1alpha1.CoreDumpHandler{}
			return k8sClient.Get(ctx, typedNamespaceName, found)
		}, time.Minute, time.Second).Should(Succeed())

		By("Reconciling the custom resource created")
		cdhReconciler := &CoreDumpHandlerReconciler{
			Client: k8sClient, Scheme: k8sClient.Scheme(),
		}
		_, err = cdhReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typedNamespaceName,
		})
		Expect(err).To(Not(HaveOccurred()))

		By("Checking if DaemonSet was successfully created in the reconciliation")
		Eventually(func() error {
			found := &appsv1.DaemonSet{}
			return k8sClient.Get(ctx, typedNamespaceName, found)
		}, time.Minute, time.Second).Should(Succeed())

		if openShift {
			By("Checking if SCC was successfully created in the reconciliation")
			Eventually(func() error {
				found := &securityv1.SecurityContextConstraints{}
				return k8sClient.Get(ctx, typedNamespaceName, found)
			}, time.Minute, time.Second).Should(Succeed())
		}

		By("Removing the custom ressource for the Kind CoreDumpHandler")
		Eventually(func() error {
			return deleteCoreDumpHandler(ctx, cdhName, namespaceName)
		}, time.Minute, time.Second).Should(Succeed())

		By("Reconciling the custom resource deleted")
		_, err = cdhReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typedNamespaceName,
		})
		Expect(err).To(Not(HaveOccurred()))

		By("Checking if DaemonSet was successfully deleted in the reconciliation")
		Eventually(func() error {
			found := &appsv1.DaemonSet{}
			return k8sClient.Get(ctx, typedNamespaceName, found)
		}, time.Minute, time.Second).ShouldNot(Succeed())

		By("Checking if SCC was successfully deleted in the reconciliation")
		Eventually(func() error {
			found := &securityv1.SecurityContextConstraints{}
			return k8sClient.Get(ctx, typedNamespaceName, found)
		}, time.Minute, time.Second).ShouldNot(Succeed())
	})
}

func testDeleteAfterOperatorRestart() {
	It("should successfully reconcile deleting a custom resource for CoreDumpHandler at operator restart", func() {
		By("Creating the custom resource for the Kind CoreDumpHandler")
		ctx := context.Background()
		typedNamespaceName := types.NamespacedName{Name: cdhName, Namespace: namespaceName}
		err := createCoreDumpHandler(ctx, cdhName, namespaceName, true)
		Expect(err).To(Not(HaveOccurred()))

		By("Reconciling the custom resource created")
		cdhReconciler := &CoreDumpHandlerReconciler{
			Client: k8sClient, Scheme: k8sClient.Scheme(),
		}
		_, err = cdhReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typedNamespaceName,
		})
		Expect(err).To(Not(HaveOccurred()))

		By("Reconciling the custom resource created second time")
		_, err = cdhReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typedNamespaceName,
		})
		Expect(err).To(Not(HaveOccurred()))

		By("Removing the Daemonset manually")
		Eventually(func() error {
			return k8sClient.Delete(ctx, &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: cdhName, Namespace: namespaceName}})
		}, time.Minute, time.Second).Should(Succeed())

		By("Removing the SCC manually")
		Eventually(func() error {
			return k8sClient.Delete(ctx, &securityv1.SecurityContextConstraints{ObjectMeta: metav1.ObjectMeta{Name: cdhName, Namespace: namespaceName}})
		}, time.Minute, time.Second).Should(Succeed())

		By("Removing the custom ressource for the Kind CoreDumpHandler")
		Eventually(func() error {
			return deleteCoreDumpHandler(ctx, cdhName, namespaceName)
		}, time.Minute, time.Second).Should(Succeed())

		By("Reconciling the custom resource deleted")
		_, err = cdhReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typedNamespaceName,
		})
		Expect(err).To(Not(HaveOccurred()))

		By("Checking if DaemonSet was successfully deleted in the reconciliation")
		Eventually(func() error {
			found := &appsv1.DaemonSet{}
			return k8sClient.Get(ctx, typedNamespaceName, found)
		}, time.Minute, time.Second).ShouldNot(Succeed())

		By("Checking if SCC was successfully deleted in the reconciliation")
		Eventually(func() error {
			found := &securityv1.SecurityContextConstraints{}
			return k8sClient.Get(ctx, typedNamespaceName, found)
		}, time.Minute, time.Second).ShouldNot(Succeed())
	})
}

func testCreateOnUserDelete() {
	It("should successfully reconcile creating a custom resource for CoreDumpHandler at random user deletion", func() {
		By("Creating the custom resource for the Kind CoreDumpHandler")
		ctx := context.Background()
		typedNamespaceName := types.NamespacedName{Name: cdhName, Namespace: namespaceName}
		err := createCoreDumpHandler(ctx, cdhName, namespaceName, true)
		Expect(err).To(Not(HaveOccurred()))

		By("Reconciling the custom resource created")
		cdhReconciler := &CoreDumpHandlerReconciler{
			Client: k8sClient, Scheme: k8sClient.Scheme(),
		}
		_, err = cdhReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typedNamespaceName,
		})
		Expect(err).To(Not(HaveOccurred()))

		err = k8sClient.Delete(ctx, &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: cdhName, Namespace: namespaceName}})
		Expect(err).To(Not(HaveOccurred()))

		_, err = cdhReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typedNamespaceName,
		})
		Expect(err).To(Not(HaveOccurred()))

		By("Checking if DaemonSet was successfully created in the reconciliation")
		Eventually(func() error {
			found := &appsv1.DaemonSet{}
			return k8sClient.Get(ctx, typedNamespaceName, found)
		}, time.Minute, time.Second).Should(Succeed())

		By("Checking if SCC was successfully created in the reconciliation")
		Eventually(func() error {
			found := &securityv1.SecurityContextConstraints{}
			return k8sClient.Get(ctx, typedNamespaceName, found)
		}, time.Minute, time.Second).Should(Succeed())

		err = k8sClient.Delete(ctx, &securityv1.SecurityContextConstraints{ObjectMeta: metav1.ObjectMeta{Name: cdhName, Namespace: namespaceName}})
		Expect(err).To(Not(HaveOccurred()))

		_, err = cdhReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typedNamespaceName,
		})
		Expect(err).To(Not(HaveOccurred()))

		By("Checking if DaemonSet was successfully created in the reconciliation")
		Eventually(func() error {
			found := &appsv1.DaemonSet{}
			return k8sClient.Get(ctx, typedNamespaceName, found)
		}, time.Minute, time.Second).Should(Succeed())

		By("Checking if SCC was successfully created in the reconciliation")
		Eventually(func() error {
			found := &securityv1.SecurityContextConstraints{}
			return k8sClient.Get(ctx, typedNamespaceName, found)
		}, time.Minute, time.Second).Should(Succeed())

		By("Removing the custom ressource for the Kind CoreDumpHandler")
		Eventually(func() error {
			return deleteCoreDumpHandler(ctx, cdhName, namespaceName)
		}, time.Minute, time.Second).Should(Succeed())

		By("Reconciling the custom resource deleted")
		_, err = cdhReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typedNamespaceName,
		})
		Expect(err).To(Not(HaveOccurred()))
	})
}

func testCreateOnUserModify() {
	It("should successfully reconcile creating a custom resource for CoreDumpHandler at random user modify", func() {
		ctx := context.Background()
		typedNamespaceName := types.NamespacedName{Name: cdhName, Namespace: namespaceName}

		By("Creating the Daemonset manually")
		err := k8sClient.Create(ctx, &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: cdhName, Namespace: namespaceName},
			Spec: appsv1.DaemonSetSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "core-dump-handler", "cluster": "test-cdh"}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "core-dump-handler", "cluster": "test-cdh"}},
					Spec: corev1.PodSpec{Containers: []corev1.Container{
						{Name: "test", Image: "image:latest"},
					}},
				},
			},
		})
		Expect(err).To(Not(HaveOccurred()))

		By("Creating the SCC manually")
		err = k8sClient.Create(ctx, &securityv1.SecurityContextConstraints{
			ObjectMeta: metav1.ObjectMeta{Name: cdhName, Namespace: namespaceName},
		})
		Expect(err).To(Not(HaveOccurred()))

		By("Creating the custom resource for the Kind CoreDumpHandler")
		err = createCoreDumpHandler(ctx, cdhName, namespaceName, true)
		Expect(err).To(Not(HaveOccurred()))

		By("Reconciling the custom resource created")
		cdhReconciler := &CoreDumpHandlerReconciler{
			Client: k8sClient, Scheme: k8sClient.Scheme(),
		}
		_, err = cdhReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typedNamespaceName,
		})
		Expect(err).To(Not(HaveOccurred()))

		err = k8sClient.Delete(ctx, &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: cdhName, Namespace: namespaceName}})
		Expect(err).To(Not(HaveOccurred()))

		_, err = cdhReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typedNamespaceName,
		})
		Expect(err).To(Not(HaveOccurred()))

		By("Checking if DaemonSet was successfully created in the reconciliation")
		Eventually(func() error {
			found := &appsv1.DaemonSet{}
			return k8sClient.Get(ctx, typedNamespaceName, found)
		}, time.Minute, time.Second).Should(Succeed())

		By("Checking if SCC was successfully created in the reconciliation")
		Eventually(func() error {
			found := &securityv1.SecurityContextConstraints{}
			return k8sClient.Get(ctx, typedNamespaceName, found)
		}, time.Minute, time.Second).Should(Succeed())

		By("Removing the custom ressource for the Kind CoreDumpHandler")
		Eventually(func() error {
			return deleteCoreDumpHandler(ctx, cdhName, namespaceName)
		}, time.Minute, time.Second).Should(Succeed())

		By("Reconciling the custom resource deleted")
		_, err = cdhReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typedNamespaceName,
		})
		Expect(err).To(Not(HaveOccurred()))
	})
}

func testEmptyRequest() {
	It("should successfully reconcile empty requests by ignoring them", func() {
		ctx := context.Background()
		typedNamespaceName := types.NamespacedName{Name: cdhName, Namespace: namespaceName}

		By("Reconciling the custom resource for empty (race condition?)")
		cdhReconciler := &CoreDumpHandlerReconciler{
			Client: k8sClient, Scheme: k8sClient.Scheme(),
		}

		_, err := cdhReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typedNamespaceName,
		})
		Expect(err).To(Not(HaveOccurred()))
	})
}

var _ = Describe("CoreDumpHandler controller", func() {
	Context("CoreDumpHandler controller test", func() {
		testCreateDeleteCoreDumpHandler(cdhName, namespaceName, true)
		testCreateDeleteCoreDumpHandler(cdhName, namespaceName, false)
		testCreateOnUserDelete()
		testDeleteAfterOperatorRestart()
		testCreateOnUserModify()
		testEmptyRequest()
	})
})

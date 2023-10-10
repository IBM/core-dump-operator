/*
 * Copyright 2023- IBM Inc. All rights reserved
 * SPDX-License-Identifier: Apache-2.0
 */

package controllers

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	chartsv1alpha1 "github.com/IBM/core-dump-operator/api/v1alpha1"
	securityv1 "github.com/openshift/api/security/v1"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment

const namespaceName = "core-dump-handler"
const cdhName = "test-cdh"

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	// Note: original yaml for securityv1 regards defaultAddCapabilities and requiredDropCapabilities required although they are nullable.
	// testEnv rejects those entries with null. and thus, we use a custom securityv1 CRD with fixed required items.
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "config", "crd", "bases"),
			//filepath.Join(build.Default.GOPATH, "pkg", "mod", "github.com", "openshift", "api@v0.0.0-20230816181854-a7ca92db022a", "security", "v1", "0000_03_security-openshift_01_scc.crd.yaml"),
			filepath.Join("..", "hack", "0000_03_security-openshift_01_scc.crd.yaml"),
		},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = chartsv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = securityv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	By("Creating the Namespace to perform the tests")
	err = k8sClient.Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceName}})
	Expect(err).To(Not(HaveOccurred()))
})

var _ = AfterSuite(func() {
	By("Deleting the Namespace to perform the tests")
	err := k8sClient.Delete(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceName}})
	Expect(err).To(Not(HaveOccurred()))

	By("tearing down the test environment")
	err = testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

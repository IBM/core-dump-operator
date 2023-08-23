/*
 * Copyright 2023- IBM Inc. All rights reserved
 * SPDX-License-Identifier: Apache-2.0
 */

package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const testKubeConfigPath = "/Users/tyos/.kube/core-dump-handler-test"

func GetK8sClient(t *testing.T, kubeConfigPath string, selectorString string) (K8sClient, error) {
	k8s := NewK8sClient(kubeConfigPath, selectorString)
	err := k8s.ResetClient()
	if err != nil {
		t.SkipNow()
		return nil, err
	}
	return k8s, nil
}

func TestCheckNamespace(t *testing.T) {
	k8s, err := GetK8sClient(t, testKubeConfigPath, "kubernetes.io/metadata.name=tyos")
	if err != nil {
		return
	}
	assert.Equal(t, nil, k8s.CheckNamespace("tyos"), "Failed: CheckNamespace, namespace tyos should have kubernetes.io/metadata.name=tyos")
	assert.Equal(t, nil, k8s.CheckNamespace("objcache"), "Failed: CheckNamespace, namespace objcache should not have kubernetes.io/metadata.name=tyos")
}

func TestGetSecret(t *testing.T) {
	testNamespace := "tyos"
	secretName := "core-dump-handler-test"

	k8s, err := GetK8sClient(t, testKubeConfigPath, "kubernetes.io/metadata.name=tyos")
	if err != nil {
		return
	}
	expected := CoreDumpUploaderSecret{
		Bucket: "bucket", KeyPrefix: "a/b/c", AccessKey: "ABCDEF", SecretKey: "12345", Endpoint: "https://endpoint", CreateBucket: true,
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: secretName, Namespace: testNamespace,
		},
		Type: corev1.SecretType("core-dump-handler"),
		StringData: map[string]string{
			"bucket": expected.Bucket, "keyPrefix": expected.KeyPrefix, "accessKey": expected.AccessKey,
			"secretKey": expected.SecretKey, "endpoint": expected.Endpoint,
		},
	}
	ctx, cancel := context.WithTimeout(context.TODO(), time.Second*10)
	_, err = k8s.GetRawClient().CoreV1().Secrets(testNamespace).Create(ctx, secret, metav1.CreateOptions{})
	cancel()
	if err != nil {
		t.Errorf("Failed: Secrets create, err=%v", err)
		return
	}
	defer func() {
		k8s.GetRawClient().CoreV1().Secrets(testNamespace).Delete(context.TODO(), secretName, metav1.DeleteOptions{})
	}()
	secretData, err := k8s.GetSecret(testNamespace)
	if err != nil {
		t.Errorf("Failed: GetSecret, namespace=%v, err=%v", testNamespace, err)
		return
	}
	c, err := NewCoreDumpUploaderSecret(secretData)
	if err != nil {
		t.Errorf("Failed: NewCoreDumpUploaderSecret, err=%v", err)
		return
	}
	assert.Equal(t, expected.AccessKey, c.AccessKey)
	assert.Equal(t, expected.SecretKey, c.SecretKey)
	assert.Equal(t, expected.Bucket, c.Bucket)
	assert.Equal(t, expected.Endpoint, c.Endpoint)
	assert.Equal(t, expected.KeyPrefix, c.KeyPrefix)
}

type MockK8sClient struct {
	resetClientFail    error
	checkNamespaceFail error
	getSecretFail      error
	malformedSecret    bool
	createBucket       bool
}

func NewMockK8sClient(resetClientFail error, checkNamespaceFail error, getSecretFail error, malformedSecret bool, createBucket bool) *MockK8sClient {
	return &MockK8sClient{
		resetClientFail: resetClientFail, checkNamespaceFail: checkNamespaceFail, getSecretFail: getSecretFail,
		malformedSecret: malformedSecret, createBucket: createBucket,
	}
}

func (k *MockK8sClient) ResetClient() error {
	return k.resetClientFail
}

func (k *MockK8sClient) CheckNamespace(string) error {
	return k.checkNamespaceFail
}

func (k *MockK8sClient) GetSecret(string) (map[string][]byte, error) {
	if k.getSecretFail != nil {
		return nil, k.getSecretFail
	}
	if k.malformedSecret {
		return map[string][]byte{
			"bucket": []byte("bucket"),
		}, nil
	}
	ret := map[string][]byte{
		"bucket":    []byte("bucket"),
		"keyPrefix": []byte("a/b/c"),
		"accessKey": []byte("ABCDEF"),
		"secretKey": []byte("12345"),
		"endpoint":  []byte("https://endpoint.io"),
	}
	if k.createBucket {
		ret["createBucket"] = []byte("true")
	} else {
		ret["createBucket"] = []byte("false")
	}
	return ret, nil
}

func (k *MockK8sClient) GetRawClient() *kubernetes.Clientset {
	return nil
}

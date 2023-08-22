package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type K8sClient interface {
	ResetClient() error
	CheckNamespace(namespace string) error
	GetSecret(namespace string) (map[string][]byte, error)
	GetRawClient() *kubernetes.Clientset
}

type K8sClientImpl struct {
	client                 *kubernetes.Clientset
	kubeConfigPath         string
	namespaceLabelSelector map[string]string
}

func ParseNamespaceLabelSelector(selectorString string) map[string]string {
	selector := make(map[string]string)
	for _, keyvalue := range strings.Split(selectorString, ",") {
		kvsplit := strings.Split(keyvalue, "=")
		if len(kvsplit) >= 2 {
			selector[kvsplit[0]] = kvsplit[1]
		}
	}
	return selector
}

func NewK8sClient(kubeConfigPath string, selectorString string) K8sClient {
	namespaceLabelSelector := ParseNamespaceLabelSelector(selectorString)
	return &K8sClientImpl{client: nil, kubeConfigPath: kubeConfigPath, namespaceLabelSelector: namespaceLabelSelector}
}

func (k *K8sClientImpl) ResetClient() error {
	var config *rest.Config = nil
	var err error
	if k.kubeConfigPath == "" {
		config, err = rest.InClusterConfig()
	} else {
		config, err = clientcmd.BuildConfigFromFlags("", k.kubeConfigPath)
	}
	if err != nil {
		return fmt.Errorf("failed: NewK8sClient, BuildConfigFromFlags, kubeConfigPath=%v, err=%v", k.kubeConfigPath, err)
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed: NewK8sClient, NewForConfig, err=%v", err)
	}
	k.client = client
	return nil
}

func (k *K8sClientImpl) CheckNamespace(namespace string) error {
	ns, err := k.client.CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed: CheckNamespace: not found namespace %v, err=%v", namespace, err)
	}
	var matched = false
	for key, value := range ns.GetLabels() {
		if found, ok := k.namespaceLabelSelector[key]; ok {
			if found == value {
				matched = true
				break
			}
		}
	}
	if !matched {
		return fmt.Errorf("failed: CheckNamespace: label selector did not match to namespace %v", namespace)
	}
	return nil
}

func (k *K8sClientImpl) GetSecret(namespace string) (map[string][]byte, error) {
	secrets, err := k.client.CoreV1().Secrets(namespace).List(context.TODO(), metav1.ListOptions{FieldSelector: "type=core-dump-handler"})
	if err != nil || len(secrets.Items) == 0 {
		return nil, fmt.Errorf("failed: GetSecret, not found core-dump-handler secrets in %v, err=%v, len(secrets.Items)=%v", namespace, err, len(secrets.Items))
	}
	var ret map[string][]byte = nil
	for _, secret := range secrets.Items {
		/* TODO: PVC validation at Admission Web Hook
		validated, ok := secret.GetAnnotations()["core-dump-handler/verified"]
		if !ok || validated != "true" {
			log.Printf("WARN: Ignore secret %v at %v without verification", secret.GetName(), namespace)
			continue
		}*/
		c := secret.Data
		if ret == nil {
			ret = c
		} else {
			log.Printf("WARN: GetSecret, Ignore duplicated secret for type=core-dump-handler (%v at %v)", secret.GetName(), namespace)
		}
	}
	return ret, err
}

func (k *K8sClientImpl) GetRawClient() *kubernetes.Clientset {
	return k.client
}

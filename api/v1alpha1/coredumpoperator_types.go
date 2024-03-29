/*
 * Copyright 2023- IBM Inc. All rights reserved
 * SPDX-License-Identifier: Apache-2.0
 */

package v1alpha1

import (
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// CoreDumpHandlerSpec defines the desired state of CoreDumpHandler
type CoreDumpHandlerSpec struct {
	// CrioEndPoint is the CRI-O's socket path to collect runtime information
	//+kubebuilder:default="unix:///run/containerd/containerd.sock"
	CrioEndPoint string `json:"crioEndPoint,omitempty"`

	// HostDir is a directory path in the host filesystem to collect core dumps and generate zip files
	//+kubebuilder:default="/mnt/core-dump-handler"
	HostDir string `json:"hostDir,omitempty"`

	// HandlerImage is the image for core-dump-handler to collect core dumps and runtime informations
	//+kubebuilder:default="quay.io/icdh/core-dump-handler:v8.10.0"
	HandlerImage string `json:"handlerImage,omitempty"`

	// UploaderImage is the image for core-dump-uploader to upload zip files generated by handlerImage containers
	//+kubebuilder:default="ghcr.io/ibm/core-dump-operator/core-dump-uploader:v0.0.1"
	UploaderImage string `json:"uploaderImage,omitempty"`

	// ImagePullSecret is used to download uploaderImage
	ImagePullSecret string `json:"imagePullSecret,omitempty"`

	// ServiceAccount is associated to daemonset pods that get/list secrets and namespaces
	ServiceAccount string `json:"serviceAccount,omitempty"`

	// NodeSelector restricts nodes that can run core dump daemonsets
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// NamespaceLabelSelector restricts namespaces that collect core dumps
	NamespaceLabelSelector map[string]string `json:"namespaceLabelSelector,omitempty"`

	// OpenShift specifies to handle securityContextConstraints
	OpenShift bool `json:"openShift,omitempty"`

	// Resource specifies resource requirements for each container
	Resource *corev1.ResourceRequirements `json:"resource,omitempty"`

	// Tolerations enable scheduling on nodes with taints
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Affinity adds scheduling affinity
	Affinity *AffinityApplyConfiguration `json:"affinity,omitempty"`
}

// CoreDumpHandlerStatus defines the observed state of CoreDumpHandler
type CoreDumpHandlerStatus struct {
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// CoreDumpHandler is the Schema for the CoreDumpHandlers API
type CoreDumpHandler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CoreDumpHandlerSpec   `json:"spec,omitempty"`
	Status CoreDumpHandlerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// CoreDumpHandlerList contains a list of CoreDumpHandler
type CoreDumpHandlerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CoreDumpHandler `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CoreDumpHandler{}, &CoreDumpHandlerList{})
}

type AffinityApplyConfiguration corev1apply.AffinityApplyConfiguration

func (in *AffinityApplyConfiguration) DeepCopy() *AffinityApplyConfiguration {
	out := new(AffinityApplyConfiguration)
	bytes, err := json.Marshal(in)
	if err != nil {
		panic("Failed to marshal")
	}
	err = json.Unmarshal(bytes, out)
	if err != nil {
		panic("Failed to unmarshal")
	}
	return out
}

/*
 * Copyright 2023- IBM Inc. All rights reserved
 * SPDX-License-Identifier: Apache-2.0
 */

package controllers

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	appsv1apply "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	metav1apply "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	chartsv1alpha1 "github.com/IBM/core-dump-operator/api/v1alpha1"
)

const coredumpHandlerFinalizer = "charts.ibm.com/finalizer"
const fieldManager = "core-dump-operator"

// CoreDumpHandlerReconciler reconciles a CoreDumpHandler object
type CoreDumpHandlerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// ManagedNamespace is the trusted namespace allowed to configure privileged agents.
	ManagedNamespace string
}

//+kubebuilder:rbac:groups=charts.ibm.com,resources=coredumphandlers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=charts.ibm.com,resources=coredumphandlers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=charts.ibm.com,resources=coredumphandlers/finalizers,verbs=update

//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=get;list;watch;create;update;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the CoreDumpHandler object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *CoreDumpHandlerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if r.ManagedNamespace == "" {
		return ctrl.Result{}, fmt.Errorf("managed namespace must be configured")
	}
	if req.Namespace != r.ManagedNamespace {
		return ctrl.Result{}, nil
	}
	logger := log.FromContext(ctx)
	l := logger.WithValues("CoreDumpHandler", req.NamespacedName)
	cdu := &chartsv1alpha1.CoreDumpHandler{}
	err := r.Get(ctx, req.NamespacedName, cdu)
	var requeue = false
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		l.Error(err, "Failed: Reconcile, Get", "namespace", req.Namespace, "name", req.Name)
		return ctrl.Result{RequeueAfter: 100 * time.Millisecond}, err
	}

	if cdu.GetDeletionTimestamp() != nil {
		if controllerutil.ContainsFinalizer(cdu, coredumpHandlerFinalizer) {
			if requeue, err = r.DeleteCluster(ctx, cdu, l); requeue || err != nil {
				return ctrl.Result{Requeue: requeue, RequeueAfter: 100 * time.Millisecond}, err
			}
			if requeue, err = r.DeleteScc(ctx, cdu, l); requeue || err != nil {
				return ctrl.Result{Requeue: requeue, RequeueAfter: 100 * time.Millisecond}, err
			}
			controllerutil.RemoveFinalizer(cdu, coredumpHandlerFinalizer)
			err = r.Update(ctx, cdu)
			if err != nil {
				l.Error(err, "Reconcile, Update")
			}
		}
	} else {
		if err := validateHandler(cdu); err != nil {
			return ctrl.Result{}, err
		}
		if !controllerutil.ContainsFinalizer(cdu, coredumpHandlerFinalizer) {
			controllerutil.AddFinalizer(cdu, coredumpHandlerFinalizer)
			if err := r.Update(ctx, cdu); err != nil {
				return ctrl.Result{}, err
			}
		}
		if requeue, err = r.UpdateScc(ctx, cdu, l); requeue || err != nil {
			return ctrl.Result{Requeue: requeue, RequeueAfter: 100 * time.Millisecond}, err
		}
		if requeue, err = r.UpdateCluster(ctx, cdu, l); requeue || err != nil {
			return ctrl.Result{Requeue: requeue, RequeueAfter: 100 * time.Millisecond}, err
		}
	}
	return ctrl.Result{Requeue: requeue}, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *CoreDumpHandlerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&chartsv1alpha1.CoreDumpHandler{}).
		Owns(&appsv1.DaemonSet{}).
		Complete(r)
}

func GetLabels(cdu *chartsv1alpha1.CoreDumpHandler) map[string]string {
	return map[string]string{"app": "core-dump-handler", "cluster": cdu.Name}
}

func (r *CoreDumpHandlerReconciler) UpdateCluster(ctx context.Context, cdu *chartsv1alpha1.CoreDumpHandler, l logr.Logger) (requeue bool, err error) {
	var ds, origApplyConfig *appsv1apply.DaemonSetApplyConfiguration
	var orig appsv1.DaemonSet
	err = r.Get(ctx, client.ObjectKey{Name: cdu.Name, Namespace: cdu.Namespace}, &orig)
	if err != nil && !errors.IsNotFound(err) {
		l.Error(err, "Failed: UpdateCluster, Get")
		return false, err
	} else if err == nil {
		if !metav1.IsControlledBy(&orig, cdu) {
			return false, fmt.Errorf("refusing to modify DaemonSet %s/%s: ownership does not match", orig.Namespace, orig.Name)
		}
		origApplyConfig, err = appsv1apply.ExtractDaemonSet(&orig, fieldManager)
		if err != nil {
			l.Error(err, "Failed: UpdateNodes, ExtractDaemonSet")
			return false, err
		}
		copied := *origApplyConfig
		ds = &copied
		ds.OwnerReferences = nil
	} else {
		ds = appsv1apply.DaemonSet(cdu.Name, cdu.Namespace)
	}
	labels := GetLabels(cdu)
	ds.WithLabels(labels).WithSpec(appsv1apply.DaemonSetSpec().
		WithSelector(metav1apply.LabelSelector().WithMatchLabels(labels)))

	pod := corev1apply.PodTemplateSpec().WithSpec(&corev1apply.PodSpecApplyConfiguration{}).WithLabels(labels)
	ds.Spec.WithTemplate(pod)

	limits := make(map[corev1.ResourceName]resource.Quantity)
	requests := make(map[corev1.ResourceName]resource.Quantity)
	if cdu.Spec.Resource != nil {
		for name, quantity := range cdu.Spec.Resource.Limits {
			limits[name] = quantity
		}
		for name, quantity := range cdu.Spec.Resource.Requests {
			requests[name] = quantity
		}
	}

	envs := []*corev1apply.EnvVarApplyConfiguration{
		corev1apply.EnvVar().WithName("COMP_FILENAME_TEMPLATE").WithValue("{uuid}-dump-{timestamp}-{hostname}-{exe_name}-{pid}-{signal}"),
		corev1apply.EnvVar().WithName("COMP_LOG_LENGTH").WithValue("500"),
		corev1apply.EnvVar().WithName("COMP_LOG_LEVEL").WithValue("Warn"),
		corev1apply.EnvVar().WithName("COMP_IGNORE_CRIO").WithValue("false"),
		corev1apply.EnvVar().WithName("COMP_CRIO_IMAGE_CMD").WithValue("images"),
		corev1apply.EnvVar().WithName("COMP_TIMEOUT").WithValue("600"),
		corev1apply.EnvVar().WithName("COMP_COMPRESSION").WithValue("true"),
		corev1apply.EnvVar().WithName("COMP_CORE_EVENTS").WithValue("false"),
		corev1apply.EnvVar().WithName("COMP_CORE_EVENT_DIR").WithValue(filepath.Join(cdu.Spec.HostDir, "events")),
		corev1apply.EnvVar().WithName("DEPLOY_CRIO_CONFIG").WithValue("false"),
		corev1apply.EnvVar().WithName("CRIO_ENDPOINT").WithValue(cdu.Spec.CrioEndPoint),
		corev1apply.EnvVar().WithName("HOST_DIR").WithValue(cdu.Spec.HostDir),
		corev1apply.EnvVar().WithName("CORE_DIR").WithValue(filepath.Join(cdu.Spec.HostDir, "cores")),
		corev1apply.EnvVar().WithName("EVENT_DIR").WithValue(filepath.Join(cdu.Spec.HostDir, "events")),
		corev1apply.EnvVar().WithName("SUID_DUMPABLE").WithValue("2"),
		corev1apply.EnvVar().WithName("DEPLOY_CRIO_EXE").WithValue("false"),
		corev1apply.EnvVar().WithName("USE_INOTIFY").WithValue("false"),
	}
	container1 := corev1apply.Container().WithName("agent").
		WithImage(cdu.Spec.HandlerImage).WithImagePullPolicy(corev1.PullIfNotPresent).WithCommand("/app/core-dump-agent").
		WithVolumeMounts(corev1apply.VolumeMount().WithName("host-volume").WithMountPath(cdu.Spec.HostDir)).
		WithEnv(envs...).WithSecurityContext(corev1apply.SecurityContext().WithPrivileged(true)).
		WithLifecycle(corev1apply.Lifecycle().WithPreStop(corev1apply.LifecycleHandler().WithExec(corev1apply.ExecAction().WithCommand("/app/core-dump-agent", "remove")))).
		WithResources(corev1apply.ResourceRequirements().WithLimits(limits).WithRequests(requests))

	command := []string{
		"/core-dump-uploader", fmt.Sprintf("--defaultNamespace=%v", cdu.Namespace), fmt.Sprintf("--watchDir=%v", filepath.Join(cdu.Spec.HostDir, "cores")),
	}
	if len(cdu.Spec.NamespaceLabelSelector) > 0 {
		// NOTE: label characters must be [a-z0-9A-Z/._-]. So, "=" and "," are available as delimiters
		selectorStrs := []string{}
		for key, value := range cdu.Spec.NamespaceLabelSelector {
			selectorStrs = append(selectorStrs, fmt.Sprintf("%s=%v", key, value))
		}
		command = append(command, fmt.Sprintf("--namespaceLabelSelector=%s", strings.Join(selectorStrs, ",")))
	}
	container2 := corev1apply.Container().WithName("uploader").
		WithImage(cdu.Spec.UploaderImage).WithImagePullPolicy(corev1.PullAlways).WithCommand(command...).
		WithVolumeMounts(corev1apply.VolumeMount().WithName("host-volume").WithMountPath(cdu.Spec.HostDir),
			corev1apply.VolumeMount().WithName("cores-volume").WithMountPath(filepath.Join(cdu.Spec.HostDir, "cores")),
			corev1apply.VolumeMount().WithName("events-volume").WithMountPath(filepath.Join(cdu.Spec.HostDir, "events"))).
		WithSecurityContext(corev1apply.SecurityContext().WithPrivileged(true)).
		WithResources(corev1apply.ResourceRequirements().WithLimits(limits).WithRequests(requests))

	pod.Spec.WithContainers(container1, container2).WithVolumes(
		corev1apply.Volume().WithName("host-volume").WithHostPath(corev1apply.HostPathVolumeSource().
			WithPath(cdu.Spec.HostDir).WithType(corev1.HostPathDirectoryOrCreate)),
		corev1apply.Volume().WithName("cores-volume").WithHostPath(corev1apply.HostPathVolumeSource().
			WithPath(filepath.Join(cdu.Spec.HostDir, "cores")).WithType(corev1.HostPathDirectoryOrCreate)),
		corev1apply.Volume().WithName("events-volume").WithHostPath(corev1apply.HostPathVolumeSource().
			WithPath(filepath.Join(cdu.Spec.HostDir, "events")).WithType(corev1.HostPathDirectoryOrCreate)),
	)

	if cdu.Spec.ImagePullSecret != "" {
		pod.Spec.WithImagePullSecrets(corev1apply.LocalObjectReference().WithName(cdu.Spec.ImagePullSecret))
	}
	if cdu.Spec.ServiceAccount != "" {
		pod.Spec.WithServiceAccountName(cdu.Spec.ServiceAccount)
	}
	if cdu.Spec.NodeSelector != nil {
		pod.Spec.WithNodeSelector(cdu.Spec.NodeSelector)
	}
	if cdu.Spec.Tolerations != nil {
		pod.Spec.Tolerations = nil
		for _, t := range cdu.Spec.Tolerations {
			pod.Spec.WithTolerations(corev1apply.Toleration().WithEffect(t.Effect).WithKey(t.Key).WithOperator(t.Operator).WithValue(t.Value))
		}
	}
	if cdu.Spec.Affinity != nil {
		pod.Spec.WithAffinity((*corev1apply.AffinityApplyConfiguration)(cdu.Spec.Affinity.DeepCopy()))
	}

	gvk, err := apiutil.GVKForObject(cdu, r.Scheme)
	if err != nil {
		l.Error(err, "Failed: UpdateCluster, GVKForObject")
		return false, err
	}
	ds.WithOwnerReferences(metav1apply.OwnerReference().
		WithAPIVersion(gvk.GroupVersion().String()).
		WithKind(gvk.Kind).
		WithName(cdu.Name).
		WithUID(cdu.GetUID()).
		WithBlockOwnerDeletion(true).
		WithController(true))

	firstApply := origApplyConfig == nil
	if !firstApply {
		if equality.Semantic.DeepEqual(ds, origApplyConfig) {
			return false, nil
		}
		diff := cmp.Diff(*origApplyConfig, *ds)
		if len(diff) > 0 {
			l.Info("UpdateCluster, Patch", "diff", diff)
		}
	}
	if !firstApply {
		ds.WithResourceVersion(orig.ResourceVersion)
	}
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(ds)
	if err != nil {
		l.Error(err, "Failed: UpdateCluster, ToUnstructured")
		return false, err
	}
	patch := &unstructured.Unstructured{Object: obj}
	if firstApply {
		if err := r.Create(ctx, patch, client.FieldOwner(fieldManager)); err != nil {
			return false, err
		}
		l.Info("Success: UpdateCluster, Create", "namespace", cdu.Namespace, "name", cdu.Name)
		return false, nil
	}
	if err = r.Patch(ctx, patch, client.Apply, &client.PatchOptions{FieldManager: fieldManager, Force: pointer.Bool(true)}); err != nil {
		l.Error(err, "Failed: UpdateCluster, Patch", "namespace", *ds.Namespace, "name", *ds.Name)
		return false, err
	}
	return false, nil
}

func (r *CoreDumpHandlerReconciler) DeleteCluster(ctx context.Context, cdu *chartsv1alpha1.CoreDumpHandler, l logr.Logger) (requeue bool, err error) {
	found := &appsv1.DaemonSet{}
	key := client.ObjectKey{Name: cdu.Name, Namespace: cdu.Namespace}
	err = r.Get(ctx, key, found)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		l.Error(err, "Failed: DeleteCluster, Get")
		return false, err
	}

	if !metav1.IsControlledBy(found, cdu) {
		l.Info("Leaving DaemonSet with different ownership untouched", "namespace", found.Namespace, "name", found.Name)
		return false, nil
	}
	if err := r.Delete(ctx, found, &client.DeleteOptions{Preconditions: &metav1.Preconditions{
		UID: &found.UID, ResourceVersion: &found.ResourceVersion,
	}}); client.IgnoreNotFound(err) != nil {
		l.Error(err, "Failed: DeleteCluster, Delete", "namespace", found.Namespace, "name", found.Name)
		return false, err
	}
	l.Info("Success: DeleteCluster", "namespace", found.Namespace, "name", found.Name)
	return false, nil
}

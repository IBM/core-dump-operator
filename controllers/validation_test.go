/*
 * Copyright 2026 IBM Inc. All rights reserved
 * SPDX-License-Identifier: Apache-2.0
 */

package controllers

import (
	"context"
	"testing"

	chartsv1alpha1 "github.com/IBM/core-dump-operator/api/v1alpha1"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestValidateLegacyHandler(t *testing.T) {
	for _, tc := range []struct {
		name, path      string
		selector        map[string]string
		allNodes, valid bool
	}{
		{name: "selected nodes", path: "/mnt/core-dump-handler", selector: map[string]string{"worker": "yes"}, valid: true},
		{name: "explicit all nodes", path: "/mnt/core-dump-handler/tenant/cores", allNodes: true, valid: true},
		{name: "host root", path: "/", allNodes: true},
		{name: "traversal", path: "/mnt/core-dump-handler/../etc", allNodes: true},
		{name: "empty host directory", allNodes: true},
		{name: "implicit all nodes", path: "/mnt/core-dump-handler"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cr := &chartsv1alpha1.CoreDumpHandler{Spec: chartsv1alpha1.CoreDumpHandlerSpec{HostDir: tc.path, NodeSelector: tc.selector, AllowAllNodes: tc.allNodes}}
			if err := validateHandler(cr); (err == nil) != tc.valid {
				t.Fatalf("validateHandler: %v; want valid=%v", err, tc.valid)
			}
		})
	}
}

type noSCCAPIClient struct{ client.Client }

func (c noSCCAPIClient) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: "security.openshift.io", Kind: "SecurityContextConstraints"}}
}

func TestSCCOnVanillaKubernetes(t *testing.T) {
	r := &CoreDumpHandlerReconciler{Client: noSCCAPIClient{}}
	cr := &chartsv1alpha1.CoreDumpHandler{}
	if _, err := r.UpdateScc(context.Background(), cr, logr.Discard()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.DeleteScc(context.Background(), cr, logr.Discard()); err != nil {
		t.Fatal(err)
	}
	cr.Spec.OpenShift = true
	if _, err := r.UpdateScc(context.Background(), cr, logr.Discard()); err == nil {
		t.Fatal("OpenShift SCC management without a UID must fail")
	}
	cr.UID = "test-uid"
	if _, err := r.UpdateScc(context.Background(), cr, logr.Discard()); !meta.IsNoMatchError(err) {
		t.Fatalf("missing SCC API must fail in OpenShift mode, got %v", err)
	}
}

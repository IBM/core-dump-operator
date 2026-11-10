/*
 * Copyright 2026 IBM Inc. All rights reserved
 * SPDX-License-Identifier: Apache-2.0
 */

package controllers

import (
	"fmt"
	"regexp"

	chartsv1alpha1 "github.com/IBM/core-dump-operator/api/v1alpha1"
)

var hostDirPattern = regexp.MustCompile(`^/mnt/core-dump-handler(/[A-Za-z0-9_-][A-Za-z0-9_.-]*)*$`)

func validateHandler(cdu *chartsv1alpha1.CoreDumpHandler) error {
	if !hostDirPattern.MatchString(cdu.Spec.HostDir) {
		return fmt.Errorf("hostDir must be /mnt/core-dump-handler or a subdirectory without traversal components")
	}
	if len(cdu.Spec.NodeSelector) == 0 && !cdu.Spec.AllowAllNodes {
		return fmt.Errorf("nodeSelector must be nonempty unless allowAllNodes is explicitly enabled")
	}
	return nil
}

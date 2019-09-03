/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package pod

import (
	"encoding/json"
	"context"
	"fmt"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	clientset "k8s.io/client-go/kubernetes"
)

// PatchPodStatus patches pod status.
func PatchPodStatus(ctx context.Context, c clientset.Interface, namespace, name string, oldPodStatus, newPodStatus v1.PodStatus, oldTraceAnnotation, newTraceAnnotation string) (*v1.Pod, []byte, error) {
	patchBytes, err := preparePatchBytesforPodStatus(namespace, name, oldPodStatus, newPodStatus, oldTraceAnnotation, newTraceAnnotation)
	if err != nil {
		return nil, nil, err
	}

	updatedPod, err := c.CoreV1().Pods(namespace).Patch(ctx, name, types.StrategicMergePatchType, patchBytes, "status")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to patch status %q for pod %q/%q: %v", patchBytes, namespace, name, err)
	}
	return updatedPod, patchBytes, nil
}

func preparePatchBytesforPodStatus(namespace, name string, oldPodStatus, newPodStatus v1.PodStatus, oldTraceAnnotation, newTraceAnnotation string) ([]byte, error) {
	oldData, err := json.Marshal(v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{"trace.kubernetes.io/context":oldTraceAnnotation},
		},
		Status: oldPodStatus,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to Marshal oldData for pod %q/%q: %v", namespace, name, err)
	}

	newData, err := json.Marshal(v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{"trace.kubernetes.io/context":newTraceAnnotation},
		},
		Status: newPodStatus,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to Marshal newData for pod %q/%q: %v", namespace, name, err)
	}

	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, v1.Pod{})
	if err != nil {
		return nil, fmt.Errorf("failed to CreateTwoWayMergePatch for pod %q/%q: %v", namespace, name, err)
	}
	return patchBytes, nil
}

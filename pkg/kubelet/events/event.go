/*
Copyright 2014 The Kubernetes Authors.

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

package events

import (
	"github.com/golang/glog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ref "k8s.io/client-go/tools/reference"
	"k8s.io/kubernetes/pkg/api/legacyscheme"
	"k8s.io/kubernetes/pkg/kubelet/metrics"
)

const (
	// Container event reason list
	CreatedContainer        = "Created"
	StartedContainer        = "Started"
	FailedToCreateContainer = "Failed"
	FailedToStartContainer  = "Failed"
	KillingContainer        = "Killing"
	PreemptContainer        = "Preempting"
	BackOffStartContainer   = "BackOff"
	ExceededGracePeriod     = "ExceededGracePeriod"

	// Pod event reason list
	FailedToKillPod                = "FailedKillPod"
	FailedToCreatePodContainer     = "FailedCreatePodContainer"
	FailedToMakePodDataDirectories = "Failed"
	NetworkNotReady                = "NetworkNotReady"

	// Image event reason list
	PullingImage            = "Pulling"
	PulledImage             = "Pulled"
	FailedToPullImage       = "Failed"
	FailedToInspectImage    = "InspectFailed"
	ErrImageNeverPullPolicy = "ErrImageNeverPull"
	BackOffPullImage        = "BackOff"

	// kubelet event reason list
	NodeReady                            = "NodeReady"
	NodeNotReady                         = "NodeNotReady"
	NodeSchedulable                      = "NodeSchedulable"
	NodeNotSchedulable                   = "NodeNotSchedulable"
	StartingKubelet                      = "Starting"
	KubeletSetupFailed                   = "KubeletSetupFailed"
	FailedAttachVolume                   = "FailedAttachVolume"
	FailedDetachVolume                   = "FailedDetachVolume"
	FailedMountVolume                    = "FailedMount"
	VolumeResizeFailed                   = "VolumeResizeFailed"
	VolumeResizeSuccess                  = "VolumeResizeSuccessful"
	FileSystemResizeFailed               = "FileSystemResizeFailed"
	FileSystemResizeSuccess              = "FileSystemResizeSuccessful"
	FailedUnMountVolume                  = "FailedUnMount"
	FailedMapVolume                      = "FailedMapVolume"
	FailedUnmapDevice                    = "FailedUnmapDevice"
	WarnAlreadyMountedVolume             = "AlreadyMountedVolume"
	SuccessfulDetachVolume               = "SuccessfulDetachVolume"
	SuccessfulAttachVolume               = "SuccessfulAttachVolume"
	SuccessfulMountVolume                = "SuccessfulMountVolume"
	SuccessfulUnMountVolume              = "SuccessfulUnMountVolume"
	HostPortConflict                     = "HostPortConflict"
	NodeSelectorMismatching              = "NodeSelectorMismatching"
	InsufficientFreeCPU                  = "InsufficientFreeCPU"
	InsufficientFreeMemory               = "InsufficientFreeMemory"
	NodeRebooted                         = "Rebooted"
	ContainerGCFailed                    = "ContainerGCFailed"
	ImageGCFailed                        = "ImageGCFailed"
	FailedNodeAllocatableEnforcement     = "FailedNodeAllocatableEnforcement"
	SuccessfulNodeAllocatableEnforcement = "NodeAllocatableEnforced"
	UnsupportedMountOption               = "UnsupportedMountOption"
	SandboxChanged                       = "SandboxChanged"
	FailedCreatePodSandBox               = "FailedCreatePodSandBox"
	FailedStatusPodSandBox               = "FailedPodSandBoxStatus"

	// Image manager event reason list
	InvalidDiskCapacity = "InvalidDiskCapacity"
	FreeDiskSpaceFailed = "FreeDiskSpaceFailed"

	// Probe event reason list
	ContainerUnhealthy = "Unhealthy"

	// Pod worker event reason list
	FailedSync = "FailedSync"

	// Config event reason list
	FailedValidation = "FailedValidation"

	// Lifecycle hooks
	FailedPostStartHook   = "FailedPostStartHook"
	FailedPreStopHook     = "FailedPreStopHook"
	UnfinishedPreStopHook = "UnfinishedPreStopHook"
)

// kubeletRecorder is a wrapper around EventRecorder that produces metrics about events
type kubeletRecorder struct {
	recorder record.EventRecorder
}

func (k *kubeletRecorder) Event(object runtime.Object, eventtype, reason, message string) {
	eventMetric(object, reason)
	k.recorder.Event(object, eventtype, reason, message)
}

// Eventf is just like Event, but with Sprintf for the message field.
func (k *kubeletRecorder) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	eventMetric(object, reason)
	k.recorder.Eventf(object, eventtype, reason, messageFmt, args)
}

// PastEventf is just like Eventf, but with an option to specify the event's 'timestamp' field.
func (k *kubeletRecorder) PastEventf(object runtime.Object, timestamp metav1.Time, eventtype, reason, messageFmt string, args ...interface{}) {
	eventMetric(object, reason)
	k.PastEventf(object, timestamp, eventtype, reason, messageFmt, args)
}

func (k *kubeletRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{}) {
	eventMetric(object, reason)
	k.AnnotatedEventf(object, annotations, eventtype, reason, messageFmt, args)
}

func NewKubeletRecorder(recorder record.EventRecorder) record.EventRecorder {
	return &kubeletRecorder{recorder: recorder}
}

func eventMetric(object runtime.Object, reason string) {
	ref, err := ref.GetReference(legacyscheme.Scheme, object)
	if err != nil {
		glog.Errorf("Could not construct reference to: '%#v' due to: '%v'. Will not report event: '%v'", object, err, reason)
		return
	}
	metrics.Events.WithLabelValues(reason, ref.Kind).Inc()
}

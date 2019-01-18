/*
Copyright 2015 The Kubernetes Authors.

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

package stats

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/klog"

	statsapi "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"
)

const (
	nodeContainerName = "machine"
	podContainerName  = "pod_sandbox"
)

var (
	cpuUsageDesc = prometheus.NewDesc(
		prometheus.BuildFQName("", "",
			"cpu_usage_millicore_seconds"),
		"Cumulative cpu time consumed in seconds",
		[]string{"container_name", "pod_name", "pod_namespace"}, nil)
	memoryUsageDesc = prometheus.NewDesc(
		prometheus.BuildFQName("", "",
			"memory_working_set_bytes"),
		"Current working set in bytes",
		[]string{"container_name", "pod_name", "pod_namespace"}, nil)
	ephemeralStorageUsageDesc = prometheus.NewDesc(
		prometheus.BuildFQName("", "",
			"ephemeral_storage_usage_bytes"),
		"Current ephemeral storage in bytes",
		[]string{"container_name", "pod_name", "pod_namespace"}, nil)
)

func NewPrometheusCoreCollector(provider SummaryProvider) *coreCollector {
	return &coreCollector{
		provider: provider,
		errors: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "scrape_error",
			Help: "1 if there was an error while getting container metrics, 0 otherwise",
		}),
	}
}

type coreCollector struct {
	provider SummaryProvider
	errors   prometheus.Gauge
}

func (pc *coreCollector) Describe(ch chan<- *prometheus.Desc) {
	pc.errors.Describe(ch)
	ch <- cpuUsageDesc
	ch <- memoryUsageDesc
	ch <- ephemeralStorageUsageDesc
}

func (pc *coreCollector) Collect(ch chan<- prometheus.Metric) {
	pc.errors.Set(0)
	summary, err := pc.provider.Get(false)
	if err != nil {
		pc.errors.Set(1)
		klog.Warningf("Error getting summary for core prometheus endpoint: %v", err)
		return
	}
	pc.errors.Collect(ch)

	collectMetrics(ch, summary.Node.CPU, summary.Node.Memory, summary.Node.Fs, nodeContainerName, "", "")
	for _, pod := range summary.Pods {
		collectMetrics(ch, pod.CPU, pod.Memory, pod.EphemeralStorage, podContainerName, pod.PodRef.Name, pod.PodRef.Namespace)
		for _, container := range pod.Containers {
			for i := 0; i < 100; i++ {
				collectMetrics(ch, container.CPU, container.Memory, container.Rootfs, fmt.Sprintf("%s%d", container.Name, i), pod.PodRef.Name, pod.PodRef.Namespace)
			}
		}
	}
}

func collectMetrics(ch chan<- prometheus.Metric, cpu *statsapi.CPUStats, memory *statsapi.MemoryStats, disk *statsapi.FsStats, labelValues ...string) {
	if cpu.UsageNanoCores != nil {
		ch <- prometheus.MustNewConstMetric(
			cpuUsageDesc,
			prometheus.GaugeValue,
			float64(*cpu.UsageNanoCores)/float64(time.Millisecond),
			labelValues...)
	}

	if memory.WorkingSetBytes != nil {
		ch <- prometheus.MustNewConstMetric(
			memoryUsageDesc,
			prometheus.GaugeValue,
			float64(*memory.WorkingSetBytes),
			labelValues...)
	}
	if disk.UsedBytes != nil {
		ch <- prometheus.MustNewConstMetric(
			ephemeralStorageUsageDesc,
			prometheus.GaugeValue,
			float64(*disk.UsedBytes),
			labelValues...)
	}
}

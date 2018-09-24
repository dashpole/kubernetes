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

package stats

import (
	"golang.org/x/net/context"

	coreapi "k8s.io/kubernetes/pkg/kubelet/apis/corestats/v1alpha1"
	statsapi "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"
)

func NewGRPCCoreStatsProvider(provider SummaryProvider) coreapi.CoreStatsServer {
	return &grpcCoreStatsProvider{provider: provider}
}

type grpcCoreStatsProvider struct {
	provider SummaryProvider
}

func (g *grpcCoreStatsProvider) Get(ctx context.Context, req *coreapi.StatsRequest) (*coreapi.StatsResponse, error) {
	summary, err := g.provider.Get(false)
	if err != nil {
		return nil, err
	}

	podsUsage := []*coreapi.PodUsage{}
	for _, pod := range summary.Pods {

		containersStats := []*coreapi.ContainerUsage{}
		for _, container := range pod.Containers {

			containersStats = append(containersStats, &coreapi.ContainerUsage{
				Name:  container.Name,
				Usage: summaryStatsToCoreUsage(container.CPU, container.Memory, container.Rootfs),
			})
		}

		podsUsage = append(podsUsage, &coreapi.PodUsage{
			Name:       pod.PodRef.Name,
			Namespace:  pod.PodRef.Namespace,
			Usage:      summaryStatsToCoreUsage(pod.CPU, pod.Memory, pod.EphemeralStorage),
			Containers: containersStats,
		})
	}
	return &coreapi.StatsResponse{
		Node: summaryStatsToCoreUsage(summary.Node.CPU, summary.Node.Memory, summary.Node.Fs),
		Pods: podsUsage,
	}, nil
}

func summaryStatsToCoreUsage(cpu *statsapi.CPUStats, memory *statsapi.MemoryStats, ephemeralStorage *statsapi.FsStats) *coreapi.Usage {
	return &coreapi.Usage{
		Time: cpu.Time.Time.UnixNano(),
		CpuUsageCoreNanoSeconds:    *cpu.UsageCoreNanoSeconds,
		MemoryWorkingSetBytes:      *memory.WorkingSetBytes,
		EphemeralStorageUsageBytes: *ephemeralStorage.UsedBytes,
	}
}

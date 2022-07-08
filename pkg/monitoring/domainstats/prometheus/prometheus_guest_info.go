/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2018 Red Hat, Inc.
 *
 */

package prometheus

import (
	"k8s.io/client-go/tools/cache"

	k6tv1 "kubevirt.io/api/core/v1"
	"kubevirt.io/kubevirt/pkg/controller"
	"kubevirt.io/kubevirt/pkg/virt-launcher/virtwrap/api"

	"github.com/prometheus/client_golang/prometheus"

	"kubevirt.io/client-go/log"
)

var (
	customlabelPrefix = "kubevirt_vmi_"
	// string default value
	defaultValue = 1.0
)

var (
	_KB           = 1024
	_MB           = _KB * 1024
	_GB           = _MB * 1024
	_nanoSeconds  = 1000000000
	_milliSeconds = 1000000
	guestStateMap = map[string]int{
		"NoState":      1,
		"Running":      2,
		"Blocked":      3,
		"Paused":       4,
		"ShuttingDown": 5,
		"Shutoff":      6,
		"Crashed":      7,
		"PMSuspended":  8,
	}
)

//负责kernel version、guest state的metrics报告，这些直接从local cache获取
type GuestExtraCollector struct {
	vmiInformer    cache.SharedIndexInformer
	domainInformer cache.SharedInformer
}

func SetupGuestInfoCollector(nodeName string, MaxRequestsInFlight int, vmiInformer cache.SharedIndexInformer, domainInformer cache.SharedInformer) {
	log.Log.Infof("Starting Guest OS Info collector: node name=%v", nodeName)
	guestState := &GuestExtraCollector{
		vmiInformer:    vmiInformer,
		domainInformer: domainInformer,
	}
	prometheus.MustRegister(guestState)
}
func (co *GuestExtraCollector) Describe(_ chan<- *prometheus.Desc) {
	// TODO: Use DescribeByCollect?
}

func (co *GuestExtraCollector) Collect(ch chan<- prometheus.Metric) {
	defer func() {
		if err := recover(); err != nil {
			log.Log.V(2).Errorf("GuestStateCollector error: %s", err)
		}
	}()
	cachedObjs := co.vmiInformer.GetIndexer().List()
	if len(cachedObjs) == 0 {
		log.Log.V(4).Infof("No VMIs detected")
		return
	}
	for _, obj := range cachedObjs {
		vmi := obj.(*k6tv1.VirtualMachineInstance)
		vmiMetrics := newVmiMetrics(vmi, ch)
		vmiMetrics.updateKubernetesLabels()
		//直接从本地domain cache获取state
		if domainInf, exist, err := co.domainInformer.GetStore().GetByKey(controller.VirtualMachineInstanceKey(vmi)); exist && err == nil {
			domain := domainInf.(*api.Domain)
			vmiMetrics.pushCustomMetric(
				customlabelPrefix+"guest_state",
				"guest state:'NoState:1,Running:2,Blocked:3,Paused:4,ShuttingDown:5,Shutoff:6,Crashed:7,PMSuspended:8'",
				prometheus.GaugeValue,
				float64(guestStateToInt(domain.Status.Status)),
				[]string{"guest_state"},
				[]string{
					string(domain.Status.Status),
				},
			)
			vmiMetrics.pushCustomMetric(
				customlabelPrefix+"OS",
				"guest os kernel info",
				prometheus.GaugeValue,
				defaultValue,
				[]string{"kernel_version", "kernel_release", "machine", "kernel_name"},
				[]string{
					domain.Status.OSInfo.KernelVersion,
					domain.Status.OSInfo.KernelRelease,
					domain.Status.OSInfo.Machine,
					domain.Status.OSInfo.Name,
				},
			)
			vmiMetrics.pushCommonMetric(
				customlabelPrefix+"mm_available",
				"guest os  available memory (KB)",
				prometheus.GaugeValue,
				float64(domain.Status.GuestMMInfo.AvailableKB),
			)
			//qa低版本获取不到总容量、使用量等信息，需要使用3.0及以上
			for i := 0; i < len(domain.Status.DiskInfo); i++ {
				vmiMetrics.pushCustomMetric(
					customlabelPrefix+"disk_total",
					"guest os disk capacity (byte)",
					prometheus.GaugeValue,
					float64(domain.Status.DiskInfo[i].TotalBytes),
					[]string{"disk_name"},
					[]string{
						domain.Status.DiskInfo[i].DiskName,
					},
				)
				vmiMetrics.pushCustomMetric(
					customlabelPrefix+"disk_usage",
					"guest os disk usage (byte)",
					prometheus.GaugeValue,
					float64(domain.Status.DiskInfo[i].UsedBytes),
					[]string{"disk_name"},
					[]string{
						domain.Status.DiskInfo[i].DiskName,
					},
				)
			}
		}
	}
}

func guestStateToInt(state api.LifeCycle) int {
	if v, ok := guestStateMap[string(state)]; ok {
		return v
	} else {
		return -1
	}
}

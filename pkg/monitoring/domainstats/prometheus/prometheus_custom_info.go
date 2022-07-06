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

type OSKernel string

var (
	customlabelPrefix = "kubevirt_vmi_"
	// string default value
	defaultValue = 1.0
	//kernel version
	OS                       = customlabelPrefix + "OS"
	OSHelp                   = "guest os kernel info"
	OSLabels                 = []string{"kernel_version", "kernel_release", "machine", "kernel_name"}
	OSKernelLinux   OSKernel = "linux"
	OSKernelWin     OSKernel = "win"
	OSKernelUnknown OSKernel = "unknown"
	//cpu info
	CPUUtilization     = customlabelPrefix + "CPU_utilization"
	CPUUtilizationHelp = "guest os cpu utilization"
	//guest inside memory info
	memoryAvailable     = customlabelPrefix + "mm_available"
	memoryAvailableHelp = "guest os  available memory (KB)"

	//disk info
	diskTotal       = customlabelPrefix + "disk_total"
	diskTotalHelp   = "guest os  total disk (byte)"
	diskTotalLabels = []string{"disk_name"}

	diskUsed       = customlabelPrefix + "disk_usage"
	diskUsedHelp   = "guest os  used disk (byte)"
	diskUsedLabels = []string{"disk_name"}

	//guest state
	guestState       = customlabelPrefix + "guest_state"
	guestStateHelp   = "guest state"
	guestStateLabels = []string{"guest_state"}
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
				guestState,
				guestStateHelp,
				prometheus.GaugeValue,
				float64(guestStateToInt(domain.Status.Status)),
				guestStateLabels,
				[]string{
					string(domain.Status.Status),
				},
			)
			vmiMetrics.pushCustomMetric(
				OS,
				OSHelp,
				prometheus.GaugeValue,
				defaultValue,
				OSLabels,
				[]string{
					domain.Status.OSInfo.KernelVersion,
					domain.Status.OSInfo.KernelRelease,
					domain.Status.OSInfo.Machine,
					domain.Status.OSInfo.Name,
				},
			)
			vmiMetrics.pushCommonMetric(
				memoryAvailable,
				memoryAvailableHelp,
				prometheus.GaugeValue,
				float64(domain.Status.GuestMMInfo.AvailableKB),
			)
			for i := 0; i < len(domain.Status.DiskInfo); i++ {
				vmiMetrics.pushCustomMetric(
					diskTotal,
					diskTotalHelp,
					prometheus.GaugeValue,
					float64(domain.Status.DiskInfo[i].TotalKB),
					diskTotalLabels,
					[]string{
						domain.Status.DiskInfo[i].Name,
					},
				)
				vmiMetrics.pushCustomMetric(
					diskUsed,
					diskUsedHelp,
					prometheus.GaugeValue,
					float64(domain.Status.DiskInfo[i].UsedKB),
					diskUsedLabels,
					[]string{
						domain.Status.DiskInfo[i].Name,
					},
				)
			}
		}
	}
}

type diskInfo struct {
	Name string `json:"name,omitempty"`
	//Mount   string `json:"mount,omitempty"`
	TotalKB int64 `json:"totalKB,omitempty"`
	UsedKB  int64 `json:"usedKB,omitempty"`
}

//虚拟机里内存占用(KB)
type guestMM struct {
	TotalKB     int64 `json:"totalKB,omitempty"`
	AvailableKB int64 `json:"AvailableKB,omitempty"`
}

func guestStateToInt(state api.LifeCycle) int {
	if v, ok := guestStateMap[string(state)]; ok {
		return v
	} else {
		return -1
	}
}

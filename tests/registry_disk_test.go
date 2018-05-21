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
 * Copyright 2017 Red Hat, Inc.
 *
 */

package tests_test

import (
	"flag"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/runtime"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"kubevirt.io/kubevirt/pkg/api/v1"
	"kubevirt.io/kubevirt/pkg/kubecli"
	"kubevirt.io/kubevirt/tests"
)

var _ = Describe("RegistryDisk", func() {

	flag.Parse()

	virtClient, err := kubecli.GetKubevirtClient()
	tests.PanicOnError(err)

	BeforeEach(func() {
		tests.BeforeTestCleanup()
	})

	LaunchVM := func(vm *v1.VirtualMachine) runtime.Object {
		By("Starting a VM")
		obj, err := virtClient.RestClient().Post().Resource("virtualmachines").Namespace(tests.NamespaceTestDefault).Body(vm).Do().Get()
		Expect(err).To(BeNil())
		return obj
	}

	VerifyRegistryDiskVM := func(vm *v1.VirtualMachine, obj runtime.Object, ignoreWarnings bool) {
		_, ok := obj.(*v1.VirtualMachine)
		Expect(ok).To(BeTrue(), "Object is not of type *v1.VM")
		if ignoreWarnings == true {
			tests.WaitForSuccessfulVMStartIgnoreWarnings(obj)
		} else {
			tests.WaitForSuccessfulVMStart(obj)
		}

		// Verify Registry Disks are Online
		pods, err := virtClient.CoreV1().Pods(tests.NamespaceTestDefault).List(tests.UnfinishedVMPodSelector(vm))
		Expect(err).To(BeNil())

		By("Checking the number of VM disks")
		disksFound := 0
		for _, pod := range pods.Items {
			if pod.ObjectMeta.DeletionTimestamp != nil {
				continue
			}
			for _, containerStatus := range pod.Status.ContainerStatuses {
				if strings.HasPrefix(containerStatus.Name, "volume") == false {
					// only check readiness of disk containers
					continue
				}
				disksFound++
			}
			break
		}
		Expect(disksFound).To(Equal(1))
	}

	Describe("Starting and stopping the same VM", func() {
		Context("with ephemeral registry disk", func() {
			It("should success multiple times", func() {
				vm := tests.NewRandomVMWithEphemeralDisk(tests.RegistryDiskFor(tests.RegistryDiskCirros))
				num := 2
				for i := 0; i < num; i++ {
					By("Starting the VM")
					obj, err := virtClient.RestClient().Post().Resource("virtualmachines").Namespace(tests.NamespaceTestDefault).Body(vm).Do().Get()
					Expect(err).To(BeNil())
					tests.WaitForSuccessfulVMStartWithTimeout(obj, 180)

					By("Stopping the VM")
					_, err = virtClient.RestClient().Delete().Resource("virtualmachines").Namespace(vm.GetObjectMeta().GetNamespace()).Name(vm.GetObjectMeta().GetName()).Do().Get()
					Expect(err).To(BeNil())
					By("Waiting until the VM is gone")
					tests.WaitForVirtualMachineToDisappearWithTimeout(vm, 120)
				}
			})
		})
	})

	Describe("Starting a VM", func() {
		Context("with ephemeral registry disk", func() {
			It("should not modify the spec on status update", func() {
				vm := tests.NewRandomVMWithEphemeralDisk(tests.RegistryDiskFor(tests.RegistryDiskCirros))
				v1.SetObjectDefaults_VirtualMachine(vm)

				By("Starting the VM")
				vm, err := virtClient.VM(tests.NamespaceTestDefault).Create(vm)
				Expect(err).To(BeNil())
				tests.WaitForSuccessfulVMStartWithTimeout(vm, 60)
				startedVM, err := virtClient.VM(tests.NamespaceTestDefault).Get(vm.ObjectMeta.Name, metav1.GetOptions{})
				Expect(err).To(BeNil())
				By("Checking that the VM spec did not change")
				Expect(startedVM.Spec).To(Equal(vm.Spec))
			})
		})
	})

	Describe("Starting multiple VMs", func() {
		Context("with ephemeral registry disk", func() {
			It("should success", func() {
				num := 5
				vms := make([]*v1.VirtualMachine, 0, num)
				objs := make([]runtime.Object, 0, num)
				for i := 0; i < num; i++ {
					vm := tests.NewRandomVMWithEphemeralDisk(tests.RegistryDiskFor(tests.RegistryDiskCirros))
					// FIXME if we give too much ram, the vms really boot and eat all our memory (cache?)
					vm.Spec.Domain.Resources.Requests[k8sv1.ResourceMemory] = resource.MustParse("1M")
					obj := LaunchVM(vm)
					vms = append(vms, vm)
					objs = append(objs, obj)
				}

				for idx, vm := range vms {
					// TODO once networking is implemented properly set ignoreWarnings == false here.
					// We have to ignore warnings because VMs started in parallel
					// may cause libvirt to fail to create the macvtap device in
					// the host network.
					// The new network implementation we're working on should resolve this.
					// NOTE the VM still starts successfully regardless of this warning.
					// It just requires virt-handler to retry the Start command at the moment.
					VerifyRegistryDiskVM(vm, objs[idx], true)
				}
			}) // Timeout is long because this test involves multiple parallel VM launches.
		})
	})
})

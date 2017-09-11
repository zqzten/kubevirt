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

package virthandler_test

import (
	"net/http"
	"testing"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	k8sv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"kubevirt.io/kubevirt/pkg/api/v1"
	configdisk "kubevirt.io/kubevirt/pkg/config-disk"
	"kubevirt.io/kubevirt/pkg/kubecli"
	"kubevirt.io/kubevirt/pkg/logging"
	. "kubevirt.io/kubevirt/pkg/virt-handler"
	"kubevirt.io/kubevirt/pkg/virt-handler/virtwrap"
)

var _ = Describe("VM", func() {
	var server *ghttp.Server
	var vmStore cache.Store
	var vmQueue workqueue.RateLimitingInterface
	var domainManager *virtwrap.MockDomainManager

	var ctrl *gomock.Controller
	var dispatch kubecli.ControllerDispatch

	var recorder record.EventRecorder

	logging.DefaultLogger().SetIOWriter(GinkgoWriter)

	BeforeEach(func() {
		server = ghttp.NewServer()
		host := ""

		virtClient, err := kubecli.GetKubevirtClientFromFlags(server.URL(), "")
		Expect(err).ToNot(HaveOccurred())

		restClient := virtClient.RestClient()

		vmStore = cache.NewStore(cache.DeletionHandlingMetaNamespaceKeyFunc)
		vmQueue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

		ctrl = gomock.NewController(GinkgoT())
		domainManager = virtwrap.NewMockDomainManager(ctrl)

		configDiskClient := configdisk.NewConfigDiskClient(virtClient)

		recorder = record.NewFakeRecorder(100)
		dispatch = NewVMHandlerDispatch(domainManager, recorder, restClient, virtClient, host, configDiskClient)

	})

	Context("VM controller gets informed about a Domain change through the Domain controller", func() {
		It("should kill the Domain if no cluster wide equivalent exists", func(done Done) {
			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/apis/kubevirt.io/v1alpha1/namespaces/default/vms/testvm"),
					ghttp.RespondWithJSONEncoded(http.StatusNotFound, struct{}{}),
				),
			)
			domainManager.EXPECT().RemoveVMSecrets(v1.NewVMReferenceFromName("testvm")).Return(nil)
			domainManager.EXPECT().KillVM(v1.NewVMReferenceFromName("testvm")).Do(func(vm *v1.VM) {
				close(done)
			})

			dispatch.Execute(vmStore, vmQueue, "default/testvm")
		}, 1)
		It("should leave the Domain alone if the VM is migrating to its host", func() {
			vm := v1.NewMinimalVM("testvm")
			vm.Status.MigrationNodeName = "master"
			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", "/apis/kubevirt.io/v1alpha1/namespaces/default/vms/testvm"),
					ghttp.RespondWithJSONEncoded(http.StatusOK, vm),
				),
			)
			vmStore.Add(vm)
			dispatch.Execute(vmStore, vmQueue, "default/testvm")

		})
		It("should re-enqueue if the Key is unparseable", func() {
			Expect(vmQueue.Len()).Should(Equal(0))
			vmQueue.Add("a/b/c/d/e")
			kubecli.Dequeue(vmStore, vmQueue, dispatch)
			Expect(vmQueue.NumRequeues("a/b/c/d/e")).To(Equal(1))
		})

		table.DescribeTable("should leave the VM alone if it is in the final phase", func(phase v1.VMPhase) {
			vm := v1.NewMinimalVM("testvm")
			vm.Status.Phase = phase
			vmStore.Add(vm)

			vmQueue.Add("default/testvm")
			kubecli.Dequeue(vmStore, vmQueue, dispatch)
			// expect no mock interactions
			Expect(vmQueue.NumRequeues("default/testvm")).To(Equal(0))
		},
			table.Entry("succeeded", v1.Succeeded),
			table.Entry("failed", v1.Failed),
		)
	})

	AfterEach(func() {
		server.Close()
		ctrl.Finish()
	})
})

var _ = Describe("PVC", func() {
	RegisterFailHandler(Fail)

	logging.DefaultLogger().SetIOWriter(GinkgoWriter)

	var (
		expectedPVC k8sv1.PersistentVolumeClaim
		expectedPV  k8sv1.PersistentVolume
		server      *ghttp.Server
	)

	BeforeEach(func() {
		expectedPVC = k8sv1.PersistentVolumeClaim{
			TypeMeta: metav1.TypeMeta{
				Kind:       "PersistentVolumeClaim",
				APIVersion: "v1",
			},
			Spec: k8sv1.PersistentVolumeClaimSpec{
				VolumeName: "disk-01",
			},
			Status: k8sv1.PersistentVolumeClaimStatus{
				Phase: k8sv1.ClaimBound,
			},
		}

		source := k8sv1.ISCSIVolumeSource{
			IQN:          "iqn.2009-02.com.test:for.all",
			Lun:          1,
			TargetPortal: "127.0.0.1:6543",
		}

		expectedPV = k8sv1.PersistentVolume{
			TypeMeta: metav1.TypeMeta{
				Kind:       "PersistentVolume",
				APIVersion: "v1",
			},
			Spec: k8sv1.PersistentVolumeSpec{
				PersistentVolumeSource: k8sv1.PersistentVolumeSource{
					ISCSI: &source,
				},
			},
		}

		server = ghttp.NewServer()
		server.AppendHandlers(
			ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", "/api/v1/namespaces/default/persistentvolumeclaims/test-claim"),
				ghttp.RespondWithJSONEncoded(http.StatusOK, expectedPVC),
			),
			ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", "/api/v1/persistentvolumes/disk-01"),
				ghttp.RespondWithJSONEncoded(http.StatusOK, expectedPV),
			),
		)
	})

	AfterEach(func() {
		server.Close()
	})

	Context("Map Source Disks", func() {
		It("looks up and applies PVC", func() {
			vm := v1.VM{}

			disk := v1.Disk{
				Type: "PersistentVolumeClaim",
				Source: v1.DiskSource{
					Name: "test-claim",
				},
				Target: v1.DiskTarget{
					Device: "vda",
				},
			}
			disk.Type = "PersistentVolumeClaim"

			domain := v1.DomainSpec{}
			domain.Devices.Disks = []v1.Disk{disk}
			vm.Spec.Domain = &domain

			restClient := getRestClient(server.URL())
			vmCopy, err := MapPersistentVolumes(&vm, restClient, k8sv1.NamespaceDefault)
			Expect(err).NotTo(HaveOccurred())

			Expect(len(vmCopy.Spec.Domain.Devices.Disks)).To(Equal(1))
			newDisk := vmCopy.Spec.Domain.Devices.Disks[0]
			Expect(newDisk.Type).To(Equal("network"))
			Expect(newDisk.Driver.Type).To(Equal("raw"))
			Expect(newDisk.Driver.Name).To(Equal("qemu"))
			Expect(newDisk.Device).To(Equal("disk"))
			Expect(newDisk.Source.Protocol).To(Equal("iscsi"))
			Expect(newDisk.Source.Name).To(Equal("iqn.2009-02.com.test:for.all/1"))
		})
	})
})

func getRestClient(url string) *rest.RESTClient {
	gv := schema.GroupVersion{Group: "", Version: "v1"}
	restConfig, err := clientcmd.BuildConfigFromFlags(url, "")
	Expect(err).NotTo(HaveOccurred())
	restConfig.GroupVersion = &gv
	restConfig.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: scheme.Codecs}
	restConfig.APIPath = "/api"
	restConfig.ContentType = runtime.ContentTypeJSON
	restClient, err := rest.RESTClientFor(restConfig)
	Expect(err).NotTo(HaveOccurred())
	return restClient
}

func TestVMs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PVC")
}

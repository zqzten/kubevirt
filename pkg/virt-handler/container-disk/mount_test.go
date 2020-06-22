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

package container_disk

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/types"

	v1 "kubevirt.io/client-go/api/v1"
	diskutils "kubevirt.io/kubevirt/pkg/ephemeral-disk-utils"
)

var _ = Describe("ContainerDisk", func() {
	var tmpDir string
	var m *mounter
	var err error
	var vmi *v1.VirtualMachineInstance

	BeforeEach(func() {
		tmpDir, err = ioutil.TempDir("", "containerdisktest")
		Expect(err).ToNot(HaveOccurred())
		vmi = v1.NewMinimalVMI("fake-vmi")
		vmi.UID = "1234"

		m = &mounter{
			mountRecords:  make(map[types.UID]*vmiMountTargetRecord),
			mountStateDir: tmpDir,
		}
	})

	AfterEach(func() {
		os.RemoveAll(tmpDir)
	})

	Describe("container-disk", func() {
		Context("verify mount target recording for vmi", func() {
			It("should set and get same results", func() {

				// verify reading non-existent results just returns empty slice
				record, err := m.getMountTargetRecord(vmi)
				Expect(err).ToNot(HaveOccurred())
				Expect(record).To(BeNil())

				// verify setting a result works
				record = &vmiMountTargetRecord{
					MountTargetEntries: []vmiMountTargetEntry{
						{
							TargetFile: "sometargetfile",
							SocketFile: "somesocketfile",
						},
					},
				}
				err = m.setMountTargetRecord(vmi, record)
				Expect(err).ToNot(HaveOccurred())

				// verify the file actually exists
				recordFile := filepath.Join(tmpDir, string(vmi.UID))
				exists, err := diskutils.FileExists(recordFile)
				Expect(err).ToNot(HaveOccurred())
				Expect(exists).To(BeTrue())

				// verify we can read a result
				record, err = m.getMountTargetRecord(vmi)
				Expect(err).ToNot(HaveOccurred())
				Expect(len(record.MountTargetEntries)).To(Equal(1))
				Expect(record.MountTargetEntries[0].TargetFile).To(Equal("sometargetfile"))
				Expect(record.MountTargetEntries[0].SocketFile).To(Equal("somesocketfile"))

				// verify we can read a result directly from disk if the entry
				// doesn't exist in the map
				delete(m.mountRecords, vmi.UID)
				record, err = m.getMountTargetRecord(vmi)
				Expect(err).ToNot(HaveOccurred())
				Expect(len(record.MountTargetEntries)).To(Equal(1))
				Expect(record.MountTargetEntries[0].TargetFile).To(Equal("sometargetfile"))
				Expect(record.MountTargetEntries[0].SocketFile).To(Equal("somesocketfile"))

				// verify the cache is populated again with the mount info after reading from disk
				_, ok := m.mountRecords[vmi.UID]
				Expect(ok).To(BeTrue())

				// verify delete results
				err = m.deleteMountTargetRecord(vmi)
				Expect(err).ToNot(HaveOccurred())

				// verify the file is actually removed
				exists, err = diskutils.FileExists(recordFile)
				Expect(err).ToNot(HaveOccurred())
				Expect(exists).To(BeFalse())

				// verify deleting results that don't exist won't fail
				err = m.deleteMountTargetRecord(vmi)
				Expect(err).ToNot(HaveOccurred())

				// verify reading deleted results just returns empty slice
				record, err = m.getMountTargetRecord(vmi)
				Expect(err).ToNot(HaveOccurred())
				Expect(record).To(BeNil())
			})
		})
	})
})

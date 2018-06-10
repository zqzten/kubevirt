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

package v1

import (
	"bytes"
	"encoding/json"
	"text/template"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/api/core/v1"
)

type NetworkTemplateConfig struct {
	InterfaceConfig string
	NetworkConfig   string
}

var exampleJSON = `{
  "kind": "VirtualMachine",
  "apiVersion": "kubevirt.io/v1alpha1",
  "metadata": {
    "name": "testvm",
    "namespace": "default",
    "selfLink": "/apis/kubevirt.io/v1alpha1/namespaces/default/virtualmachines/testvm",
    "creationTimestamp": null
  },
  "spec": {
    "domain": {
      "resources": {
        "requests": {
          "memory": "8Mi"
        }
      },
      "cpu": {
        "cores": 3
      },
      "machine": {
        "type": "q35"
      },
      "firmware": {
        "uuid": "28a42a60-44ef-4428-9c10-1a6aee94627f"
      },
      "clock": {
        "utc": {},
        "timer": {
          "hpet": {
            "present": true
          },
          "kvm": {
            "present": true
          },
          "pit": {
            "present": true
          },
          "rtc": {
            "present": true
          },
          "hyperv": {
            "present": true
          }
        }
      },
      "features": {
        "acpi": {
          "enabled": false
        },
        "apic": {
          "enabled": true
        },
        "hyperv": {
          "relaxed": {
            "enabled": true
          },
          "vapic": {
            "enabled": false
          },
          "spinlocks": {
            "enabled": true,
            "spinlocks": 4096
          },
          "vpindex": {
            "enabled": false
          },
          "runtime": {
            "enabled": true
          },
          "synic": {
            "enabled": false
          },
          "synictimer": {
            "enabled": true
          },
          "reset": {
            "enabled": false
          },
          "vendorid": {
            "enabled": true,
            "vendorid": "vendor"
          }
        }
      },
      "devices": {
        "disks": [
          {
            "name": "disk0",
            "volumeName": "volume0",
            "disk": {
              "bus": "virtio"
            }
          },
          {
            "name": "cdrom0",
            "volumeName": "volume1",
            "cdrom": {
              "bus": "virtio",
              "readonly": true,
              "tray": "open"
            }
          },
          {
            "name": "floppy0",
            "volumeName": "volume2",
            "floppy": {
              "readonly": true,
              "tray": "open"
            }
          },
          {
            "name": "lun0",
            "volumeName": "volume3",
            "lun": {
              "bus": "virtio",
              "readonly": true
            }
          }
        ],
        "interfaces": [
          {
            "name": "default",
            {{.InterfaceConfig}}
          }
        ]
      }
    },
    "volumes": [
      {
        "name": "volume0",
        "registryDisk": {
          "image": "test/image"
        }
      },
      {
        "name": "volume1",
        "cloudInitNoCloud": {
          "secretRef": {
            "name": "testsecret"
          }
        }
      },
      {
        "name": "volume2",
        "persistentVolumeClaim": {
          "claimName": "testclaim"
        }
      }
    ],
    "networks": [
      {
        "name": "default",
        {{.NetworkConfig}}
      }
    ]
  },
  "status": {}
}`

var _ = Describe("Schema", func() {
	//The example domain should stay in sync to the json above
	var exampleVM *VirtualMachine

	BeforeEach(func() {
		exampleVM = NewMinimalVM("testvm")
		exampleVM.Spec.Domain.Devices.Disks = []Disk{
			{
				Name:       "disk0",
				VolumeName: "volume0",
				DiskDevice: DiskDevice{
					Disk: &DiskTarget{
						Bus:      "virtio",
						ReadOnly: false,
					},
				},
			},
			{
				Name:       "cdrom0",
				VolumeName: "volume1",
				DiskDevice: DiskDevice{
					CDRom: &CDRomTarget{
						Bus:      "virtio",
						ReadOnly: _true,
						Tray:     "open",
					},
				},
			},
			{
				Name:       "floppy0",
				VolumeName: "volume2",
				DiskDevice: DiskDevice{
					Floppy: &FloppyTarget{
						ReadOnly: true,
						Tray:     "open",
					},
				},
			},
			{
				Name:       "lun0",
				VolumeName: "volume3",
				DiskDevice: DiskDevice{
					LUN: &LunTarget{
						Bus:      "virtio",
						ReadOnly: true,
					},
				},
			},
		}

		exampleVM.Spec.Volumes = []Volume{
			{
				Name: "volume0",
				VolumeSource: VolumeSource{
					RegistryDisk: &RegistryDiskSource{
						Image: "test/image",
					},
				},
			},
			{
				Name: "volume1",
				VolumeSource: VolumeSource{
					CloudInitNoCloud: &CloudInitNoCloudSource{
						UserDataSecretRef: &v1.LocalObjectReference{
							Name: "testsecret",
						},
					},
				},
			},
			{
				Name: "volume2",
				VolumeSource: VolumeSource{
					PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
						ClaimName: "testclaim",
					},
				},
			},
		}
		exampleVM.Spec.Domain.Features = &Features{
			ACPI: FeatureState{Enabled: _false},
			APIC: &FeatureAPIC{Enabled: _true},
			Hyperv: &FeatureHyperv{
				Relaxed:    &FeatureState{Enabled: _true},
				VAPIC:      &FeatureState{Enabled: _false},
				Spinlocks:  &FeatureSpinlocks{Enabled: _true},
				VPIndex:    &FeatureState{Enabled: _false},
				Runtime:    &FeatureState{Enabled: _true},
				SyNIC:      &FeatureState{Enabled: _false},
				SyNICTimer: &FeatureState{Enabled: _true},
				Reset:      &FeatureState{Enabled: _false},
				VendorID:   &FeatureVendorID{Enabled: _true, VendorID: "vendor"},
			},
		}
		exampleVM.Spec.Domain.Clock = &Clock{
			ClockOffset: ClockOffset{
				UTC: &ClockOffsetUTC{},
			},
			Timer: &Timer{
				HPET:   &HPETTimer{},
				KVM:    &KVMTimer{},
				PIT:    &PITTimer{},
				RTC:    &RTCTimer{},
				Hyperv: &HypervTimer{},
			},
		}
		exampleVM.Spec.Domain.Firmware = &Firmware{
			UUID: "28a42a60-44ef-4428-9c10-1a6aee94627f",
		}
		exampleVM.Spec.Domain.CPU = &CPU{
			Cores: 3,
		}

		SetObjectDefaults_VirtualMachine(exampleVM)
	})
	Context("With example schema in json use pod network and bridge interface", func() {
		It("Unmarshal json into struct ", func() {
			exampleVM.Spec.Domain.Devices.Interfaces = []Interface{
				Interface{
					Name: "default",
					InterfaceBindingMethod: InterfaceBindingMethod{
						Bridge: &InterfaceBridge{},
					},
				},
			}
			exampleVM.Spec.Networks = []Network{
				Network{
					Name: "default",
					NetworkSource: NetworkSource{
						Pod: &PodNetwork{},
					},
				},
			}

			networkTemplateData := NetworkTemplateConfig{NetworkConfig: `"pod": {}`, InterfaceConfig: `"bridge": {}`}
			tmpl, err := template.New("vmexample").Parse(exampleJSON)
			Expect(err).To(BeNil())
			var tpl bytes.Buffer
			err = tmpl.Execute(&tpl, networkTemplateData)
			Expect(err).To(BeNil())
			newVM := &VirtualMachine{}
			exampleJSONParsed := tpl.String()
			err = json.Unmarshal([]byte(exampleJSONParsed), newVM)
			Expect(err).To(BeNil())
			Expect(newVM).To(Equal(exampleVM))
		})
		It("Marshal struct into json", func() {
			exampleVM.Spec.Domain.Devices.Interfaces = []Interface{
				Interface{
					Name: "default",
					InterfaceBindingMethod: InterfaceBindingMethod{
						Bridge: &InterfaceBridge{},
					},
				},
			}
			exampleVM.Spec.Networks = []Network{
				Network{
					Name: "default",
					NetworkSource: NetworkSource{
						Pod: &PodNetwork{},
					},
				},
			}

			networkTemplateData := NetworkTemplateConfig{NetworkConfig: `"pod": {}`, InterfaceConfig: `"bridge": {}`}
			tmpl, err := template.New("vmexample").Parse(exampleJSON)
			Expect(err).To(BeNil())
			var tpl bytes.Buffer
			err = tmpl.Execute(&tpl, networkTemplateData)
			Expect(err).To(BeNil())
			exampleJSONParsed := tpl.String()
			buf, err := json.MarshalIndent(*exampleVM, "", "  ")
			Expect(err).To(BeNil())
			Expect(string(buf)).To(Equal(exampleJSONParsed))
		})
	})
	Context("With example schema in json use proxy network and slirp interface", func() {
		It("Unmarshal json into struct", func() {
			exampleVM.Spec.Domain.Devices.Interfaces = []Interface{
				Interface{
					Name: "default",
					InterfaceBindingMethod: InterfaceBindingMethod{
						Slirp: &InterfaceSlirp{},
					},
				},
			}
			exampleVM.Spec.Networks = []Network{
				Network{
					Name: "default",
					NetworkSource: NetworkSource{
						Proxy: &ProxyNetwork{},
					},
				},
			}

			networkTemplateData := NetworkTemplateConfig{NetworkConfig: `"proxy": {}`, InterfaceConfig: `"slirp": {}`}
			tmpl, err := template.New("vmexample").Parse(exampleJSON)
			Expect(err).To(BeNil())

			var tpl bytes.Buffer
			err = tmpl.Execute(&tpl, networkTemplateData)
			Expect(err).To(BeNil())

			newVM := &VirtualMachine{}
			newVM.Spec.Domain.Devices.Interfaces = []Interface{
				Interface{
					Name: "default",
					InterfaceBindingMethod: InterfaceBindingMethod{
						Slirp: &InterfaceSlirp{},
					},
				},
			}
			newVM.Spec.Networks = []Network{
				Network{
					Name: "default",
					NetworkSource: NetworkSource{
						Proxy: &ProxyNetwork{},
					},
				},
			}

			exampleJSONParsed := tpl.String()
			err = json.Unmarshal([]byte(exampleJSONParsed), newVM)
			Expect(err).To(BeNil())
			Expect(newVM).To(Equal(exampleVM))
		})
		It("Marshal struct into json", func() {
			exampleVM.Spec.Domain.Devices.Interfaces = []Interface{
				Interface{
					Name: "default",
					InterfaceBindingMethod: InterfaceBindingMethod{
						Slirp: &InterfaceSlirp{},
					},
				},
			}
			exampleVM.Spec.Networks = []Network{
				Network{
					Name: "default",
					NetworkSource: NetworkSource{
						Proxy: &ProxyNetwork{},
					},
				},
			}

			networkTemplateData := NetworkTemplateConfig{NetworkConfig: `"proxy": {}`, InterfaceConfig: `"slirp": {}`}
			tmpl, err := template.New("vmexample").Parse(exampleJSON)
			Expect(err).To(BeNil())
			var tpl bytes.Buffer
			err = tmpl.Execute(&tpl, networkTemplateData)
			Expect(err).To(BeNil())
			exampleJSONParsed := tpl.String()
			buf, err := json.MarshalIndent(*exampleVM, "", "  ")
			Expect(err).To(BeNil())
			Expect(string(buf)).To(Equal(exampleJSONParsed))
		})
	})
})

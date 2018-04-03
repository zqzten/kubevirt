package emptydisk

import (
	"os"
	"os/exec"
	"path"
	"strconv"

	"kubevirt.io/kubevirt/pkg/api/v1"
)

var EmptyDiskBaseDir = "/var/run/libvirt/empty-disks/"

func CreateTemporaryDisks(vm *v1.VirtualMachine) error {

	for _, volume := range vm.Spec.Volumes {

		if volume.EmptyDisk != nil {
			// qemu-img takes the size in bytes or in Kibibytes/Mebibytes/...; lets take bytes
			size := strconv.FormatInt(volume.EmptyDisk.Capacity.ToDec().ScaledValue(0), 10)
			file := FilePathForVolumeName(volume.Name)
			if err := os.MkdirAll(EmptyDiskBaseDir, 0777); err != nil {
				return err
			}
			if _, err := os.Stat(file); os.IsNotExist(err) {
				if err := exec.Command("qemu-img", "create", "-f", "qcow2", file, size).Run(); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		}
	}

	return nil
}

func FilePathForVolumeName(volumeName string) string {
	return path.Join(EmptyDiskBaseDir, volumeName+".qcow2")
}

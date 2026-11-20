// Package vref names Nebla's volumes in Proxmox and recognises them again.
package vref

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// OwnerVMID is the reserved VM ID that owns every volume's disk.
	OwnerVMID = 999999999
	// MaxPerServer is how many volumes one server can hold.
	MaxPerServer = 14

	namePrefix = "vm-999999999-nb-vol-"
	serialPre  = "nb-vol-"
)

// Name is the Proxmox volume name for volume id.
func Name(id int64) string { return namePrefix + strconv.FormatInt(id, 10) }

// Serial is the virtio serial for volume id.
func Serial(id int64) string { return serialPre + strconv.FormatInt(id, 10) }

// ConfigValue is the virtioN config value that attaches volume id on storage.
func ConfigValue(storage string, id int64) string {
	return fmt.Sprintf("%s:%s,serial=%s,backup=0", storage, Name(id), Serial(id))
}

// LinuxDevice is the stable guest path of volume id.
func LinuxDevice(id int64) string { return "/dev/disk/by-id/virtio-" + Serial(id) }

// Parse extracts the volume ID from a volid or config value; only the exact Nebla name matches.
func Parse(volidOrValue string) (int64, bool) {
	v, _, _ := strings.Cut(volidOrValue, ",")
	if i := strings.IndexByte(v, ':'); i >= 0 {
		v = v[i+1:]
	}
	digits, ok := strings.CutPrefix(v, namePrefix)
	if !ok || digits == "" || digits[0] == '0' {
		return 0, false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	id, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// Keys lists the virtio config keys volumes may use; virtio0 is left to the boot disk.
func Keys() []string {
	k := make([]string, 0, 15)
	for i := 1; i <= 15; i++ {
		k = append(k, "virtio"+strconv.Itoa(i))
	}
	return k
}

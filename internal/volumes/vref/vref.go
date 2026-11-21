// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

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
	// fileDir is the owner subdirectory file storages put the disk in; fileExt its raw format suffix.
	fileDir = "999999999/"
	fileExt = ".raw"
)

// Name is the Proxmox volume name for volume id.
func Name(id int64) string { return namePrefix + strconv.FormatInt(id, 10) }

// Filename is the name allocated on storage; file storages need the raw extension.
func Filename(id int64, file bool) string {
	if file {
		return Name(id) + fileExt
	}
	return Name(id)
}

// Volid is the volume ID Proxmox gives volume id's disk on storage.
func Volid(storage string, id int64, file bool) string {
	if file {
		return storage + ":" + fileDir + Filename(id, true)
	}
	return storage + ":" + Name(id)
}

// IsFileStorage reports whether a storage type keeps disks as files, which need a format extension.
func IsFileStorage(typ string) bool {
	switch typ {
	case "dir", "nfs", "cifs", "glusterfs", "btrfs", "cephfs":
		return true
	}
	return false
}

// Serial is the virtio serial for volume id.
func Serial(id int64) string { return serialPre + strconv.FormatInt(id, 10) }

// ConfigValue is the virtioN config value that attaches volume id's disk volid.
func ConfigValue(volid string, id int64) string {
	return fmt.Sprintf("%s,serial=%s,backup=0", volid, Serial(id))
}

// Matches compares the disk reference in a VM config value to the recorded volume ID.
func Matches(value, volid string) bool {
	ref, _, _ := strings.Cut(value, ",")
	return volid != "" && ref == volid
}

// LinuxDevice is the stable guest path of volume id.
func LinuxDevice(id int64) string { return "/dev/disk/by-id/virtio-" + Serial(id) }

// Parse extracts the volume ID from a volid or config value; only the exact Nebla block or raw file name matches.
func Parse(volidOrValue string) (int64, bool) {
	v, _, _ := strings.Cut(volidOrValue, ",")
	if i := strings.IndexByte(v, ':'); i >= 0 {
		v = v[i+1:]
	}
	if rest, ok := strings.CutPrefix(v, fileDir); ok {
		if v, ok = strings.CutSuffix(rest, fileExt); !ok {
			return 0, false
		}
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

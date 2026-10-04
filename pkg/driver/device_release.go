// Copyright 2021 Synology Inc.

package driver

import (
	"fmt"
	"sort"
	"strings"

	log "github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
	"k8s.io/mount-utils"
)

// mountInfoPath lists the mounts of the driver's own mount namespace. It is a
// variable so tests can point it at a fixture.
var mountInfoPath = "/proc/self/mountinfo"

// releaseDevice makes sure no filesystem on devPath is still mounted in the
// driver's mount namespace before the device is disconnected.
//
// NodeUnstageVolume runs only after kubelet has unpublished the volume, and
// the staging path has been unmounted by then, so any mount of the device left
// in this namespace is stale. The usual source is a copy inherited through the
// host root mount when the node plugin started: it keeps the filesystem alive
// after the staging path is gone, and disconnecting the device under it fails
// in-flight writes and aborts the journal. Unmount what is left, and refuse to
// go on if anything remains.
func (ns *nodeServer) releaseDevice(devPath string) error {
	var st unix.Stat_t
	if err := unix.Stat(devPath, &st); err != nil {
		return fmt.Errorf("failed to stat device %s: %v", devPath, err)
	}
	return ns.releaseDeviceNumber(devPath, unix.Major(uint64(st.Rdev)), unix.Minor(uint64(st.Rdev)))
}

func (ns *nodeServer) releaseDeviceNumber(devPath string, major, minor uint32) error {
	mountPoints, err := deviceMountPoints(major, minor)
	if err != nil {
		return err
	}
	if len(mountPoints) == 0 {
		return nil
	}

	// Reverse order puts a nested mount point before the one it sits under.
	sort.Sort(sort.Reverse(sort.StringSlice(mountPoints)))
	for _, mountPoint := range mountPoints {
		log.Warnf("Device %s is still mounted at %s after unstage, unmounting it", devPath, mountPoint)
		if err := ns.Mounter.Interface.Unmount(mountPoint); err != nil {
			log.Errorf("Failed to unmount %s: %v", mountPoint, err)
		}
	}

	if mountPoints, err = deviceMountPoints(major, minor); err != nil {
		return err
	}
	if len(mountPoints) > 0 {
		return fmt.Errorf("device %s is still mounted at %s", devPath, strings.Join(mountPoints, ", "))
	}
	return nil
}

// deviceMountPoints returns where filesystems on the given block device are
// mounted in the driver's mount namespace.
func deviceMountPoints(major, minor uint32) ([]string, error) {
	infos, err := mount.ParseMountInfo(mountInfoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %v", mountInfoPath, err)
	}

	var mountPoints []string
	for _, info := range infos {
		if info.Major == int(major) && info.Minor == int(minor) {
			mountPoints = append(mountPoints, info.MountPoint)
		}
	}
	return mountPoints, nil
}

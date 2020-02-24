// +build linux

/*
Copyright 2014 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package fs

import (
	"fmt"

	"golang.org/x/sys/unix"
	"github.com/google/cadvisor/fs"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/kubernetes/pkg/volume/util/fsquota"
)

// FSInfo linux returns (available bytes, byte capacity, byte usage, total inodes, inodes free, inode usage, error)
// for the filesystem that path resides upon.
func FsInfo(path string) (int64, int64, int64, int64, int64, int64, error) {
	statfs := &unix.Statfs_t{}
	err := unix.Statfs(path, statfs)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}

	// Available is blocks available * fragment size
	available := int64(statfs.Bavail) * int64(statfs.Bsize)

	// Capacity is total block count * fragment size
	capacity := int64(statfs.Blocks) * int64(statfs.Bsize)

	// Usage is block being used * fragment size (aka block size).
	usage := (int64(statfs.Blocks) - int64(statfs.Bfree)) * int64(statfs.Bsize)

	inodes := int64(statfs.Files)
	inodesFree := int64(statfs.Ffree)
	inodesUsed := inodes - inodesFree

	return available, capacity, usage, inodes, inodesFree, inodesUsed, nil
}

// DirUsage gets disk and inode usage of specified path.
func DirUsage(path string) (*resource.Quantity, *resource.Quantity, error) {
	if path == "" {
    	return nil, nil, fmt.Errorf("invalid directory")
	}
	// First check whether the quota system knows about this directory
    // A nil quantity with no error means that the path does not support quotas
    // and we should use other mechanisms.
    quotaDisk, err := fsquota.GetConsumption(path)
    if err != nil {
        return nil, nil, fmt.Errorf("unable to retrieve disk consumption via quota for %s: %v", path, err)
    }
    if path == "" {
	    return nil, nil, fmt.Errorf("invalid directory")
    }
    quotaInodes, err := fsquota.GetInodes(path)
    if err != nil {
        return nil, nil, fmt.Errorf("unable to retrieve inode consumption via quota for %s: %v", path, err)
    }
    // nil values indicate that the path does not support quotas
    if quotaDisk != nil && quotaInodes != nil {
    	return quotaDisk, quotaInodes, nil
    }

    // Fall back to walking the directory tree if the path does not support quotas
	usageInfo, err := fs.GetDirUsage(path)
	if err != nil {
		return nil, nil, err
	}
	disk := resource.NewQuantity(int64(usageInfo.Bytes), resource.BinarySI)
	inodes := resource.NewQuantity(int64(usageInfo.Inodes), resource.DecimalSI)
	return disk, inodes, nil
}

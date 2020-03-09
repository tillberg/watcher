// +build !windows

package watcher

import (
	"os"
	"syscall"
)

func getDirInode(stat os.FileInfo) uint64 {
	statT, ok := stat.Sys().(*syscall.Stat_t)
	if !ok {
		panic("Failed to coerce FileInfo.Sys to *syscall.Stat_t")
	}
	return statT.Ino
}

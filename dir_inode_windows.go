// +build windows

package watcher

import (
	"os"
	"sync/atomic"
)

var nextInode uint64

func getDirInode(stat os.FileInfo) uint64 {
	return atomic.AddUint64(&nextInode, 1)
}

package watcher

import (
	"bytes"
	"crypto/sha1"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"time"

	fsnotify "gopkg.in/fsnotify/fsnotify.v1"

	"github.com/tillberg/alog"
	"github.com/tillberg/stringset"
)

func WatchPath(path string) (<-chan PathEvent, error) {
	l := NewListener()
	l.Path = path
	err := l.Start()
	if err != nil {
		return nil, err
	}
	return l.NotifyChan, nil
}

func WatchPaths(paths ...string) (<-chan PathEvent, error) {
	mainChan := make(chan PathEvent)
	subChans := make([]reflect.SelectCase, len(paths))
	for i, path := range paths {
		l := NewListener()
		l.Path = path
		err := l.Start()
		if err != nil {
			return nil, err
		}
		subChans[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(l.NotifyChan)}
	}
	go func() {
		for {
			_, value, _ := reflect.Select(subChans)
			mainChan <- value.Interface().(PathEvent)
		}
	}()
	return mainChan, nil
}

func getExePath(pathish string) (string, error) {
	if filepath.IsAbs(pathish) {
		fileInfo, err := os.Stat(pathish)
		if err != nil && !fileInfo.IsDir() {
			return pathish, nil
		}
	}
	path, err := exec.LookPath(pathish)
	if err != nil {
		Log.Printf("Failed to resolve path to %s: %v", pathish, err)
		return "", err
	} else if !filepath.IsAbs(path) {
		absPath, err := filepath.Abs(path)
		if err != nil {
			Log.Printf("Failed to resolve absolute path to %s: %s", path, err)
			return "", err
		}
		path = absPath
	}
	return filepath.Clean(path), nil
}

func WatchExecutable(pathish string, alsoPoll bool) (<-chan string, error) {
	exePath, err := getExePath(pathish)
	if err != nil {
		return nil, err
	}
	notify, err := WatchPath(exePath)
	if err != nil {
		return nil, err
	}
	pollEvents := make(chan struct{})
	debounced := make(chan string)
	go func() {
		pathsChanged := stringset.New()
		for {
			if pathsChanged.Len() > 0 {
				select {
				case pe := <-notify:
					// ignore attribute-only change events:
					if pe.Op&(^fsnotify.Chmod) != 0 {
						pathsChanged.Add(pe.Path)
					}
				case <-pollEvents:
					pathsChanged.Add(exePath)
				case <-time.After(400 * time.Millisecond):
					for _, p := range pathsChanged.All() {
						debounced <- p
					}
					pathsChanged.Clear()
				}
			} else {
				select {
				case pe := <-notify:
					// ignore attribute-only change events:
					if pe.Op&(^fsnotify.Chmod) != 0 {
						pathsChanged.Add(pe.Path)
					}
				case <-pollEvents:
					pathsChanged.Add(exePath)
				}
			}
		}
	}()
	if alsoPoll {
		go func() {
			info, err := os.Stat(exePath)
			for err != nil {
				alog.Printf("failed to stat %s: %v\n", exePath, err)
				time.Sleep(1 * time.Minute)
				info, err = os.Stat(exePath)
			}
			origModTime := info.ModTime().Unix()
			origSize := info.Size()
			origSHA1 := calcFileSHA1(exePath)
			Log.Printf("WatchExecutable %s initial modtime %d size %d\n", exePath, origModTime, origSize)
			for {
				time.Sleep(time.Duration(30+rand.Intn(30)) * time.Second)
				info2, err := os.Stat(exePath)
				if err != nil {
					Log.Printf("failed to stat %s: %v\n", exePath, err)
				} else {
					modTime := info2.ModTime().Unix()
					size := info2.Size()
					Log.Printf("WatchExecutable %s modtime %d size %d\n", exePath, modTime, size)
					if origModTime != modTime {
						newSHA1 := calcFileSHA1(exePath)
						if !bytes.Equal(origSHA1, newSHA1) {
							Log.Printf("WatchExecutable %s modtime changed from %d to %d; confirmed by hash change\n", exePath, origModTime, modTime)
							pollEvents <- struct{}{}
							origModTime = modTime
						}
					}
					if origSize != size {
						Log.Printf("WatchExecutable %s size changed from %d to %d\n", exePath, origSize, size)
						pollEvents <- struct{}{}
						origSize = size
					}
				}
			}
		}()
	}
	return debounced, nil
}

func calcFileSHA1(path string) []byte {
	h := sha1.New()
	f, err := os.Open(path)
	defer f.Close()
	if err == nil {
		io.Copy(h, f)
	}
	return h.Sum(nil)[:]
}

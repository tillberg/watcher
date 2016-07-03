package watcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"time"

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

func WatchExecutable(pathish string) (<-chan string, error) {
	exePath, err := getExePath(pathish)
	if err != nil {
		return nil, err
	}
	notify, err := WatchPath(exePath)
	if err != nil {
		return nil, err
	}
	debounced := make(chan string)
	go func() {
		pathsChanged := stringset.New()
		for {
			if pathsChanged.Len() > 0 {
				select {
				case pe := <-notify:
					pathsChanged.Add(pe.Path)
					break
				case <-time.After(400 * time.Millisecond):
					for _, p := range pathsChanged.All() {
						debounced <- p
					}
					pathsChanged.Clear()
				}
			} else {
				pathsChanged.Add((<-notify).Path)
			}
		}
	}()
	return debounced, nil
}

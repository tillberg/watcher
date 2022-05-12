package watcher

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tillberg/alog"
	"gopkg.in/fsnotify/fsnotify.v1"
)

var listeners = []*Listener{}
var watchedDirs = map[uint64]string{}

var watcher *fsnotify.Watcher
var mutex sync.Mutex

func addListener(l *Listener, dir string) error {
	mutex.Lock()
	listeners = append(listeners, l)
	mutex.Unlock()
	listenToDir(dir)
	return nil
}

func listenForUpdates(watcher *fsnotify.Watcher) {
	for {
		select {
		case err := <-watcher.Errors:
			alog.Info("[watcher] @(error:Watcher error: %s)", err)
		case ev := <-watcher.Events:
			if strings.HasSuffix(ev.Name, ".nsynctmp") {
				continue
			}
			mutex.Lock()
			_listeners := listeners
			mutex.Unlock()
			for _, l := range _listeners {
				l.Notify(PathEvent{
					Path: ev.Name,
					Op:   ev.Op,
				})
			}
			// XXX filter which newly-created directories we watch based on the Ignored and Recursive
			// settings of existing filters
			if ev.Op&fsnotify.Create != 0 {
				listenToDir(ev.Name)
			}
		}
	}
}

func listenToDir(path string) error {
	stat, err := os.Lstat(path)
	if err != nil {
		alog.Info("[watcher] @(warn:Failed to Lstat) @(cyan:%s) @(warn:in listenToDir)", path)
		return err
	}
	if !stat.IsDir() {
		// log.Println("[watcher] Not watching non-directory", path)
		return nil
	}

	dirInode := getDirInode(stat)
	mutex.Lock()
	if watchedDirs[dirInode] != path {
		watchedDirs[dirInode] = path
		// log.Println("[watcher] Watching directory", path)
		err := watcher.Add(path)
		if err != nil {
			mutex.Unlock()
			alog.Info("[watcher] @(warn:Failed to start filesystem watcher on %s: %s)", path, err)
			return err
		}
	}
	mutex.Unlock()

	srcEntries, err := ioutil.ReadDir(path)
	if err != nil {
		alog.Info("[watcher] @(warn:Error reading directory) @(cyan:%s) @(warn:in listenToDir)", path)
		return err
	}
	mutex.Lock()
	_listeners := listeners
	mutex.Unlock()
	pathNotifies := make([][]PathEvent, len(_listeners))
	for _, entry := range srcEntries {
		name := entry.Name()
		subpath := filepath.Join(path, name)
		listenToSubPath := false
		for i, l := range _listeners {
			if l.IsWatched(subpath) {
				if entry.IsDir() {
					listenToSubPath = true
				} else if l.NotifyOnStartup {
					pathNotifies[i] = append(pathNotifies[i], PathEvent{
						Path:           subpath,
						IsStartupEvent: true,
					})
				}
			}
		}
		if listenToSubPath {
			listenToDir(subpath)
		}
	}
	go func() {
		for i, l := range _listeners {
			if l.NotifyDirectoriesOnStartup {
				l.Notify(PathEvent{
					Path:           path,
					IsStartupEvent: true,
				})
			}
			for _, pe := range pathNotifies[i] {
				l.Notify(pe)
			}
		}
	}()
	return nil
}

func ensureWatcher() error {
	mutex.Lock()
	defer mutex.Unlock()
	if watcher == nil {
		var err error
		watcher, err = fsnotify.NewWatcher()
		if err != nil {
			alog.Info("[watcher] @(warn:Failed to initialize gopkg.in/fsnotify/fsnotify.v1 watcher: %s)", err)
			return err
		}
		go listenForUpdates(watcher)
	}
	return nil
}

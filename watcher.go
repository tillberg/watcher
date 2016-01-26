package watcher

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/tillberg/ansi-log"
	"gopkg.in/fsnotify.v1"
)

var Log = alog.New(os.Stderr, "@(dim:[watcher]) ", 0)

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
			Log.Printf("@(error:Watcher error: %s)\n", err)
		case ev := <-watcher.Events:
			// Log.Println("change", ev.Name, ev)
			mutex.Lock()
			_listeners := listeners
			mutex.Unlock()
			for _, l := range _listeners {
				l.Notify(ev.Name)
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
		Log.Printf("@(warn:Failed to Lstat) @(cyan:%s) @(warn:in listenToDir)\n", path)
		return err
	}
	if !stat.IsDir() {
		// Log.Println("Not watching non-directory", path)
		return nil
	}

	statT, ok := stat.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("Failed to coerce FileInfo.Sys to *syscall.Stat_t; watcher not implemented for non-linux environments.")
	}
	mutex.Lock()
	if watchedDirs[statT.Ino] != path {
		watchedDirs[statT.Ino] = path
		// Log.Println("Watching directory", path)
		err := watcher.Add(path)
		if err != nil {
			mutex.Unlock()
			Log.Printf("Failed to start filesystem watcher on %s: %s", path, err)
			return err
		}
	}
	mutex.Unlock()

	srcEntries, err := ioutil.ReadDir(path)
	if err != nil {
		Log.Printf("@(warn:Error reading directory) @(cyan:%s) @(warn:in listenToDir)\n", path)
		return err
	}
	mutex.Lock()
	_listeners := listeners
	mutex.Unlock()
	pathNotifies := make([][]string, len(_listeners))
	go func() {
		for _, l := range _listeners {
			if l.NotifyDirectoriesOnStartup {
				l.Notify(path)
			}
		}
	}()
	for _, entry := range srcEntries {
		name := entry.Name()
		subpath := filepath.Join(path, name)
		listenToSubPath := false
		for i, l := range _listeners {
			if l.IsWatched(subpath) {
				if entry.IsDir() {
					listenToSubPath = true
				} else if l.NotifyOnStartup {
					pathNotifies[i] = append(pathNotifies[i], subpath)
				}
			}
		}
		if listenToSubPath {
			listenToDir(subpath)
		}
	}
	go func() {
		for i, l := range _listeners {
			for _, p := range pathNotifies[i] {
				l.Notify(p)
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
			Log.Printf("Failed to initialize gopkg.in/fsnotify.v1 watcher: %s", err)
			return err
		}
		go listenForUpdates(watcher)
	}
	return nil
}

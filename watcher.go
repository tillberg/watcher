package watcher

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"github.com/tillberg/ansi-log"
	"github.com/tillberg/stringset"
	"gopkg.in/fsnotify.v1"
)

var Log = alog.New(os.Stderr, "@(dim:[watcher]) ", 0)

var listeners = []*Listener{}
var watchedDirs = stringset.New()

var watcher *fsnotify.Watcher
var mutex sync.Mutex

func listenForUpdates(watcher *fsnotify.Watcher) {
	for {
		select {
		case err := <-watcher.Errors:
			Log.Printf("Watcher error: %s", err)
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
				listenToDir(ev.Name, nil)
			}
		}
	}
}

func listenToDir(path string, listener *Listener) error {
	stat, err := os.Lstat(path)
	if err != nil {
		Log.Println("@(warn:Failed to Lstat @(cyan:%s) @(warn:in listenToDir)\n", path)
		return err
	}
	if !stat.IsDir() {
		// Log.Println("Not watching non-directory", path)
		return nil
	}

	mutex.Lock()
	if !watchedDirs.Has(path) {
		watchedDirs.Add(path)
		Log.Println("Watching directory", path)
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
		Log.Println("@(warn:Error reading directory) @(cyan:%s) @(warn:in listenToDir)\n", path)
		return err
	}
	pathNotifies := []string{}
	for _, entry := range srcEntries {
		name := entry.Name()
		subpath := filepath.Join(path, name)
		// XXX when called from listenForUpdates, there is no explicit listener, so we just blindly
		// recurse into everything
		if listener == nil {
			listenToDir(subpath, listener)
			continue
		}
		if listener.Ignored != nil && listener.Ignored.Has(name) {
			continue
		}
		if entry.IsDir() {
			if listener.Recursive {
				listenToDir(subpath, listener)
			}
		} else if listener.NotifyOnStartup {
			pathNotifies = append(pathNotifies, subpath)
		}
	}
	if len(pathNotifies) > 0 {
		go func() {
			for _, p := range pathNotifies {
				listener.Notify(p)
			}
		}()
	}
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

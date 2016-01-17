package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tillberg/util/stringset"
)

var PathSeparator = string(filepath.Separator)

type Listener struct {
	NotifyChan       chan string
	Ignored          *stringset.StringSet
	Recursive        bool
	NotifyOnStartup  bool
	Path             string
	DebounceDuration time.Duration

	pathIsFile         bool
	debounceNotifyChan chan string
}

func NewListener() *Listener {
	return &Listener{
		NotifyChan: make(chan string),
		Recursive:  true,
	}
}

func (l *Listener) Start() error {
	err := ensureWatcher()
	if err != nil {
		return err
	}
	// Figure out whether Path is a file or a directory. We need to *watch* a directory,
	// even if we only want notifications for a specific file inside it.
	dir := l.Path
	fileInfo, err := os.Stat(l.Path)
	if err != nil {
		return err
	}
	l.pathIsFile = !fileInfo.IsDir()
	if l.pathIsFile {
		dir = filepath.Dir(l.Path)
	} else {
		if !strings.HasSuffix(l.Path, PathSeparator) {
			l.Path += PathSeparator
		}
	}
	if l.DebounceDuration != 0 {
		l.debounceNotifyChan = make(chan string)
		go l.debounceNotify()
	}
	mutex.Lock()
	listeners = append(listeners, l)
	mutex.Unlock()
	err = listenToDir(dir, l)
	if err != nil {
		return err
	}
	return nil
}

func (l *Listener) debounceNotify() {
	neverChan := make(<-chan time.Time)
	updated := make(map[string]bool)
	for {
		timeoutChan := neverChan
		if len(updated) > 0 {
			timeoutChan = time.After(l.DebounceDuration)
		}
		select {
		case p := <-l.debounceNotifyChan:
			updated[p] = true
		case <-timeoutChan:
			for p, _ := range updated {
				l.NotifyChan <- p
			}
			updated = make(map[string]bool)
		}
	}
}

func (l *Listener) IsWatched(path string) bool {
	if l.pathIsFile {
		return path == l.Path
	} else {
		return strings.HasPrefix(path, l.Path)
	}
}

func (l *Listener) Notify(path string) {
	if l.IsWatched(path) {
		if l.DebounceDuration == 0 {
			l.NotifyChan <- path
		} else {
			l.debounceNotifyChan <- path
		}
	}
}

package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tillberg/stringset"
)

var PathSeparator = string(filepath.Separator)

type Listener struct {
	NotifyChan                 chan string
	IgnorePart                 *stringset.StringSet
	IgnoreSuffix               []string
	IgnoreSubstring            []string
	Recursive                  bool
	NotifyOnStartup            bool
	NotifyDirectoriesOnStartup bool
	Path                       string
	DebounceDuration           time.Duration

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
	// Figure out whether Path is a file or a directory. We need to watch the directory,
	// even if we only want notifications for a specific file inside it.
	l.Path = filepath.Clean(l.Path)
	dir := l.Path
	fileInfo, err := os.Stat(l.Path)
	if err != nil {
		return err
	}
	l.pathIsFile = !fileInfo.IsDir()
	if l.pathIsFile {
		dir = filepath.Dir(l.Path)
	}
	if l.DebounceDuration != 0 {
		l.debounceNotifyChan = make(chan string)
		go l.debounceNotify()
	}
	err = addListener(l, dir)
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

func matches(path string, parts *stringset.StringSet, suffixes, substrings []string) bool {
	if parts != nil {
		for _, part := range strings.Split(path, PathSeparator) {
			if parts.Has(part) {
				return true
			}
		}
	}
	for _, suffix := range suffixes {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	wrappedRel := path + PathSeparator
	for _, str := range substrings {
		if strings.Contains(wrappedRel, str) {
			return true
		}
	}
	return false
}

func (l *Listener) IsWatched(path string) bool {
	if l.pathIsFile {
		return path == l.Path
	} else {
		if !strings.HasPrefix(path, l.Path) {
			return false
		}
		return !matches(path, l.IgnorePart, l.IgnoreSuffix, l.IgnoreSubstring)
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

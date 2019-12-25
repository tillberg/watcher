package watcher

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	fsnotify "gopkg.in/fsnotify/fsnotify.v1"

	"github.com/tillberg/stringset"
)

type PathEvent struct {
	Path           string
	IsStartupEvent bool
	Op             fsnotify.Op
}

var PathSeparator = string(filepath.Separator)

type Listener struct {
	NotifyChan                 chan PathEvent
	IgnorePart                 *stringset.StringSet
	IgnoreSuffix               []string
	IgnoreSubstring            []string
	Recursive                  bool
	NotifyOnStartup            bool
	NotifyDirectoriesOnStartup bool
	Path                       string
	DebounceDuration           time.Duration

	pathIsFile         bool
	debounceNotifyChan chan PathEvent
	slowNotifyChan     chan PathEvent
}

func NewListener() *Listener {
	return &Listener{
		NotifyChan: make(chan PathEvent, 20),
		Recursive:  true,
	}
}

func (l *Listener) Start() error {
	if l.DebounceDuration != 0 {
		l.debounceNotifyChan = make(chan PathEvent, 20)
		go l.debounceNotify()
	}
	if l.NotifyOnStartup || l.NotifyDirectoriesOnStartup {
		l.slowNotifyChan = make(chan PathEvent, 20)
		go l.slowNotifyStartupEvents()
	}
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
	// Add a trailing slash to handle the case that the root folder is a symlink to a folder.
	// We don't want to follow symlinks elsewhere, but we do want to follow them at the root.
	if !strings.HasSuffix(dir, "/") {
		dir += "/"
	}
	err = addListener(l, dir)
	if err != nil {
		return err
	}
	return nil
}

func (l *Listener) debounceNotify() {
	updated := make(map[PathEvent]fsnotify.Op)
	for {
		var timeoutChan <-chan time.Time
		if len(updated) > 0 {
			timeoutChan = time.After(l.DebounceDuration)
		}
		select {
		case pe := <-l.debounceNotifyChan:
			// Debounce all ops together, bit-merging ops together:
			op := pe.Op
			pe.Op = 0
			updated[pe] |= op
		case <-timeoutChan:
			for pe, op := range updated {
				pe.Op = op
				l.NotifyChan <- pe
			}
			updated = make(map[PathEvent]fsnotify.Op)
		}
	}
}

func (l *Listener) slowNotifyStartupEvents() {
	// NOTE this logic depends on NotifyChan being a *buffered* channel
	var events []PathEvent
	var forwardCheckDelay time.Duration
	for {
		if len(events) == 0 {
			event := <-l.slowNotifyChan
			events = append(events, event)
			continue
		}
		if len(l.NotifyChan) <= cap(l.NotifyChan)/2 {
			select {
			case event := <-l.slowNotifyChan:
				events = append(events, event)
			case l.NotifyChan <- events[0]:
				events = events[1:]
				forwardCheckDelay = time.Millisecond
			}
			continue
		}
		select {
		case event := <-l.slowNotifyChan:
			events = append(events, event)
		case <-time.After(forwardCheckDelay):
			forwardCheckDelay = (3 * forwardCheckDelay) / 2
			if forwardCheckDelay > time.Second {
				forwardCheckDelay = time.Second
			}
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
	wrappedRel := "^"
	if !strings.HasPrefix(path, "/") {
		wrappedRel += PathSeparator
	}
	wrappedRel += path
	// XXX This should really only be added for directories:
	if !strings.HasSuffix(path, "/") {
		wrappedRel += PathSeparator
	}
	wrappedRel += "$"
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
		relPath, err := filepath.Rel(l.Path, path)
		if err != nil {
			log.Printf("[watcher] @(error:Error getting relative path from %q to %q: %v)\n", l.Path, path)
			relPath = path
		}
		return !matches(relPath, l.IgnorePart, l.IgnoreSuffix, l.IgnoreSubstring)
	}
}

func (l *Listener) Notify(pathEvent PathEvent) {
	if l.IsWatched(pathEvent.Path) {
		if pathEvent.IsStartupEvent {
			l.slowNotifyChan <- pathEvent
		} else if l.DebounceDuration == 0 {
			l.NotifyChan <- pathEvent
		} else {
			l.debounceNotifyChan <- pathEvent
		}
	}
}

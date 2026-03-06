package ui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/nahime0/lazyclaude/internal/claude"
)

// fileWatchMsg is sent when any JSONL file in ~/.claude/projects changes.
type fileWatchMsg struct{}

// projectWatcher watches ~/.claude/projects for JSONL changes using FSEvents.
// It debounces rapid writes so that a burst of JSONL lines triggers one reload.
type projectWatcher struct {
	fw     *fsnotify.Watcher
	Events <-chan struct{}
}

// newProjectWatcher starts an FSEvents watcher on ~/.claude/projects.
// Returns nil (no error) if the directory doesn't exist yet.
func newProjectWatcher() (*projectWatcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	projectsDir := claude.ClaudeProjectsDir()
	if projectsDir == "" {
		fw.Close()
		return nil, nil
	}

	// Watch the projects dir itself (catches new project subdirs being created).
	if err := fw.Add(projectsDir); err != nil {
		fw.Close()
		return nil, err
	}

	// Watch all existing project subdirectories.
	entries, _ := os.ReadDir(projectsDir)
	for _, e := range entries {
		if e.IsDir() {
			_ = fw.Add(filepath.Join(projectsDir, e.Name()))
		}
	}

	ch := make(chan struct{}, 1)
	w := &projectWatcher{fw: fw, Events: ch}
	go w.run(projectsDir, ch)
	return w, nil
}

func (w *projectWatcher) run(projectsDir string, out chan<- struct{}) {
	defer w.fw.Close()

	var timer *time.Timer
	notify := func() {
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(200*time.Millisecond, func() {
			select {
			case out <- struct{}{}:
			default:
			}
		})
	}

	for {
		select {
		case event, ok := <-w.fw.Events:
			if !ok {
				return
			}
			// New subdirectory created (new project) → watch it immediately.
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					_ = w.fw.Add(event.Name)
				}
			}
			// Only reload when a JSONL file is touched.
			if strings.HasSuffix(event.Name, ".jsonl") {
				notify()
			}
		case _, ok := <-w.fw.Errors:
			if !ok {
				return
			}
		}
	}
}

// watchCmd returns a tea.Cmd that blocks until the next file change event.
// Must be re-armed after each fileWatchMsg.
func watchCmd(events <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-events
		return fileWatchMsg{}
	}
}

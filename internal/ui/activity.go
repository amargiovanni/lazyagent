package ui

import (
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/nahime0/lazyclaude/internal/claude"
)

// ActivityTimeout is the duration a tool-based activity stays visible after
// the tool finishes, unless replaced by a newer activity.
const ActivityTimeout = 30 * time.Second

// WaitingTimeout is how long "waiting" stays visible before falling back to idle.
const WaitingTimeout = 2 * time.Minute

// ActivityKind is the human-readable label shown in the session list.
type ActivityKind string

const (
	ActivityIdle      ActivityKind = "idle"
	ActivityWaiting   ActivityKind = "waiting"   // Claude finished, awaiting user input
	ActivityThinking  ActivityKind = "thinking"  // Claude generating a response
	ActivityReading   ActivityKind = "reading"   // Read
	ActivityWriting   ActivityKind = "writing"   // Write / Edit
	ActivityRunning   ActivityKind = "running"   // Bash
	ActivitySearching ActivityKind = "searching" // Glob / Grep
	ActivityBrowsing  ActivityKind = "browsing"  // WebFetch / WebSearch
	ActivitySpawning  ActivityKind = "spawning"  // Agent (subagent)
)

// activityColors maps each activity kind to a display color.
var activityColors = map[ActivityKind]lipgloss.Color{
	ActivityIdle:      colorMuted,
	ActivityWaiting:   lipgloss.Color("#4ADE80"), // green
	ActivityThinking:  colorWarning,              // amber
	ActivityReading:   lipgloss.Color("#38BDF8"), // sky blue
	ActivityWriting:   lipgloss.Color("#FB923C"), // orange
	ActivityRunning:   lipgloss.Color("#A78BFA"), // violet
	ActivitySearching: lipgloss.Color("#34D399"), // emerald
	ActivityBrowsing:  lipgloss.Color("#22D3EE"), // cyan
	ActivitySpawning:  lipgloss.Color("#F472B6"), // pink
}

// activityEntry holds a session's current sticky activity state.
type activityEntry struct {
	kind     ActivityKind
	lastSeen time.Time // last time this kind was confirmed from JSONL
}

// resolveActivity determines the display activity for a session.
//
// Priority:
//  1. StatusWaitingForUser within WaitingTimeout (2m) → "waiting".
//     Uses its own longer timeout since Claude finished and the user hasn't replied yet.
//  2. LastActivity older than ActivityTimeout (30s) → idle.
//  3. Most recent tool in RecentTools within ActivityTimeout → show that tool activity.
//     Handles tool executions that complete between polls.
//  4. Current JSONL status → thinking if Claude is processing, idle otherwise.
func resolveActivity(s *claude.Session, now time.Time) ActivityKind {
	sinceActivity := now.Sub(s.LastActivity)

	// Claude finished responding and is waiting for user input.
	if s.Status == claude.StatusWaitingForUser {
		if !s.LastActivity.IsZero() && sinceActivity < WaitingTimeout {
			return ActivityWaiting
		}
		return ActivityIdle
	}

	// Gate on LastActivity for all other states.
	if s.LastActivity.IsZero() || sinceActivity > ActivityTimeout {
		return ActivityIdle
	}

	// Most recent tool within timeout takes priority.
	if len(s.RecentTools) > 0 {
		last := s.RecentTools[len(s.RecentTools)-1]
		if !last.Timestamp.IsZero() && now.Sub(last.Timestamp) < ActivityTimeout {
			return toolActivity(last.Name)
		}
	}

	// Active but no recent tool — use JSONL status.
	switch s.Status {
	case claude.StatusThinking, claude.StatusProcessingResult:
		return ActivityThinking
	case claude.StatusExecutingTool:
		return toolActivity(s.CurrentTool)
	}
	return ActivityIdle
}

// toolActivity maps a Claude tool name to an activity kind.
func toolActivity(tool string) ActivityKind {
	switch tool {
	case "Read":
		return ActivityReading
	case "Write", "Edit", "NotebookEdit":
		return ActivityWriting
	case "Bash":
		return ActivityRunning
	case "Glob", "Grep":
		return ActivitySearching
	case "WebFetch", "WebSearch":
		return ActivityBrowsing
	case "Agent":
		return ActivitySpawning
	default:
		if tool != "" {
			return ActivityRunning // unknown tools treated as running
		}
		return ActivityIdle
	}
}

// updateActivities resolves and stores the current activity for each session.
// The timeout logic lives in resolveActivity (via RecentTools timestamps),
// so this is a straightforward map update.
func (m *Model) updateActivities(now time.Time) {
	if m.activities == nil {
		m.activities = make(map[string]*activityEntry)
	}
	for _, s := range m.sessions {
		id := s.SessionID
		if id == "" {
			continue
		}
		m.activities[id] = &activityEntry{kind: resolveActivity(s, now), lastSeen: now}
	}
}

// activityFor returns the current sticky activity for a session.
func (m Model) activityFor(sessionID string) ActivityKind {
	if e, ok := m.activities[sessionID]; ok {
		return e.kind
	}
	return ActivityIdle
}

// renderActivity returns a styled activity label for use in the list row.
func renderActivity(kind ActivityKind) string {
	color, ok := activityColors[kind]
	if !ok {
		color = colorMuted
	}
	return lipgloss.NewStyle().Foreground(color).Bold(true).Render(string(kind))
}

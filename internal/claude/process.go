package claude

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ProcessInfo holds OS-level info about a running Claude process.
type ProcessInfo struct {
	PID         int
	CWD         string
	Args        string
	IsDangerous bool
}

// FindClaudeProcesses returns all running Claude Code processes on macOS.
// Uses pgrep -lf to avoid issues with ps aliases and spaces in paths.
func FindClaudeProcesses() ([]ProcessInfo, error) {
	out, err := exec.Command("pgrep", "-lf", "claude").Output()
	if err != nil {
		// exit code 1 means no matches — not an error
		return nil, nil
	}

	selfPID := os.Getpid()
	var procs []ProcessInfo

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		if pid == selfPID {
			continue
		}
		// fields[1] is the executable path (or bare name).
		// Only match actual claude binaries, not wrappers (disclaimer, ShipIt, etc.)
		if filepath.Base(fields[1]) != "claude" {
			continue
		}
		args := strings.Join(fields[1:], " ")
		cwd := getCWD(pid)
		procs = append(procs, ProcessInfo{
			PID:         pid,
			CWD:         cwd,
			Args:        args,
			IsDangerous: strings.Contains(args, "dangerously-skip-permissions"),
		})
	}
	return procs, nil
}

// getCWD returns the current working directory of a process via lsof.
// The -a flag is required to AND the filters (-p AND -d cwd), otherwise lsof
// returns all files for all processes matching any criterion.
func getCWD(pid int) string {
	out, err := exec.Command("lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n") {
			return strings.TrimPrefix(line, "n")
		}
	}
	return ""
}

// EnrichWithProcessInfo matches sessions to running processes by CWD.
func EnrichWithProcessInfo(sessions []*Session, procs []ProcessInfo) {
	cwdToPID := make(map[string]ProcessInfo)
	for _, p := range procs {
		if p.CWD != "" {
			cwdToPID[p.CWD] = p
		}
	}
	for _, s := range sessions {
		if p, ok := cwdToPID[s.CWD]; ok {
			s.PID = p.PID
			s.IsDangerous = p.IsDangerous
		}
	}
}

// IsWorktree detects if a path is a git worktree and returns the main repo.
func IsWorktree(path string) (bool, string) {
	out, err := exec.Command("git", "-C", path, "rev-parse", "--git-dir").Output()
	if err != nil {
		return false, ""
	}
	gitDir := strings.TrimSpace(string(out))

	// In a regular repo: .git
	// In a worktree: absolute path like /repo/.git/worktrees/name
	if filepath.Base(gitDir) == ".git" || !filepath.IsAbs(gitDir) {
		return false, ""
	}

	// It's a worktree — find the main repo
	// gitDir looks like /path/to/main/.git/worktrees/branch-name
	parts := strings.Split(gitDir, string(os.PathSeparator))
	for i, p := range parts {
		if p == ".git" && i+1 < len(parts) && parts[i+1] == "worktrees" {
			mainRepo := filepath.Join(parts[:i]...)
			return true, "/" + mainRepo
		}
	}
	return true, ""
}

// ClaudeProjectsDir returns the path to ~/.claude/projects.
func ClaudeProjectsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// ProjectDirForCWD encodes a CWD path to the ~/.claude/projects directory name.
// Claude encodes paths by replacing / with -.
func ProjectDirForCWD(cwd string) string {
	// Replace leading / and all / with -
	encoded := strings.TrimPrefix(cwd, "/")
	encoded = strings.ReplaceAll(encoded, "/", "-")
	return encoded
}

// DiscoverSessions scans ~/.claude/projects for JSONL session files.
func DiscoverSessions() ([]*Session, error) {
	projectsDir := ClaudeProjectsDir()
	if projectsDir == "" {
		return nil, fmt.Errorf("could not find home directory")
	}

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, fmt.Errorf("could not read projects dir: %w", err)
	}

	var sessions []*Session
	for _, projectEntry := range entries {
		if !projectEntry.IsDir() {
			continue
		}
		projectPath := filepath.Join(projectsDir, projectEntry.Name())
		jsonlFiles, err := filepath.Glob(filepath.Join(projectPath, "*.jsonl"))
		if err != nil || len(jsonlFiles) == 0 {
			continue
		}
		// Each JSONL file is one session — take the most recent
		latestFile := mostRecentFile(jsonlFiles)
		if latestFile == "" {
			continue
		}
		session, err := ParseJSONL(latestFile)
		if err != nil {
			continue
		}
		// If CWD is empty, derive from directory name
		if session.CWD == "" {
			session.CWD = decodeDirName(projectEntry.Name())
		}
		isWT, mainRepo := IsWorktree(session.CWD)
		session.IsWorktree = isWT
		session.MainRepo = mainRepo
		sessions = append(sessions, session)
	}
	return sessions, nil
}

func mostRecentFile(files []string) string {
	var latest string
	var latestMod int64
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		if info.ModTime().Unix() > latestMod {
			latestMod = info.ModTime().Unix()
			latest = f
		}
	}
	return latest
}

func decodeDirName(name string) string {
	// Reverse of ProjectDirForCWD: dashes → slashes, prepend /
	// This is a best-effort heuristic
	return "/" + strings.ReplaceAll(name, "-", "/")
}

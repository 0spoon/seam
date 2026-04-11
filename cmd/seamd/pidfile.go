package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// pidLock is an advisory exclusive lock on a pid file. It uses flock(2)
// so the kernel releases the lock automatically if seamd crashes or is
// killed uncleanly -- a stale pid file on disk doesn't block a restart
// the way a raw O_EXCL check would.
//
// Only one seamd can hold the lock at a time. The second one gets a
// clear error pointing at the currently running process and the
// shortest path to stop it.
type pidLock struct {
	path string
	f    *os.File
}

// acquirePIDLock opens path (creating it if needed) and takes an
// exclusive, non-blocking flock on the file descriptor. On success, it
// truncates the file and writes the current PID. On failure, it reads
// whatever PID is in the existing file and returns an error that names
// the holder and the recovery commands.
func acquirePIDLock(path string) (*pidLock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open pid file %s: %w", path, err)
	}

	// LOCK_NB makes flock return immediately if another process holds
	// the lock instead of blocking. In that case the realistic error
	// is EWOULDBLOCK; any other failure (EBADF, EINVAL) would be a
	// programmer error on a fd we just opened, so treat every flock
	// failure as "already running" and help the user recover.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		msg := lockHeldMessage(path, f)
		_ = f.Close()
		return nil, msg
	}

	// We hold the lock. Replace the file's contents with our PID.
	if err := f.Truncate(0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("truncate pid file: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("seek pid file: %w", err)
	}
	if _, err := fmt.Fprintf(f, "%d\n", os.Getpid()); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("write pid: %w", err)
	}

	return &pidLock{path: path, f: f}, nil
}

// release closes the pid file (dropping the flock) and removes it so a
// subsequent startup sees a clean slate. Safe to call multiple times.
func (p *pidLock) release() {
	if p == nil || p.f == nil {
		return
	}
	_ = p.f.Close()
	p.f = nil
	_ = os.Remove(p.path)
}

// lockHeldMessage produces a human-readable error that identifies the
// seamd holding the lock and points at the command that will stop it.
// We check the holder's parent PID so we can give a precise hint:
// parent 1 means launchd/systemd owns it, otherwise it's an orphan
// from `make run` or a direct invocation.
func lockHeldMessage(path string, f *os.File) error {
	// Re-read whatever PID was written by the other instance. We seek
	// to 0 because the fd cursor advanced past the PID in some edge
	// cases (shouldn't happen here, but belt-and-suspenders).
	_, _ = f.Seek(0, 0)
	buf := make([]byte, 64)
	n, _ := f.Read(buf)
	pidStr := strings.TrimSpace(string(buf[:n]))

	pid, parseErr := strconv.Atoi(pidStr)
	if parseErr != nil || pid <= 0 {
		return fmt.Errorf(
			"seamd already running (pid file %s is locked)\n"+
				"  Stop a supervised service:  make service-stop\n"+
				"  Stop an orphan listener:    make kill-stale",
			path,
		)
	}

	if supervisedParent(pid) {
		return fmt.Errorf(
			"seamd already running (pid %d, under launchd/systemd)\n"+
				"  Stop it with:  make service-stop",
			pid,
		)
	}
	return fmt.Errorf(
		"seamd already running (pid %d, not under a service manager)\n"+
			"  Stop it with:  make kill-stale   (or kill %d)",
		pid, pid,
	)
}

// supervisedParent reports whether the given PID's parent is PID 1.
// On macOS that's launchd; on Linux it's systemd (for user services)
// or init. We shell out to `ps` because /proc is Linux-only and the
// project targets both platforms. Any failure falls back to "not
// supervised" so the caller still gets an actionable error.
func supervisedParent(pid int) bool {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "ppid=").Output()
	if err != nil {
		return false
	}
	ppid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return false
	}
	return ppid == 1
}

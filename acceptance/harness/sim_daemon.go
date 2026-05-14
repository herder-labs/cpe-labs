//go:build acceptance

package harness

import (
	"bytes"
	"context"
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"
)

// SimProcess is a running cpe-sim subprocess. Stop sends SIGTERM and
// waits for clean exit; safe to call multiple times. The process is
// also stopped automatically via t.Cleanup so scenarios don't have to
// remember.
type SimProcess struct {
	cmd  *exec.Cmd
	out  *bytes.Buffer
	once sync.Once
	done chan struct{}
	err  error
}

// LaunchSimDaemon starts cpe-sim as a background subprocess and
// returns a SimProcess the caller stops at test cleanup. Use this for
// USP (daemon-mode) scenarios and any CWMP scenario that needs the
// simulator alive while the test drives it.
//
// The fixture's --profile and --acs-url are pre-supplied; extraArgs
// are appended verbatim (e.g. "--seed=1"). The process inherits a
// captured stdout+stderr buffer; on test failure or Stop the buffer
// is dumped via t.Logf for diagnosis.
func LaunchSimDaemon(t *testing.T, fix *Fixture, extraArgs ...string) *SimProcess {
	t.Helper()

	args := append([]string{
		"--profile=" + fix.ProfilePath,
		"--acs-url=" + fix.ACS.URL,
		"--log-level=error",
	}, extraArgs...)

	out := &bytes.Buffer{}
	cmd := exec.Command(fix.BinPath, args...)
	cmd.Stdout = out
	cmd.Stderr = out
	// New process group so we can SIGTERM the simulator without
	// catching child goroutines or signal handlers oddly.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		t.Fatalf("LaunchSimDaemon: start cpe-sim: %v", err)
	}

	p := &SimProcess{cmd: cmd, out: out, done: make(chan struct{})}
	go func() {
		p.err = cmd.Wait()
		close(p.done)
	}()

	t.Cleanup(func() { p.stopInternal(t) })
	return p
}

// Stop sends SIGTERM and waits up to 5s for the process to exit. Safe
// to call multiple times. On a non-zero exit (other than the expected
// signal-induced exit), logs the captured output via t.Logf.
func (p *SimProcess) Stop(t *testing.T) {
	t.Helper()
	p.stopInternal(t)
}

func (p *SimProcess) stopInternal(t *testing.T) {
	p.once.Do(func() {
		// SIGTERM the whole process group so cpe-sim's signal handler
		// triggers its clean shutdown path.
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)

		select {
		case <-p.done:
		case <-time.After(5 * time.Second):
			t.Logf("SimProcess.Stop: cpe-sim did not exit within 5s; SIGKILL")
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
			<-p.done
		}

		if p.err != nil {
			// Distinguish signal-induced exit (expected) from real failure.
			if ee, ok := p.err.(*exec.ExitError); ok {
				if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
					return
				}
			}
			t.Logf("SimProcess: cpe-sim exited %v; output:\n%s", p.err, p.out.String())
		}
	})
}

// Output returns the captured stdout+stderr accumulated so far.
// Reads are not synchronized with the wait goroutine; call after Stop
// (or use t.Cleanup's deferred dump) for the full record.
func (p *SimProcess) Output() string {
	if p.out == nil {
		return ""
	}
	return p.out.String()
}

// blockUntilDoneOrCtx waits for the process to exit or the context
// to cancel, whichever happens first. Not currently used by the
// public surface; reserved for scenarios that need to assert a clean
// exit before running further checks.
func (p *SimProcess) blockUntilDoneOrCtx(ctx context.Context) error {
	select {
	case <-p.done:
		return p.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

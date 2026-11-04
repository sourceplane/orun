package preview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/fslock"
)

// DefaultTimeout bounds how long Start waits for each component's port.
const DefaultTimeout = 120 * time.Second

// stopGrace is how long a process group has to exit on SIGTERM.
const stopGrace = 5 * time.Second

// Options shapes one Start.
type Options struct {
	// Timeout bounds the wait for each component's port (DefaultTimeout).
	Timeout time.Duration
	// Environ is the environment processes inherit, before ChildEnv strips
	// it (os.Environ()).
	Environ []string
}

// Start runs the plan's components in order, each detached in a session of
// its own and logging to LogPath, and waits for each one's port to accept
// connections on 127.0.0.1 before starting the next. A preview already
// running healthy for the same plan is left alone. On failure every process
// it started is stopped and the returned state, also on disk, says why.
func Start(ctx context.Context, plan *Plan, opts Options) (State, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.Environ == nil {
		opts.Environ = os.Environ()
	}
	root := plan.Root
	if err := ensureDir(root); err != nil {
		return State{}, err
	}
	lock, err := fslock.Acquire(ctx, lockPath(root))
	if err != nil {
		return State{}, fmt.Errorf("lock %s: %w", lockPath(root), err)
	}
	defer lock.Release()

	if prev, err := ReadState(root); err == nil && prev.State != StateStopped {
		if prev.State != StateFailed && samePlan(prev, plan) {
			if cur := inspect(root, prev); cur.State == StateReady {
				if prev.State != StateReady || prev.Detail != "" {
					_ = writeState(root, cur)
				}
				return cur, nil
			}
		}
		// What is left of a run that died, or of another declaration.
		stopProcesses(prev.Processes)
	}

	target := plan.Target()
	st := State{
		Component: target.Name, Port: target.Port, Path: target.Path,
		State: StateStarting, Processes: []Process{},
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	fail := func(detail string) (State, error) {
		stopProcesses(st.Processes)
		st.State, st.Detail = StateFailed, detail
		_ = writeState(root, st)
		return st, errors.New(detail)
	}
	for _, c := range plan.Components {
		if portAnswers(c.Port) {
			return fail(fmt.Sprintf("port %d is already in use, not by this preview", c.Port))
		}
	}
	if err := writeState(root, st); err != nil {
		return State{}, err
	}
	for _, c := range plan.Components {
		pid, exited, err := launch(root, c, opts.Environ)
		if err != nil {
			return fail(fmt.Sprintf("%s: %v", c.Name, err))
		}
		st.Processes = append(st.Processes, Process{Component: c.Name, PID: pid, Port: c.Port})
		if err := writeState(root, st); err != nil {
			return fail(err.Error())
		}
		if detail := waitForPort(ctx, root, c, exited, opts.Timeout); detail != "" {
			return fail(detail)
		}
	}
	st.State = StateReady
	if err := writeState(root, st); err != nil {
		return st, err
	}
	return st, nil
}

// Status reads the state and checks it against the processes and ports: a
// process that has exited makes it failed; one alive but not answering makes
// it starting. What it finds is written back when no Start or Stop holds the
// state.
func Status(root string) (State, error) {
	st, err := ReadState(root)
	if err != nil {
		return st, err
	}
	cur := inspect(root, st)
	if cur.State != st.State || cur.Detail != st.Detail {
		if lock, ok, _ := fslock.TryAcquire(lockPath(root)); ok {
			_ = writeState(root, cur)
			lock.Release()
		}
	}
	return cur, nil
}

// Stop ends every process of the preview, the shown one first, and marks it
// stopped.
func Stop(root string) (State, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	lock, err := fslock.Acquire(ctx, lockPath(root))
	if err != nil {
		return State{}, fmt.Errorf("lock %s: %w", lockPath(root), err)
	}
	defer lock.Release()
	st, err := ReadState(root)
	if err != nil {
		return st, err
	}
	stopProcesses(st.Processes)
	st.State, st.Detail = StateStopped, ""
	return st, writeState(root, st)
}

// samePlan reports whether a recorded preview runs exactly the plan's
// components on the plan's ports.
func samePlan(st State, plan *Plan) bool {
	if len(st.Processes) != len(plan.Components) || st.Component != plan.Target().Name {
		return false
	}
	for i, c := range plan.Components {
		if st.Processes[i].Component != c.Name || st.Processes[i].Port != c.Port {
			return false
		}
	}
	return true
}

// inspect re-checks a recorded starting or ready preview.
func inspect(root string, st State) State {
	if st.State != StateStarting && st.State != StateReady {
		return st
	}
	out := st
	out.Detail = ""
	for _, p := range st.Processes {
		if !alive(p.PID) {
			out.State = StateFailed
			out.Detail = exitedDetail(root, p.Component, "")
			return out
		}
	}
	for _, p := range st.Processes {
		if !portAnswers(p.Port) {
			out.State = StateStarting
			out.Detail = fmt.Sprintf("%s is running but 127.0.0.1:%d does not answer", p.Component, p.Port)
			return out
		}
	}
	if len(st.Processes) > 0 {
		out.State = StateReady
	}
	return out
}

// launch starts one component and returns its pid and a channel that
// receives its exit. The process is reaped here, so a long-lived caller
// (serve) never sees a zombie as alive.
func launch(root string, c Component, environ []string) (int, <-chan error, error) {
	if err := ensureDir(root); err != nil {
		return 0, nil, err
	}
	logf, err := os.OpenFile(LogPath(root, c.Name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_APPEND, 0o644)
	if err != nil {
		return 0, nil, err
	}
	defer logf.Close()
	fmt.Fprintf(logf, "orun preview: %s — %s (in %s, port %d)\n", c.Name, strings.Join(c.Run, " "), c.Dir, c.Port)
	cmd := exec.Command(c.Run[0], c.Run[1:]...)
	cmd.Dir = c.Dir
	cmd.Env = ChildEnv(environ, c.Env, c.Port)
	cmd.Stdin = nil
	cmd.Stdout = logf
	cmd.Stderr = logf
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return 0, nil, err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	return cmd.Process.Pid, exited, nil
}

// waitForPort waits until c's port answers; "" when it does, else why not.
func waitForPort(ctx context.Context, root string, c Component, exited <-chan error, timeout time.Duration) string {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		if portAnswers(c.Port) {
			return ""
		}
		select {
		case <-ctx.Done():
			return fmt.Sprintf("%s: canceled before 127.0.0.1:%d answered", c.Name, c.Port)
		case err := <-exited:
			status := "exited"
			if err != nil {
				status = "exited (" + err.Error() + ")"
			}
			return exitedDetail(root, c.Name, status)
		case <-deadline.C:
			return fmt.Sprintf("%s did not answer on 127.0.0.1:%d within %s (is it listening on 0.0.0.0 or 127.0.0.1?)", c.Name, c.Port, timeout)
		case <-tick.C:
		}
	}
}

// exitedDetail is the one line a failed preview reports: what exited and
// the last thing it logged.
func exitedDetail(root, component, status string) string {
	if status == "" {
		status = "exited"
	}
	d := component + " " + status
	if last := lastLogLine(LogPath(root, component)); last != "" {
		d += ": " + last
	}
	return d
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// lastLogLine is the last non-empty line of a log, plain and short.
func lastLogLine(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	const tail = 4096
	if info, err := f.Stat(); err == nil && info.Size() > tail {
		_, _ = f.Seek(info.Size()-tail, io.SeekStart)
	}
	b, _ := io.ReadAll(f)
	lines := strings.Split(ansiEscape.ReplaceAllString(string(b), ""), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(strings.TrimRight(lines[i], "\r"))
		if l == "" || strings.HasPrefix(l, "orun preview: ") {
			continue
		}
		if len(l) > 160 {
			l = l[:160] + "…"
		}
		return l
	}
	return ""
}

// portAnswers reports whether 127.0.0.1:port accepts a TCP connection.
func portAnswers(port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// stopProcesses ends each process group, the last started first: SIGTERM,
// up to stopGrace for the leader to go, then SIGKILL for whatever is left.
func stopProcesses(procs []Process) {
	for i := len(procs) - 1; i >= 0; i-- {
		pid := procs[i].PID
		if pid <= 1 || pid == os.Getpid() || (!alive(pid) && !groupAlive(pid)) {
			continue
		}
		signalGroup(pid, false)
		deadline := time.Now().Add(stopGrace)
		for alive(pid) && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if groupAlive(pid) {
			signalGroup(pid, true)
		}
	}
}

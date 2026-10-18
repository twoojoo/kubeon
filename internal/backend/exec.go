package backend

import (
	"context"
	"errors"
	"io"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/twoojoo/kubeon/third_party/k9s/client"
)

// TermSize is a terminal size in cells.
type TermSize struct{ Cols, Rows uint16 }

// ExecSession is an interactive shell attached to a container.
type ExecSession struct {
	// Stdin never blocks the caller (the UI goroutine): input is queued and
	// fed to the stream by a goroutine, also while it is still connecting.
	Stdin  io.WriteCloser
	resize sizeQueue
	cancel context.CancelFunc
}

// Resize forwards a terminal resize; only the latest pending size is kept.
func (e *ExecSession) Resize(s TermSize) {
	e.resize.push(remotecommand.TerminalSize{Width: s.Cols, Height: s.Rows})
}

// Close terminates the session.
func (e *ExecSession) Close() {
	e.cancel()
	_ = e.Stdin.Close()
}

// sizeQueue holds at most one pending size and ends with the session.
type sizeQueue struct {
	ch  chan remotecommand.TerminalSize
	ctx context.Context
}

func (q sizeQueue) push(s remotecommand.TerminalSize) {
	for {
		select {
		case q.ch <- s:
			return
		default:
			select { // replace the stale pending size
			case <-q.ch:
			default:
			}
		}
	}
}

func (q sizeQueue) Next() *remotecommand.TerminalSize {
	select {
	case s := <-q.ch:
		return &s
	case <-q.ctx.Done():
		return nil
	}
}

// asyncWriter queues writes for a pipe; Write fails once closed or when
// the remote side stopped reading for too long (queue full).
type asyncWriter struct {
	ch   chan []byte
	done chan struct{}
	once sync.Once
}

func newAsyncWriter(w io.WriteCloser) *asyncWriter {
	a := &asyncWriter{ch: make(chan []byte, 1024), done: make(chan struct{})}
	go func() {
		defer w.Close()
		for {
			select {
			case b := <-a.ch:
				if _, err := w.Write(b); err != nil {
					return
				}
			case <-a.done:
				return
			}
		}
	}()
	return a
}

func (a *asyncWriter) Write(b []byte) (int, error) {
	select {
	case <-a.done:
		return 0, io.ErrClosedPipe
	default:
	}
	select {
	case a.ch <- append([]byte(nil), b...):
		return len(b), nil
	default:
		return 0, errors.New("terminal input queue full")
	}
}

func (a *asyncWriter) Close() error {
	a.once.Do(func() { close(a.done) })
	return nil
}

// Exec opens a TTY shell in a pod container. Output is written to out;
// done is called with the exit error when the session ends.
func (c *Cluster) Exec(path, container string, cmd []string, size TermSize, out io.Writer, done func(error)) (*ExecSession, error) {
	ns, name := client.Namespaced(path)
	cfg, err := c.conn.RestConfig()
	if err != nil {
		return nil, err
	}
	cs, err := c.conn.Dial()
	if err != nil {
		return nil, err
	}
	if len(cmd) == 0 {
		cmd = []string{"/bin/sh", "-c", "command -v bash >/dev/null && exec bash || exec sh"}
	}
	req := cs.CoreV1().RESTClient().Post().
		Resource("pods").Namespace(ns).Name(name).SubResource("exec").
		VersionedParams(&v1.PodExecOptions{
			Container: container,
			Command:   cmd,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       true,
		}, scheme.ParameterCodec)

	wsExec, err := remotecommand.NewWebSocketExecutor(cfg, "GET", req.URL().String())
	if err != nil {
		return nil, err
	}
	spdyExec, err := remotecommand.NewSPDYExecutor(cfg, "POST", req.URL())
	if err != nil {
		return nil, err
	}
	// fall back to SPDY only when the websocket upgrade itself fails (like
	// kubectl): any other error ends the session, it must not start a new shell
	exec, err := remotecommand.NewFallbackExecutor(wsExec, spdyExec, func(err error) bool {
		return httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err)
	})
	if err != nil {
		return nil, err
	}

	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	q := sizeQueue{ch: make(chan remotecommand.TerminalSize, 1), ctx: ctx}
	q.push(remotecommand.TerminalSize{Width: size.Cols, Height: size.Rows})
	s := ExecSession{Stdin: newAsyncWriter(pw), resize: q, cancel: cancel}
	go func() {
		err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdin:             pr,
			Stdout:            out,
			Tty:               true,
			TerminalSizeQueue: q,
		})
		cancel()
		_ = pr.Close()
		done(err)
	}()

	return &s, nil
}

package backend

import (
	"context"
	"io"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/giovanni/k9desk/third_party/k9s/client"
)

// TermSize is a terminal size in cells.
type TermSize struct{ Cols, Rows uint16 }

// ExecSession is an interactive shell attached to a container.
type ExecSession struct {
	Stdin   io.WriteCloser
	resize  chan remotecommand.TerminalSize
	cancel  context.CancelFunc
	stdinR  *io.PipeReader
}

// Resize forwards a terminal resize.
func (e *ExecSession) Resize(s TermSize) {
	select {
	case e.resize <- remotecommand.TerminalSize{Width: s.Cols, Height: s.Rows}:
	default:
	}
}

// Close terminates the session.
func (e *ExecSession) Close() {
	e.cancel()
	_ = e.Stdin.Close()
}

type sizeQueue chan remotecommand.TerminalSize

func (q sizeQueue) Next() *remotecommand.TerminalSize {
	s, ok := <-q
	if !ok {
		return nil
	}
	return &s
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
	exec, err := remotecommand.NewFallbackExecutor(wsExec, spdyExec, func(err error) bool {
		return true
	})
	if err != nil {
		return nil, err
	}

	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	q := make(sizeQueue, 4)
	q <- remotecommand.TerminalSize{Width: size.Cols, Height: size.Rows}
	s := ExecSession{Stdin: pw, resize: q, cancel: cancel, stdinR: pr}
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

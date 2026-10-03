// Command k9desk is a native Kubernetes IDE (Lens-like) powered by the k9s engine.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/giovanni/k9desk/internal/safety"
	"github.com/giovanni/k9desk/internal/ui"
)

func main() {
	var (
		kubeconfig = flag.String("kubeconfig", "", "path to kubeconfig (default: $KUBECONFIG or ~/.kube/config)")
		kctx       = flag.String("context", "", "kube context to open (default: current-context)")
		ns         = flag.String("namespace", "", "initial namespace (default: all)")
		resource   = flag.String("resource", "", "initial resource gvr or alias, e.g. v1/pods")
		logLevel   = flag.String("log-level", "warn", "debug|info|warn|error")
	)
	flag.Parse()

	if err := setupLog(*logLevel); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	kc, err := safety.ResolveKubeconfig(*kubeconfig)
	if err != nil {
		fmt.Fprintln(os.Stderr, "k9desk:", err)
		os.Exit(2)
	}
	if *kctx != "" {
		if err := safety.CheckContext(*kctx); err != nil {
			fmt.Fprintln(os.Stderr, "k9desk:", err)
			os.Exit(2)
		}
	}
	safety.IsolateCaches(kc)
	if safety.Dev {
		slog.Info("dev build: locked to", "kubeconfig", kc)
	}

	if err := ui.Run(ui.Options{Kubeconfig: kc, Context: *kctx, Namespace: *ns, Resource: *resource}); err != nil {
		fmt.Fprintln(os.Stderr, "k9desk:", err)
		os.Exit(1)
	}
}

// setupLog writes logs to the user cache dir, never to the terminal UI.
func setupLog(level string) error {
	var lv slog.Level
	if err := lv.UnmarshalText([]byte(level)); err != nil {
		lv = slog.LevelWarn
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	dir = filepath.Join(dir, "k9desk")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "k9desk.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: lv})))
	return nil
}

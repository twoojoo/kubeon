package backend

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContextsMergesPathList(t *testing.T) {
	dir := t.TempDir()
	cfg := func(ctx string) string {
		return "apiVersion: v1\nkind: Config\ncurrent-context: " + ctx + "\n" +
			"clusters: [{name: c-" + ctx + ", cluster: {server: 'https://127.0.0.1:1'}}]\n" +
			"users: [{name: u-" + ctx + ", user: {}}]\n" +
			"contexts: [{name: " + ctx + ", context: {cluster: c-" + ctx + ", user: u-" + ctx + "}}]\n"
	}
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	_ = os.WriteFile(a, []byte(cfg("first")), 0o600)
	_ = os.WriteFile(b, []byte(cfg("second")), 0o600)
	names, cur, err := Contexts(a + string(filepath.ListSeparator) + b + string(filepath.ListSeparator) + filepath.Join(dir, "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || cur != "first" {
		t.Fatalf("names %v current %q", names, cur)
	}
}

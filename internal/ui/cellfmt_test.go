package ui

import "testing"

func TestFmtCell(t *testing.T) {
	cases := []struct {
		col, in, want string
		dim           bool
	}{
		{"CPU", "250", "250m", false},
		{"CPU", "0", "0m", true},
		{"CPU/A", "1000", "1 core", false},
		{"CPU", "2500", "2.5 cores", false},
		{"MEM", "512", "512 MiB", false},
		{"MEM/A", "1536", "1.5 GiB", false},
		{"MEM", "n/a", "—", true},
		{"CPU/RL", "100:500", "100m / 500m", false},
		{"CPU/RL", "100:0", "100m / —", false},
		{"CPU/RL", "0:0", "—", true},
		{"MEM/RL", "128:2048", "128 MiB / 2 GiB", false},
		{"GPU/RL", "1:1", "1 / 1", false},
		{"%CPU/L", "42", "42%", false},
		{"%MEM", "n/a", "—", true},
		{"RESTARTS", "0", "0", true},
		{"RESTARTS", "3", "3", false},
		{"CAPACITY", "10Gi", "10 GiB", false},
		{"CAPACITY", "500M", "500 MB", false},
		{"CAPACITY", "", "", false},
		{"SELECTOR", "<none>", "—", true},
		{"CPU", "1.5", "1.5", false}, // custom column with the same name: untouched
	}
	for _, c := range cases {
		got, dim := fmtCell(colKind(c.col), c.in)
		if got != c.want || dim != c.dim {
			t.Errorf("%s %q: got %q,%v want %q,%v", c.col, c.in, got, dim, c.want, c.dim)
		}
	}
}

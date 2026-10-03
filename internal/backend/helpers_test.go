package backend

import "testing"

func TestStripTags(t *testing.T) {
	cases := map[string]string{
		"plain":                 "plain",
		"[green::b] ✓ [::]":     "✓",
		"[orangered::b] × [::]": "×",
		"nginx:[1.2]":           "nginx:[1.2]",
	}
	for in, want := range cases {
		if got := stripTags(in); got != want {
			t.Errorf("stripTags(%q)=%q want %q", in, got, want)
		}
	}
}

func TestStripTimestamp(t *testing.T) {
	if got := stripTimestamp("2026-10-03T07:48:40.555625228Z hello world"); got != "hello world" {
		t.Fatalf("got %q", got)
	}
	if got := stripTimestamp("no timestamp here"); got != "no timestamp here" {
		t.Fatalf("got %q", got)
	}
}

func TestLabelSelector(t *testing.T) {
	if _, ok := labelSelector("-l app=web"); !ok {
		t.Fatal("expected label selector")
	}
	if _, ok := labelSelector("web"); ok {
		t.Fatal("plain text is not a label selector")
	}
}

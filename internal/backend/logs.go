package backend

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/twoojoo/kubeon/third_party/k9s/dao"
)

// batchLogs coalesces log items so the UI redraws at most ~20 times/sec.
func batchLogs(ctx context.Context, in <-chan *dao.LogItem, showTime bool, out func([]LogLine)) {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	buf := make([]LogLine, 0, 256)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		out(buf)
		buf = make([]LogLine, 0, 256)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case it := <-in:
			if it == nil || it == dao.ItemEOF || it.IsEmpty() {
				continue
			}
			txt := unescapeTview(strings.TrimRight(string(it.Bytes), "\r\n"))
			if it.IsError {
				txt = stripTags(txt)
			}
			for _, l := range strings.Split(txt, "\n") {
				if !showTime {
					l = stripTimestamp(l)
				}
				buf = append(buf, LogLine{Pod: it.Pod, Container: it.Container, Text: l, IsError: it.IsError})
			}
			if len(buf) >= 4096 {
				flush()
			}
		case <-tick.C:
			flush()
		}
	}
}

// stripTimestamp drops the RFC3339 prefix the api-server adds to log lines.
func stripTimestamp(l string) string {
	i := strings.IndexByte(l, ' ')
	if i < 20 || i > 40 {
		return l
	}
	if _, err := time.Parse(time.RFC3339Nano, l[:i]); err != nil {
		return l
	}
	return l[i+1:]
}

// k9s escapes log bytes for tview (tview.Escape: "[foo]" → "[foo[]",
// "[a[]" → "[a[[]"); this is its exact inverse.
var tviewEscRx = regexp.MustCompile(`(\[[a-zA-Z0-9_,;: \-\."#]+\[*)\[\]`)

func unescapeTview(s string) string {
	if !strings.Contains(s, "[]") {
		return s
	}
	return tviewEscRx.ReplaceAllString(s, "$1]")
}

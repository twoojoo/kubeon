package ui

import (
	"image"
	"strconv"
	"strings"

	"github.com/twoojoo/kubeon/internal/backend"
)

// cellKind tells how a table column is displayed. k9s renders raw numbers
// (millicores, MiB, bare percentages) for its terminal; the table formats
// them for display only, so sorting and filtering keep using k9s values.
type cellKind uint8

const (
	kindPlain    cellKind = iota
	kindCPU               // millicores: 250 -> 250m, 1500 -> 1.5 cores
	kindMem               // MiB: 512 -> 512 MiB, 1536 -> 1.5 GiB
	kindCPURL             // "req:lim" millicores
	kindMemRL             // "req:lim" MiB
	kindCountRL           // "req:lim" plain counts (GPUs)
	kindPct               // bare percentage, drawn with a usage bar
	kindRestarts          // restart count: 0 dimmed, >0 warned
	kindQuantity          // k8s quantity: 10Gi -> 10 GiB
)

const placeholder = "—"

func colKind(name string) cellKind {
	switch name {
	case "CPU", "CPU/A":
		return kindCPU
	case "MEM", "MEM/A":
		return kindMem
	case "CPU/RL":
		return kindCPURL
	case "MEM/RL":
		return kindMemRL
	case "GPU/RL":
		return kindCountRL
	case "%CPU", "%MEM", "%CPU/R", "%CPU/L", "%MEM/R", "%MEM/L":
		return kindPct
	case "RESTARTS":
		return kindRestarts
	case "CAPACITY":
		return kindQuantity
	}
	return kindPlain
}

// isPlaceholder reports k9s' "no value" markers.
func isPlaceholder(s string) bool {
	switch s {
	case "n/a", "<none>", "<unknown>", "<nil>":
		return true
	}
	return false
}

// fmtCell returns the display text of a cell and whether it carries no
// information (drawn dimmed). Values that don't parse as expected (custom
// columns that happen to share a name) are shown as they are.
func fmtCell(k cellKind, s string) (string, bool) {
	if isPlaceholder(s) {
		return placeholder, true
	}
	switch k {
	case kindCPU:
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return fmtMilli(n), n == 0
		}
	case kindMem:
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return fmtMiB(n), n == 0
		}
	case kindCPURL, kindMemRL, kindCountRL:
		a, b, ok := strings.Cut(s, ":")
		if !ok {
			break
		}
		ra, ea := strconv.ParseInt(a, 10, 64)
		rb, eb := strconv.ParseInt(b, 10, 64)
		if ea != nil || eb != nil {
			break
		}
		if ra == 0 && rb == 0 {
			return placeholder, true
		}
		conv := func(n int64) string { return strconv.FormatInt(n, 10) }
		switch k {
		case kindCPURL:
			conv = fmtMilli
		case kindMemRL:
			conv = fmtMiB
		}
		side := func(n int64) string {
			if n == 0 {
				return placeholder
			}
			return conv(n)
		}
		return side(ra) + " / " + side(rb), false
	case kindPct:
		if _, err := strconv.Atoi(s); err == nil {
			return s + "%", false
		}
	case kindRestarts:
		return s, s == "0"
	case kindQuantity:
		return fmtQuantity(s), false
	}
	return s, false
}

// pctValue parses a percentage cell (k9s bare integer).
func pctValue(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil && n >= 0
}

func fmtMilli(m int64) string { return backend.HumanMilli(m) }

func fmtMiB(n int64) string {
	if n == 0 {
		return "0 MiB"
	}
	return backend.HumanBytes(n << 20)
}

// fmtQuantity spells out the binary/decimal suffix of a k8s quantity
// (10Gi -> 10 GiB, 500M -> 500 MB); anything else is returned unchanged.
func fmtQuantity(s string) string {
	i := strings.IndexFunc(s, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
	if i <= 0 {
		return s
	}
	num, suf := s[:i], s[i:]
	switch suf {
	case "Ki", "Mi", "Gi", "Ti", "Pi", "Ei":
		return num + " " + suf + "B"
	case "k", "M", "G", "T", "P", "E":
		if suf == "k" {
			suf = "K"
		}
		return num + " " + suf + "B"
	}
	return s
}

// pctBar draws a thin usage meter along the bottom of a percentage cell,
// right-aligned under the text: accent below 70%, warning up to 90%, error
// above (the meter caps at 100%).
func pctBar(gtx C, raw string) {
	p, ok := pctValue(raw)
	if !ok {
		return
	}
	sz := gtx.Constraints.Max
	w, h := min(sz.X, gtx.Dp(48)), gtx.Dp(2)
	r := image.Rect(sz.X-w, sz.Y-gtx.Dp(6), sz.X, sz.Y-gtx.Dp(6)+h)
	fillRRect(gtx, withAlpha(pal.TextDim, 0x40), r, h/2)
	c := pal.Accent
	switch {
	case p >= 90:
		c = pal.Err
	case p >= 70:
		c = pal.Warn
	}
	if fw := w * min(p, 100) / 100; fw > 0 {
		r.Max.X = r.Min.X + max(fw, h)
		fillRRect(gtx, c, r, h/2)
	}
}

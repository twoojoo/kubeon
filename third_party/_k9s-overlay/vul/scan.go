// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s
// k9desk overlay: grype-free version of upstream scan.go.

package vul

import (
	"fmt"
	"io"
)

// Scans tracks scans per image.
type Scans map[string]*Scan

// Dump dumps reports to writer.
func (s Scans) Dump(w io.Writer) error {
	for k, v := range s {
		_, _ = fmt.Fprintf(w, "Image: %s -- ", k)
		v.Tally.Dump(w)
		_, _ = fmt.Fprintln(w)
		if err := v.Dump(w); err != nil {
			return err
		}
	}

	return nil
}

// Scan tracks image vulnerability scan.
type Scan struct {
	ID    string
	Table *table
	Tally tally
}

// Dump dump report to stdout.
func (s *Scan) Dump(w io.Writer) error {
	return s.Table.dump(w)
}

func colorize(rr []string) []string {
	crr := make([]string, len(rr))
	copy(crr, rr)

	crr[len(crr)-1] = sevColor(crr[len(crr)-1])
	return crr
}

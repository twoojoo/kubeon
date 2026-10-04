// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s
// kubeon overlay: image scanning is disabled; this stub keeps the API so
// dao/render compile without pulling grype/syft.

package vul

import (
	"context"
	"log/slog"

	"github.com/giovanni/kubeon/third_party/k9s/config"
)

// ImgScanner stays nil: scanning disabled.
var ImgScanner *imageScanner

type imageScanner struct{}

// NewImageScanner returns a no-op scanner.
func NewImageScanner(config.ImageScans, *slog.Logger) *imageScanner { return &imageScanner{} }

func (*imageScanner) Init(string, string)                     {}
func (*imageScanner) Stop()                                   {}
func (*imageScanner) ShouldExcludes(string, map[string]string) bool { return true }
func (*imageScanner) GetScan(string) (*Scan, bool)            { return nil, false }
func (*imageScanner) Score(...string) string                  { return "" }
func (*imageScanner) IsInitialized() bool                     { return false }
func (*imageScanner) Enqueue(context.Context, ...string)      {}

// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package dao

import (
	"context"
	"log/slog"

	"github.com/giovanni/k9desk/third_party/k9s/client"
	"github.com/giovanni/k9desk/third_party/k9s/render"
	"github.com/giovanni/k9desk/third_party/k9s/slogs"
	"k8s.io/apimachinery/pkg/runtime"
)

var (
	_ Accessor   = (*Context)(nil)
	_ Switchable = (*Context)(nil)
)

// Context represents a kubernetes context.
type Context struct {
	NonResource
}

func (c *Context) config() *client.Config {
	return c.getFactory().Client().Config()
}

// Get a Context.
func (c *Context) Get(_ context.Context, path string) (runtime.Object, error) {
	co, err := c.config().GetContext(path)
	if err != nil {
		return nil, err
	}
	return &render.NamedContext{Name: path, Context: co}, nil
}

// List all Contexts on the current cluster.
func (c *Context) List(context.Context, string) ([]runtime.Object, error) {
	ctxs, err := c.config().Contexts()
	if err != nil {
		return nil, err
	}
	cc := make([]runtime.Object, 0, len(ctxs))
	for k, v := range ctxs {
		cc = append(cc, render.NewNamedContext(c.config(), k, v))
	}

	return cc, nil
}

// MustCurrentContextName return the active context name.
func (c *Context) MustCurrentContextName() string {
	cl, err := c.config().CurrentContextName()
	if err != nil {
		slog.Error("Fetching current context", slogs.Error, err)
	}
	return cl
}

// Switch to another context.
func (c *Context) Switch(ctx string) error {
	return c.getFactory().Client().SwitchContext(ctx)
}

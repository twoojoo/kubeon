// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package model

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/giovanni/kubeon/third_party/k9s"
	"github.com/giovanni/kubeon/third_party/k9s/client"
	"github.com/giovanni/kubeon/third_party/k9s/dao"
	"github.com/giovanni/kubeon/third_party/k9s/render"
	"github.com/sahilm/fuzzy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func getMeta(ctx context.Context, gvr *client.GVR) (ResourceMeta, error) {
	meta := resourceMeta(gvr)
	factory, ok := ctx.Value(internal.KeyFactory).(dao.Factory)
	if !ok {
		return ResourceMeta{}, fmt.Errorf("expected Factory in context but got %T", ctx.Value(internal.KeyFactory))
	}
	meta.DAO.Init(factory, gvr)

	return meta, nil
}

func resourceMeta(gvr *client.GVR) ResourceMeta {
	meta, ok := Registry[gvr]
	if !ok {
		meta = ResourceMeta{
			DAO:      new(dao.Table),
			Renderer: new(render.Table),
		}
	}
	if meta.DAO == nil {
		meta.DAO = new(dao.Resource)
	}
	// kubeon: Registry entries are shared; DAO.Init and
	// Renderer.SetViewSetting mutate them, so concurrent tables on the same
	// GVR race. Hand out private copies.
	meta.DAO = cloneZero(meta.DAO)
	meta.Renderer = cloneZero(meta.Renderer)
	if meta.TreeRenderer != nil {
		meta.TreeRenderer = cloneZero(meta.TreeRenderer)
	}

	return meta
}

// cloneZero returns a fresh instance of v's concrete type: value fields are
// copied, non-nil pointer fields (e.g. an embedded *render.Base) are replaced
// by new zero values so no mutable state is shared.
func cloneZero[T any](v T) T {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return v
	}
	src := rv.Elem()
	dst := reflect.New(src.Type())
	dst.Elem().Set(src)
	for i := 0; i < src.NumField(); i++ {
		f := dst.Elem().Field(i)
		if f.Kind() == reflect.Pointer && !f.IsNil() && f.CanSet() && f.Type().Elem().Kind() == reflect.Struct {
			f.Set(reflect.New(f.Type().Elem()))
		}
	}
	out, ok := dst.Interface().(T)
	if !ok {
		return v
	}
	return out
}

// MetaFQN returns a fully qualified resource name.
func MetaFQN(m *metav1.ObjectMeta) string {
	return FQN(m.Namespace, m.Name)
}

// FQN returns a fully qualified resource name.
func FQN(ns, n string) string {
	if ns == "" {
		return n
	}
	return ns + "/" + n
}

// NewExpBackOff returns a new exponential backoff timer.
func NewExpBackOff(ctx context.Context, start, maxVal time.Duration) backoff.BackOffContext {
	bf := backoff.NewExponentialBackOff()
	bf.InitialInterval, bf.MaxElapsedTime = start, maxVal
	return backoff.WithContext(bf, ctx)
}

func rxFilter(q string, lines []string) fuzzy.Matches {
	rx, err := regexp.Compile(`(?i)` + q)
	if err != nil {
		return nil
	}

	matches := make(fuzzy.Matches, 0, len(lines))
	for i, l := range lines {
		locs := rx.FindAllStringIndex(l, -1)
		for _, loc := range locs {
			indexes := make([]int, 0, loc[1]-loc[0])
			for v := loc[0]; v < loc[1]; v++ {
				indexes = append(indexes, v)
			}

			matches = append(matches, fuzzy.Match{Str: q, Index: i, MatchedIndexes: indexes})
		}
	}

	return matches
}

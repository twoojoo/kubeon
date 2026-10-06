package backend

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestHumanQuantity(t *testing.T) {
	cases := []struct {
		r    v1.ResourceName
		q    string
		want string
	}{
		{v1.ResourceCPU, "100m", "100m"},
		{v1.ResourceCPU, "8", "8 cores"},
		{v1.ResourceCPU, "7910m", "7.91 cores"},
		{v1.ResourceMemory, "16264036Ki", "15.5 GiB"},
		{v1.ResourceMemory, "128Mi", "128 MiB"},
		{v1.ResourceEphemeralStorage, "100Gi", "100 GiB"},
		{v1.ResourceMemory, "512", "512 B"},
		{v1.ResourcePods, "110", "110"},
	}
	for _, c := range cases {
		if got := HumanQuantity(c.r, resource.MustParse(c.q)); got != c.want {
			t.Errorf("%s %s: got %q want %q", c.r, c.q, got, c.want)
		}
	}
}

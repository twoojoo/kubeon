package backend

import (
	"encoding/base64"
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/twoojoo/kubeon/third_party/k9s/client"
)

// SecretEntry is one decoded secret key.
type SecretEntry struct {
	Key    string
	Value  []byte
	Binary bool
}

// SecretData returns the decoded data of a secret, sorted by key.
func (c *Cluster) SecretData(path string) ([]SecretEntry, error) {
	o, err := c.factory.Get(client.SecGVR, path, true, labels.Everything())
	if err != nil {
		return nil, err
	}
	u, ok := o.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("unexpected %T", o)
	}
	data, _, _ := unstructured.NestedStringMap(u.Object, "data")
	out := make([]SecretEntry, 0, len(data))
	for k, v := range data {
		b, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			b = []byte(v)
		}
		out = append(out, SecretEntry{Key: k, Value: b, Binary: !printable(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func printable(b []byte) bool {
	for _, r := range string(b) {
		if r == 0xfffd || (r < 0x20 && r != '\n' && r != '\r' && r != '\t') {
			return false
		}
	}
	return true
}

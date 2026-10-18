package backend

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"github.com/twoojoo/kubeon/third_party/k9s/client"
)

// PortForward is an active local → pod port tunnel.
type PortForward struct {
	ID     string
	GVR    string // requested resource (pod, service, deployment…)
	Target string // requested path (ns/name)
	Pod    string
	Local  int
	Remote int
	stop   chan struct{}
}

func (p *PortForward) String() string {
	return fmt.Sprintf("localhost:%d → %s:%d", p.Local, p.Pod, p.Remote)
}

type forwards struct {
	mx sync.Mutex
	m  map[string]*PortForward
}

func (f *forwards) add(p *PortForward) {
	f.mx.Lock()
	defer f.mx.Unlock()
	if f.m == nil {
		f.m = map[string]*PortForward{}
	}
	f.m[p.ID] = p
}

func (f *forwards) remove(id string) *PortForward {
	f.mx.Lock()
	defer f.mx.Unlock()
	p := f.m[id]
	delete(f.m, id)
	return p
}

// PortForwards lists active forwards.
func (c *Cluster) PortForwards() []*PortForward {
	c.pf.mx.Lock()
	defer c.pf.mx.Unlock()
	out := make([]*PortForward, 0, len(c.pf.m))
	for _, p := range c.pf.m {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Local < out[j].Local })
	return out
}

// StopPortForward stops a forward by id.
func (c *Cluster) StopPortForward(id string) {
	if p := c.pf.remove(id); p != nil {
		close(p.stop)
	}
}

func (c *Cluster) stopAllForwards() {
	for _, p := range c.PortForwards() {
		c.StopPortForward(p.ID)
	}
}

// ContainerPorts returns declared container ports of a pod-backed resource.
func (c *Cluster) ContainerPorts(gvr, path string) ([]int, error) {
	pod, err := c.PodFor(gvr, path)
	if err != nil {
		return nil, err
	}
	o, err := c.factory.Get(client.PodGVR, pod, true, labels.Everything())
	if err != nil {
		return nil, err
	}
	u, ok := o.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("unexpected %T", o)
	}
	var out []int
	cc, _, _ := unstructured.NestedSlice(u.Object, "spec", "containers")
	for _, co := range cc {
		m, _ := co.(map[string]any)
		pp, _, _ := unstructured.NestedSlice(m, "ports")
		for _, p := range pp {
			pm, _ := p.(map[string]any)
			if n, ok, _ := unstructured.NestedInt64(pm, "containerPort"); ok {
				out = append(out, int(n))
			}
		}
	}
	return out, nil
}

// PortForward opens localhost:local → pod(remote). local=0 picks a free port.
func (c *Cluster) PortForward(gvr, path string, local, remote int) (*PortForward, error) {
	pod, err := c.PodFor(gvr, path)
	if err != nil {
		return nil, err
	}
	ns, name := client.Namespaced(pod)
	cfg, err := c.conn.RestConfig()
	if err != nil {
		return nil, err
	}
	cs, err := c.conn.Dial()
	if err != nil {
		return nil, err
	}
	req := cs.CoreV1().RESTClient().Post().Resource("pods").Namespace(ns).Name(name).SubResource("portforward")
	tr, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return nil, err
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: tr}, http.MethodPost, req.URL())

	stop, ready := make(chan struct{}), make(chan struct{})
	fw, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"},
		[]string{fmt.Sprintf("%d:%d", local, remote)}, stop, ready, io.Discard, io.Discard)
	if err != nil {
		return nil, err
	}
	errc := make(chan error, 1)
	go func() { errc <- fw.ForwardPorts() }()
	select {
	case err := <-errc:
		return nil, err
	case <-ready:
	}
	ports, err := fw.GetPorts()
	if err != nil || len(ports) == 0 {
		close(stop)
		return nil, fmt.Errorf("port-forward: %v", err)
	}
	p := PortForward{
		ID:     fmt.Sprintf("%s:%d:%d", pod, ports[0].Local, remote),
		GVR:    gvr,
		Target: path,
		Pod:    pod,
		Local:  int(ports[0].Local),
		Remote: remote,
		stop:   stop,
	}
	c.pf.add(&p)
	go func() {
		<-errc
		c.pf.remove(p.ID)
	}()
	return &p, nil
}

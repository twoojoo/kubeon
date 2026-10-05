package backend

import (
	"testing"

	"github.com/derailed/tview"
	v1 "k8s.io/api/core/v1"
)

func TestPodProblemStableAcrossCrashLoop(t *testing.T) {
	crashed := v1.ContainerState{Terminated: &v1.ContainerStateTerminated{ExitCode: 1, Reason: "Error"}}
	pod := func(st v1.ContainerState, ready bool, restarts int32) *v1.Pod {
		return &v1.Pod{Status: v1.PodStatus{Phase: v1.PodRunning, ContainerStatuses: []v1.ContainerStatus{{
			State: st, Ready: ready, RestartCount: restarts, LastTerminationState: crashed,
		}}}}
	}
	for name, p := range map[string]*v1.Pod{
		"back-off":   pod(v1.ContainerState{Waiting: &v1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}, false, 3),
		"running":    pod(v1.ContainerState{Running: &v1.ContainerStateRunning{}}, false, 3),
		"terminated": pod(crashed, false, 3),
	} {
		if got := podProblem(p); got != "CrashLoopBackOff" {
			t.Errorf("%s: got %q", name, got)
		}
	}
	if got := podProblem(pod(v1.ContainerState{Running: &v1.ContainerStateRunning{}}, true, 3)); got != "" {
		t.Errorf("ready again: got %q", got)
	}
	pull := &v1.Pod{Status: v1.PodStatus{ContainerStatuses: []v1.ContainerStatus{{
		State: v1.ContainerState{Waiting: &v1.ContainerStateWaiting{Reason: "ErrImagePull"}},
	}}}}
	if got := podProblem(pull); got != "ImagePullBackOff" {
		t.Errorf("image pull: got %q", got)
	}
}

func TestUnescapeTviewRoundTrip(t *testing.T) {
	for _, s := range []string{"[foo]", "[a[]", "x [INFO] y", "[]", "plain", "[a[[]", "arr[0]"} {
		if got := unescapeTview(tview.Escape(s)); got != s {
			t.Errorf("%q → %q → %q", s, tview.Escape(s), got)
		}
	}
}

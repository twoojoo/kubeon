//go:build dev && integration

package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giovanni/k9desk/internal/ai"
)

func TestUIAssistant(t *testing.T) {
	cfgDir, _ := filepath.Abs(filepath.Join("..", "..", ".dev", "aiconfig"))
	_ = os.RemoveAll(cfgDir)
	t.Setenv("K9DESK_CONFIG_DIR", cfgDir)
	model := os.Getenv("K9DESK_TEST_OLLAMA_MODEL")
	if model == "" {
		model = "llama3.1:8b"
	}
	cfg := ai.LoadConfig()
	cfg.Set("ollama", ai.ProviderConfig{Model: model})
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	h := newHarness(t, Options{Namespace: "demo", Resource: "deploy"})
	a := h.a
	selectRow(t, h, "demo/web")
	h.press("A")
	if !a.ai.open {
		t.Fatal("A should open the assistant")
	}
	h.settle(150 * time.Millisecond)
	h.shot("40-ai-empty")

	a.overlay = newAISettings(a, a.ai)
	h.settle(150 * time.Millisecond)
	h.shot("41-ai-settings")
	a.overlay = nil

	// proposal card + real approval (scale web to its current 3 replicas)
	three := int32(3)
	a.ai.add(&chatItem{kind: chatUser, text: "Scale web back to 3 replicas"})
	a.ai.add(&chatItem{kind: chatTool, tool: &ai.ToolRun{ID: "t1", Name: "describe_resource", Args: "kind=deploy name=web", Output: "Replicas: 3 desired", Duration: 420 * time.Millisecond}})
	a.ai.add(&chatItem{kind: chatAssistant, text: "The deployment **web** runs `3/3` replicas.\n\n- rollout is healthy\n- no warnings\n\n```yaml\nspec:\n  replicas: 3\n```"})
	it := a.ai.add(&chatItem{kind: chatProposal, prop: &ai.Proposal{Action: "scale", Kind: "deploy", GVR: "apps/v1/deployments", Name: "web", Namespace: "demo", Replicas: &three, Reason: "Keep 3 replicas for availability."}})
	h.settle(150 * time.Millisecond)
	h.shot("42-ai-proposal")
	a.ai.execute(it)
	h.waitFor("proposal executed", func() bool { return it.state == propDone || it.state == propFailed })
	if it.state != propDone {
		t.Fatalf("proposal failed: %s", it.result)
	}
	if len(a.ai.notes) != 1 || !strings.Contains(a.ai.notes[0], "executed") {
		t.Fatalf("notes %v", a.ai.notes)
	}

	if os.Getenv("K9DESK_TEST_OLLAMA_MODEL") == "" {
		return
	}
	a.ai.reset()
	a.ai.send("Is the crasher deployment in namespace demo healthy? Look at its pods.")
	deadline := time.Now().Add(15 * time.Minute)
	for a.ai.running && time.Now().Before(deadline) {
		h.frame()
		time.Sleep(200 * time.Millisecond)
	}
	h.settle(300 * time.Millisecond)
	h.shot("43-ai-ollama-conversation")
	tools := 0
	for _, it := range a.ai.items {
		if it.kind == chatTool {
			tools++
		}
		if it.kind == chatError {
			t.Fatalf("assistant error: %s", it.text)
		}
	}
	if tools == 0 {
		t.Fatal("no tool calls in UI conversation")
	}
}

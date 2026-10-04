# kubeon

Native Kubernetes IDE in the spirit of Lens — no Electron, no web stack.

- **UI**: [Gio](https://gioui.org) (GPU immediate-mode, pure Go, Wayland/X11).
- **Engine**: [k9s](https://github.com/derailed/k9s) v0.51.0 backend packages
  (`client`, `watch` informers, `dao`, `model`, `render`) vendored in
  `third_party/k9s` with rewritten import paths (Apache-2.0, see its LICENSE).

## Features

| Area | What |
|---|---|
| Navigation | Lens-like sidebar (Workloads, Config, Network, Storage, Access Control, CRDs by group), context & namespace pickers |
| Tables | k9s renderers/colorers, live informer updates, virtualized rows (2k rows ≈ 3 ms/frame), click-to-sort, k9s filters (regex, `-f` fuzzy, `!` inverse, `-l` labels), wide mode |
| Details | Describe + YAML drawer, in-place edit & apply |
| Dock | Log streaming (multi-container, follow, search, wrap, timestamps), pod shell (exec + VT emulator) |
| Actions | Delete (with force), scale, rollout restart, port-forward (`pf` lists/stops) |
| Nodes | Cordon / uncordon, drain (ignore DaemonSets, delete emptyDir, force) |
| Logs+ | Multi-pod streams for deployments/sts/ds/jobs/services with per-pod tags, JSON pretty-print (level/msg/fields), level filter, download to `~/Downloads` |
| Secrets | Decoded data tab, per-key reveal/copy, copy all as `.env`, download `.env` (mode 600) |
| Helm | Releases, history (enter), values (user/computed), rollback, uninstall |
| XRay | k9s dependency tree for deploy/sts/ds/rs/svc/pods as collapsible tree or pannable graph |
| Safety | Per-context policies: **protected** (auto-detected for names like `prod`/`production`/`prd`/`live`) adds a red frame, a PRODUCTION badge and type-the-name confirmation for every change; **read-only** blocks edits, deletes, scaling, shells and assistant actions |
| Selection | Ctrl+click, Shift+click, `space`, Ctrl+A; bulk delete/restart/cordon with one confirmation; right-click context menu with every action for the row (or the selection) |
| Workspace | Tabs (Ctrl+T / Ctrl+W / Ctrl+Tab), bookmarks of resource+namespace+filter (`m`, sidebar section, `:bm`), port-forward manager with pinned forwards restored on connect and open-in-browser |
| Appearance | Dark and light themes (title bar toggle, `:theme`), preferences panel (Ctrl+, or gear) |
| Notifications | Health watcher (CrashLoop/ImagePull/OOM, node NotReady, deployment degraded/rollout complete) → desktop notifications via D-Bus + in-app bell |
| Overview | Nodes, pods by status, deployments health, CPU/memory requests (+usage with metrics-server), warning events |
| Keyboard | k9s style: `:` / Ctrl+K command palette (`po`, `deploy`, `ns demo`, `ctx …`, `pf`), `/` filter, `j/k`, `enter`, `l` logs, `p` previous logs, `s` shell, `d` describe, `y` yaml, `e` edit, `shift+f` port-forward, `r` restart (drain on nodes), `c`/`u` cordon/uncordon, `x` xray (decode on secrets), `v` helm values, `a` assistant, `b`/`esc` back, `ctrl+d` delete, `w` wide, `n` namespace, `0` all namespaces; palette: `xray deploy`, `helm`, `notify` |

## Assistant (AI)

Right-side panel (`a`, the **Assistant** button in the title bar, or `:ai <question>`)
that troubleshoots the cluster with read-only tools: overview, list, YAML, describe,
events, logs (multi-pod, previous, grep) and XRay. Changes are only **proposed**
(`scale`, `restart`, `delete`, `apply`, `cordon`, `uncordon`) and run after you approve
them in a confirmation dialog.

| Provider | API | Key |
|---|---|---|
| Anthropic (Claude, default `claude-opus-5-5`) | official `anthropic-sdk-go`, streaming, server-side refusal fallback | `$ANTHROPIC_API_KEY` / `ant auth login` |
| OpenAI, Google Gemini, Mistral, Groq, OpenRouter, DeepSeek, xAI | OpenAI Chat Completions (`openai-go`) | `$OPENAI_API_KEY`, `$GEMINI_API_KEY`, … |
| Ollama, LM Studio, vLLM, llama.cpp, LocalAI (local) | OpenAI-compatible endpoint | none |

Settings (gear icon) store provider, model, endpoint and optionally a key in
`~/.config/kubeon/ai.json` (mode 600); models can be listed from the endpoint. The
assistant can be disabled per kube context.

Privacy: Secret values are never sent (data/stringData replaced, keys kept); env
vars with sensitive names, bearer tokens, passwords in URLs/logs, JWTs, cloud and
VCS tokens and PEM private keys are redacted from every tool output.

Local models need reliable tool calling (e.g. Llama 3.1 8B+, Qwen 2.5 7B+); very
small models may describe tool calls instead of making them — kubeon executes
well-formed JSON calls written as text, but larger models give far better answers.

## Window chrome

kubeon draws its own title bar (brand, centered command search, window
controls). Gio is vendored in
`third_party/gio` with a small Wayland patch (`third_party/_gio-patches`,
re-apply with `scripts/vendor-gio.sh`):

- drag the title bar of a maximized window to restore & move it
- double-click the title bar to maximize / restore
- right-click the title bar for the compositor window menu
- window mode stays in sync with compositor actions (Super+↑, drag-to-top, dock restore)

## Build & run

```sh
make build            # release binary ./kubeon (uses $KUBECONFIG / ~/.kube/config like kubectl)
./kubeon --context my-ctx --namespace default --resource deploy
```

Linux system deps: Wayland/EGL/xkbcommon (present). For X11 + Vulkan support install
`libx11-dev libx11-xcb-dev libxkbcommon-x11-dev libxcursor-dev libxfixes-dev libvulkan-dev`
and build with `make build GIO_TAGS=`.

## Development (safe by construction)

Dev builds (`-tags dev`) are **locked to a disposable kind cluster**: they only accept a
kubeconfig inside a `.dev/` directory and contexts named `kind-kubeon*`; client-go caches go
to `.dev/cache`. Real clusters configured on the machine are never touched.

```sh
make dev-up           # kind cluster "kubeon-dev" → .dev/kubeconfig (+ fixtures; STRESS=1 adds 2000 configmaps)
make run              # dev build against the kind cluster
make test             # unit tests
make test-int         # backend integration + headless UI tests; screenshots in .dev/shots/
make dev-down
make vendor-k9s       # re-vendor k9s (K9S_REF=vX.Y.Z), overlays in third_party/_k9s-overlay
```

## Layout

```
cmd/kubeon            entry point, flags, logging (~/.cache/kubeon/kubeon.log)
internal/backend      facade over k9s: Cluster, TableWatch snapshots, actions, logs, exec, port-forward, overview
internal/ui           Gio app: table, sidebar, drawer, dock (logs/terminal), picker/palette, overview, icons
internal/safety       kubeconfig/context guard for dev builds
third_party/k9s       vendored k9s engine (generated by scripts/vendor-k9s.sh — do not edit by hand)
```

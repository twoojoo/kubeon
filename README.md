<p align="center"><img src="packaging/icons/kubeon-128.png" width="96" alt="Kubeon logo"></p>

# Kubeon

**Kubeon** is a fast, native, open-source Kubernetes IDE — an alternative to Lens, written in Go
with a GPU-rendered [Gio](https://gioui.org) UI on top of the [k9s](https://github.com/derailed/k9s)
engine. No Electron, no web stack, no account: one small binary for Linux, Windows and macOS.

[![ci](https://github.com/twoojoo/kubeon/actions/workflows/ci.yml/badge.svg)](https://github.com/twoojoo/kubeon/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/twoojoo/kubeon?label=download)](https://github.com/twoojoo/kubeon/releases/latest)
[![license](https://img.shields.io/badge/license-GPL--3.0--or--later-blue)](LICENSE)

**Website: [twoojoo.github.io/kubeon](https://twoojoo.github.io/kubeon/)**

- **UI**: [Gio](https://gioui.org) (GPU immediate-mode, pure Go): Linux (Wayland/X11),
  Windows (Direct3D 11), macOS (Metal).
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
| Safety | Per-context policies: **protected** (auto-detected for names like `prod`/`production`/`prd`/`live`) adds a red frame, a PRODUCTION badge and type-the-name confirmation for every change, and asks before opening a port-forward tunnel (pinned tunnels restored on connect are listed in the notification bell); **read-only** blocks edits, deletes, scaling, shells and assistant actions — port-forwards stay allowed: they change nothing in the cluster (Kubernetes RBAC on `pods/portforward` still applies) |
| Selection | Ctrl+click, Shift+click, `space`, Ctrl+A; bulk delete/restart/cordon with one confirmation; right-click context menu with every action for the row (or the selection) |
| Workspace | Tabs (Ctrl+T / Ctrl+W / Ctrl+Tab), bookmarks of resource+namespace+filter (`m`, sidebar section, `:bm`), port-forward manager (`:pf`, or click the tunnel counter in the status bar): name tunnels, pin them to re-open on connect (they follow the resource, so a deployment survives pod restarts), open in browser |
| Appearance | Dark and light themes (title bar toggle, `:theme`), preferences panel (Ctrl+, or gear) |
| Notifications | Health watcher (CrashLoop/ImagePull/OOM, node NotReady, deployment degraded/rollout complete) → desktop notifications (D-Bus on Linux, Notification Center on macOS, toasts on Windows) + in-app bell |
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
small models may describe tool calls instead of making them — Kubeon executes
well-formed JSON calls written as text, but larger models give far better answers.

## Install

Download the latest package from
[Releases](https://github.com/twoojoo/kubeon/releases/latest), or build it yourself — see Packaging:

| OS | Packages | Notes |
|---|---|---|
| Linux amd64 (arm64: `make package-linux` on an arm64 host) | `.AppImage`, `.deb`, `.rpm`, `.tar.gz` | glibc ≥ 2.34 (Ubuntu 22.04+, Debian 12+, Fedora 35+); Wayland or X11 |
| Windows 10+ amd64/arm64 | portable `.zip` (`kubeon.exe`) | unsigned: SmartScreen asks once ("More info → Run anyway") |
| macOS 12+ (universal) | `.dmg`, `.zip` | ad-hoc signed unless built with a Developer ID: first launch via right-click → Open, or `xattr -dr com.apple.quarantine /Applications/Kubeon.app` |

Kubeon reads `$KUBECONFIG` / `~/.kube/config` like kubectl. Started from a desktop
launcher (Finder, Dock, app menu) it loads the login shell environment first, so
exec credential plugins (`aws`, `gke-gcloud-auth-plugin`, `kubelogin`, …) on your
shell `PATH` work. `kubeon --version` prints the version.

### Updates

Release builds check the latest release of `twoojoo/kubeon` daily (Preferences → Updates, or *Check
now*). A newer version shows in the status bar; *Update now* downloads it,
verifies it and replaces this copy in place, then restarts:

| Installed as | Update |
|---|---|
| AppImage, `tar.gz` binary, portable Windows `kubeon.exe`, macOS `Kubeon.app` | in place, when the location is writable |
| `.deb` / `.rpm` (in `/usr`) | release page link: update with the package manager |

Every download must match `SHA256SUMS.txt`, and that file must carry a valid
ed25519 signature (`SHA256SUMS.txt.sig`) from the Kubeon release key whose
public half is built into the app (`internal/update`). Unsigned or foreign
releases are never installed. CI signs releases with the secret
`UPDATE_SIGNING_KEY` (`go run ./scripts/signsums keygen <file>` creates a key
pair; keep the private key safe — losing it means existing installs can no
longer update themselves). Dev builds never check for updates.
`KUBEON_UPDATE_URL` points the check at another release endpoint.

### Verifying a download

`SHA256SUMS.txt` lists the checksum of every file and is signed with the Kubeon release key
(ed25519, `SHA256SUMS.txt.sig`, base64). Public key:

```
pX1Dosf+ulIw6K2Q6uyYvCbwz99oGrYDw3AmXOLltvg=
```

The self-updater refuses any file that doesn't match a checksum signed with this key. To check by
hand: `sha256sum -c SHA256SUMS.txt --ignore-missing`, then verify the signature with any ed25519
tool, e.g. in Python with `cryptography`:

```python
import base64
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
key = Ed25519PublicKey.from_public_bytes(base64.b64decode("pX1Dosf+ulIw6K2Q6uyYvCbwz99oGrYDw3AmXOLltvg="))
key.verify(base64.b64decode(open("SHA256SUMS.txt.sig").read()), open("SHA256SUMS.txt", "rb").read())
print("signature ok")
```

### Third-party licenses

Every package ships `THIRD_PARTY_NOTICES.txt` (`make notices`): the license of
each linked dependency, the vendored k9s (Apache-2.0) and Gio (Unlicense)
with the list of Kubeon's modifications, and the MPL-2.0 source note.

## How it compares

| | Kubeon | Lens Desktop | Freelens | Headlamp | k9s |
|---|---|---|---|---|---|
| Runtime | native (Go, GPU UI) | Electron | Electron | web / Electron | terminal (TUI) |
| License | GPL-3.0 | proprietary | MIT | Apache-2.0 | Apache-2.0 |
| Price | free | free tier + paid plans | free | free | free |
| Account | none | Lens ID | none | none | none |

## FAQ

**Is there a native Lens alternative without Electron?**
Yes: Kubeon is a single native binary with a GPU-rendered UI. Tables are virtualized and fed by
informers, so namespaces with thousands of resources stay responsive.

**Does it need an account, an agent in the cluster or a cloud service?**
No. Kubeon reads your kubeconfig like kubectl and talks to the API server directly; nothing is
installed in the cluster and no account is needed.

**Can I use it safely on production clusters?**
Contexts can be marked protected (auto-detected for names like `prod`): every change asks for a
typed confirmation. Read-only contexts block edits, deletes, scaling, shells and assistant actions.

**Does the AI assistant send my cluster data anywhere?**
Only if you configure it, to the provider you choose (a local Ollama model works too). Its tools are
read-only, Secret values are never sent and tokens, passwords and keys are redacted from every tool
output. It can be disabled per context.

## Window chrome

Kubeon draws its own title bar (brand, namespace, centered command search,
window controls). Gio is vendored in `third_party/gio` with small patches
(`third_party/_gio-patches`, re-apply with `scripts/vendor-gio.sh`):

- Linux/Wayland: drag the title bar of a maximized window to restore & move it,
  double-click to maximize / restore, right-click for the compositor window menu,
  window mode stays in sync with compositor actions (Super+↑, drag-to-top, dock restore)
- macOS: native traffic lights over the custom title bar, double-click zooms
- Windows: native caption behaviour (snap, double-click, system menu);
  Direct3D 11 falls back to the WARP software rasterizer without a GPU (VMs, RDP)

## Build & run

```sh
make build            # release binary ./kubeon (uses $KUBECONFIG / ~/.kube/config like kubectl)
./kubeon --context my-ctx --namespace default --resource deploy
```

Linux system deps: Wayland/EGL/xkbcommon (present). For X11 + Vulkan support install
`libx11-dev libx11-xcb-dev libxkbcommon-x11-dev libxcursor-dev libxfixes-dev libvulkan-dev`
and build with `make build GIO_TAGS=`.

## Packaging

Everything lands in `dist/`; the version comes from `git describe` (override with `VERSION=`).

```sh
make icons            # packaging/icons (png/ico/icns) + internal/ui/assets/logo.png from packaging/logo-source.png
make package-linux    # Docker (Ubuntu 22.04 + full Gio deps): AppImage, deb, rpm, tar.gz for the host arch
make package-windows  # cross-compiled from any OS (pure Go): zips for amd64 + arm64, icon/manifest/version via go-winres
make package-macos    # on a Mac: universal Kubeon.app (arm64+amd64), dmg, zip
```

macOS signing/notarization is optional: `MACOS_SIGN_IDENTITY="Developer ID Application: …"`
and `MACOS_NOTARY_PROFILE=<notarytool keychain profile>`. In CI set the secrets
`MACOS_CERT_P12` (base64), `MACOS_CERT_PASSWORD`, `MACOS_NOTARY_APPLE_ID`,
`MACOS_NOTARY_TEAM_ID`, `MACOS_NOTARY_PASSWORD`.

CI (`.github/workflows/ci.yml`) runs unit tests, the kind integration suite with
headless UI screenshots, builds every package on Linux, Windows and macOS
runners with a launch smoke test, and on a `v*` tag publishes them, with a
signed `SHA256SUMS.txt`, as a release of this repository. Secret:
`UPDATE_SIGNING_KEY` (release signing key).

### Upstream k9s

`scripts/k9s-upstream-report.sh [tag|branch]` (default: latest release) tells
what moving the vendored k9s there would take:

| | |
|---|---|
| **verdict** | ✅ looks safe · ⚠️ needs attention · ❌ will break |
| **kubeon patches** | each file of `third_party/_k9s-patches` dry-run against the target exactly as vendoring applies it (no fuzz): applies cleanly · **already upstream** (drop it) · only with fuzz (context moved: review) · **conflict** (with `patch`'s messages) |
| **overlays** | `third_party/_k9s-overlay` files changed or removed upstream |
| **API** | incompatible changes found by `apidiff` in every vendored package, split into those touching what kubeon uses (`go run ./scripts/k9suse` lists it: functions, types, methods, fields and the k9s types in their signatures) and the rest; packages removed or no longer building included |
| **history** | release notes, commits touching the vendored packages, per-package diffstat |

`scripts/k9s-upstream-report-test.sh` checks every outcome end to end
(conflict, fuzz, already upstream, API change used/unused, interface change,
package removed/broken, unresolvable dependency).

`.github/workflows/upstream-k9s.yml` runs daily:

- keeps the **"k9s upstream watch"** issue current with the report against
  k9s `master` (body edits are silent; a comment, i.e. a notification, only
  when the verdict changes);
- when a **new k9s release** is out: re-vendors it (patches applied strictly,
  no fuzz), `go mod tidy`, build, unit and kind integration tests, then opens a
  pull request (draft if anything failed) with the report — an issue if
  Actions may not open PRs, or **"k9s X: update blocked"** when a patch no
  longer applies. Run it by hand with `ref` to try a tag or branch early.

`K9S_REF=<tag> make vendor-k9s` re-vendors locally (default: the version in
`third_party/k9s/UPSTREAM`).

## Development (safe by construction)

Dev builds (`-tags dev`) are **locked to a disposable kind cluster**: they only accept a
kubeconfig inside a `.dev/` directory and contexts named `kind-kubeon*`; client-go caches go
to `.dev/cache`. Real clusters configured on the machine are never touched.

```sh
make dev-up           # kind cluster "kubeon-dev" → .dev/kubeconfig (+ fixtures; STRESS=1 adds 2000 configmaps)
make run              # dev build against the kind cluster
make test             # unit tests
make test-int         # backend integration + headless UI tests; screenshots in .dev/shots/
make site             # website (GitHub Pages) in dist/site; make site-shots renews its screenshots
make dev-down
make vendor-k9s       # re-vendor k9s (K9S_REF=vX.Y.Z), overlays in third_party/_k9s-overlay
```

## Layout

```
cmd/kubeon            entry point, flags, logging (~/.cache/kubeon/kubeon.log)
internal/backend      facade over k9s: Cluster, TableWatch snapshots, actions, logs, exec, port-forward, overview
internal/ui           Gio app: table, sidebar, drawer, dock (logs/terminal), picker/palette, overview, icons
internal/safety       kubeconfig/context guard for dev builds
internal/desktop      OS integration: open URLs, login-shell env import, Wayland cursor settings
internal/notify       desktop notifications (D-Bus / Notification Center / Windows toasts)
internal/site         website (GitHub Pages): make site, screenshots from make site-shots
packaging/            icons, Linux desktop entry + Dockerfile + nfpm, macOS Info.plist, Windows resources
scripts/package-*.sh  release packaging per OS (make package-linux|windows|macos)
third_party/k9s       vendored k9s engine (generated by scripts/vendor-k9s.sh — do not edit by hand)
```

See [CONTRIBUTING.md](CONTRIBUTING.md) to send changes and [SECURITY.md](SECURITY.md) to report a
vulnerability.

## License

Kubeon is free software: you can redistribute it and/or modify it under the terms of the GNU General
Public License as published by the Free Software Foundation, either version 3 of the License, or (at
your option) any later version. See [LICENSE](LICENSE). The vendored k9s engine (`third_party/k9s`)
is Apache-2.0 and Gio (`third_party/gio`) is Unlicense/MIT; third-party licenses are listed in
`THIRD_PARTY_NOTICES.txt`, shipped with every package.

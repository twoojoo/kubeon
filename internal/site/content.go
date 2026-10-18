package site

// Text shared by the HTML pages and llms.txt / llms-full.txt.

const (
	name    = "Kubeon"
	tagline = "A fast, native, open-source Kubernetes IDE"
	summary = "Kubeon is a free, open-source (GPL-3.0) Kubernetes IDE for Linux, Windows and macOS, written in Go " +
		"with a GPU-rendered native UI on top of the k9s engine: no Electron, no account, nothing installed in the cluster. " +
		"Live resource tables, logs, pod shells, port-forwards, Helm, XRay, YAML editing with diff review and an optional AI assistant."
)

type card struct{ Title, Text string }

var cards = []card{
	{"Native and light", "One small binary with a GPU-rendered UI. No Electron, no browser engine, no account: Kubeon reads your kubeconfig like kubectl."},
	{"The k9s engine", "Tables come from k9s renderers fed by informers: live updates, virtualized rows, k9s filters (regex, fuzzy, inverse, labels) and k9s-style keys."},
	{"Logs and shells", "Log streams for a pod or a whole deployment with per-pod tags, JSON pretty-printing and level filter; pod shells in a real terminal emulator."},
	{"Port-forwards", "Open tunnels from any pod, deployment or service, name them, pin them to reopen on connect, open them in the browser."},
	{"Helm and XRay", "Helm releases, history, values and rollback; the k9s XRay dependency tree as a collapsible tree or a pannable graph."},
	{"Edit with a diff", "Edit YAML in place with live validation, review the diff and a server-side dry run, then apply."},
	{"Safe on production", "Protected contexts confirm every change by typing the resource name; read-only contexts block edits, deletes, scaling and shells."},
	{"AI assistant", "Optional: Anthropic or any OpenAI-compatible provider, Ollama included. Read-only tools, Secret values never sent, changes only proposed."},
}

type faq struct{ Q, A string }

var homeFAQ = []faq{
	{"Is there a native Lens alternative without Electron?",
		"Yes. Kubeon is a single native binary with a GPU-rendered UI. Its tables are virtualized and fed by informers, so namespaces with thousands of resources stay responsive."},
	{"How is Kubeon related to k9s?",
		"Kubeon embeds the backend packages of k9s (client, informers, renderers, XRay) and draws them in a desktop UI: the same columns, colors, filters and many of the same keys, plus mouse, tabs, a log viewer and editors."},
	{"Is Kubeon free?",
		"Yes. Kubeon is free software under the GNU GPL 3.0 or later, with no paid edition and no account."},
	{"Which operating systems does it run on?",
		"Linux (AppImage, .deb, .rpm, .tar.gz), Windows 10 and later (x64 and ARM64) and macOS 12 and later (Apple silicon and Intel)."},
	{"Does it need anything installed in the cluster?",
		"No. Kubeon talks to the API server with your kubeconfig, like kubectl, including exec credential plugins such as aws, gke-gcloud-auth-plugin and kubelogin. Resource usage is shown when metrics-server is available."},
	{"Can I use it safely on production clusters?",
		"Contexts can be marked protected (auto-detected for names like prod or production): every change asks you to type the resource name, and opening a tunnel asks first. Read-only contexts block edits, deletes, scaling, shells and assistant actions."},
	{"Does the AI assistant send my cluster data anywhere?",
		"Only if you configure it, and only to the provider you choose (a local Ollama model works too). Its tools are read-only, Secret values are never sent and tokens, passwords and keys are redacted from every tool output."},
	{"How are updates delivered?",
		"Kubeon updates itself and installs only releases whose checksums are signed with the Kubeon release key. .deb and .rpm installs are updated by the package manager."},
}

// shot is a screenshot shipped in static/img (made by
// TestUISiteScreenshots, see the Makefile target site-shots).
type shot struct{ File, Alt string }

var shots = []shot{
	{"pods.png", "Kubeon pod table with live status, the sidebar and the action bar"},
	{"overview.png", "Cluster overview: nodes, pods by status, deployments and warning events"},
	{"logs.png", "Multi-pod JSON logs of a deployment with level colors"},
	{"xray.png", "XRay dependency graph of a deployment"},
	{"helm.png", "Helm release values next to the release list"},
	{"diff.png", "YAML edit reviewed as a diff with a server-side dry run"},
	{"assistant.png", "AI assistant proposing a change that runs only after approval"},
	{"light.png", "Kubeon with the light theme"},
}

// Kubeon vs Lens. Keep it factual: what each has, no benchmarks.
type compareRow struct{ Topic, Kubeon, Other string }

var lensRows = []compareRow{
	{"Runtime", "Native Go binary, GPU-rendered UI", "Electron (Chromium + Node.js)"},
	{"License", "GPL-3.0-or-later, single free edition", "Proprietary; free tier and paid subscriptions"},
	{"Account", "None", "Lens ID sign-in"},
	{"Engine", "k9s renderers and informers, k9s filters and keys", "Own engine"},
	{"Logs", "Multi-pod streams with per-pod tags, JSON pretty-print, level filter, download", "Pod logs viewer"},
	{"Port-forwards", "Named, pinned tunnels reopened on connect, following the resource", "Port-forward list"},
	{"Production safety", "Protected contexts (typed confirmation) and read-only contexts", "Relies on Kubernetes RBAC"},
	{"Cluster checks", "Sanitizer (failing pods, missing references, services without endpoints…) and RBAC explorer", "Depends on version and plan"},
	{"Metrics", "metrics-server usage in the overview", "Built-in metrics charts (Prometheus)"},
	{"Extensions", "Not available", "Extension API"},
	{"Updates", "Built-in, signed (ed25519) self-update", "Built-in updater"},
}

var lensFAQ = []faq{
	{"Is Kubeon a drop-in replacement for Lens?",
		"For everyday work (browse resources, logs, shells, port-forwards, Helm, editing YAML) yes. If you rely on Lens extensions, its Prometheus charts or team features, Lens is still the better tool."},
	{"Why choose a native Kubernetes client over Lens?",
		"Kubeon starts as a single native process without a browser engine, needs no account and no network access besides your clusters, and keeps large tables responsive with virtualized rows."},
	{"I use k9s in the terminal: why Kubeon?",
		"Kubeon keeps the k9s engine, columns and keys and adds a desktop UI: mouse, tabs, multi-pod log viewer, YAML editing with a diff, a graph view for XRay, protected contexts and an assistant."},
	{"Can I keep using my kubeconfig and credential plugins?",
		"Yes. Kubeon reads $KUBECONFIG or ~/.kube/config like kubectl. Started from a desktop launcher it loads your login shell environment first, so exec plugins on your shell PATH work."},
}

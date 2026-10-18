package site

// Feature is one page under features/. Keep every claim verifiable in the
// code (internal/ui, internal/backend).
type Feature struct {
	Slug string // URL path segment
	Name string
	// Title and Description are the <title> and meta description.
	Title       string
	Description string
	Intro       []string
	Highlights  []string
	Notes       []string // limits and differences worth knowing
	Keys        []keyHint
	// Shot is a screenshot in static/img (TestUISiteScreenshots).
	Shot, ShotAlt string
}

type keyHint struct{ Key, Action string }

var features = []Feature{
	{
		Slug: "logs", Name: "Log viewer",
		Title:       "Kubernetes log viewer for pods and deployments — Kubeon",
		Description: "Stream Kubernetes logs of a pod or a whole deployment, statefulset, daemonset, job or service: per-pod tags, JSON pretty-print, level filter.",
		Intro: []string{
			"Kubeon streams logs into a dock at the bottom of the window, next to the resource you are looking at. Select a pod and press l, or select a deployment, statefulset, daemonset, job or service to follow every pod behind it in one stream, each line tagged with its pod.",
			"Structured logs are recognized: JSON lines are shown as level, message and fields, with colors per level and a filter to keep only warnings or errors.",
		},
		Highlights: []string{
			"Multi-pod streams for deployments, statefulsets, daemonsets, jobs and services, with per-pod tags.",
			"Multi-container pods: one container or all of them.",
			"JSON pretty-print (level, message, fields) and a minimum-level filter.",
			"Regex search, follow, wrap and timestamps toggles.",
			"Previous logs of a restarted container (p).",
			"Download the stream to a file in ~/Downloads.",
		},
		Keys: []keyHint{{"l", "logs of the selected resource"}, {"p", "previous logs"}},
		Shot: "logs.png", ShotAlt: "Multi-pod JSON logs of a deployment with level colors",
	},
	{
		Slug: "pod-shell", Name: "Pod shell",
		Title:       "Shell into Kubernetes pods (kubectl exec) — Kubeon",
		Description: "Open a shell in any Kubernetes pod container from a desktop app: real terminal emulator, container picker, bash with sh fallback, tabs in a dock.",
		Intro: []string{
			"Press s on a pod to open a shell in one of its containers, like kubectl exec -it. Kubeon runs bash when the image has it and falls back to sh, in a terminal emulator that handles full-screen programs such as top or vi.",
			"Shells live in the same dock as logs, as tabs, so you can keep a shell and the logs of the same pod side by side with the resource table.",
		},
		Highlights: []string{
			"Container picker for multi-container pods.",
			"bash when available, sh otherwise.",
			"VT terminal emulator: colors, cursor movement, full-screen programs, resize.",
			"Several shells and log streams open at once as dock tabs.",
		},
		Notes: []string{"Read-only contexts block shells: a shell can change anything inside the container."},
		Keys:  []keyHint{{"s", "shell into the selected pod"}},
		Shot:  "shell.png", ShotAlt: "Shell in a pod container in the Kubeon dock",
	},
	{
		Slug: "port-forward", Name: "Port-forward manager",
		Title:       "Kubernetes port-forward manager with pinned tunnels — Kubeon",
		Description: "Port-forward Kubernetes pods, deployments and services from a desktop app: named tunnels, pinned tunnels reopened on connect, open in the browser.",
		Intro: []string{
			"Press Shift+F on a pod, deployment or service to open a tunnel, like kubectl port-forward. Every open tunnel is listed in the port-forward manager (pf in the command palette, or the tunnel counter in the status bar).",
			"Tunnels can be named and pinned: pinned tunnels are reopened every time you connect to the context. They follow the resource, so a tunnel to a deployment survives pod restarts.",
		},
		Highlights: []string{
			"Tunnels to pods, deployments and services.",
			"Names for tunnels, shown in messages and notifications.",
			"Pinned tunnels reopened on connect, announced in the notification bell.",
			"Open a tunnel in the browser with one click.",
			"Protected contexts ask before opening a tunnel.",
		},
		Notes: []string{"Port-forwards stay allowed on read-only contexts: they change nothing in the cluster (Kubernetes RBAC on pods/portforward still applies)."},
		Keys:  []keyHint{{"Shift+F", "port-forward the selected resource"}, {":pf", "port-forward manager"}},
		Shot:  "portforwards.png", ShotAlt: "Port-forward manager with a pinned tunnel",
	},
	{
		Slug: "helm", Name: "Helm releases",
		Title:       "Helm GUI: releases, history, values and rollback — Kubeon",
		Description: "Browse Helm releases in a native Kubernetes IDE: revision history, user and computed values, rollback to a revision and uninstall, with confirmations.",
		Intro: []string{
			"The Releases entry of the sidebar lists the Helm releases of the namespace with chart, app version and status, read from the cluster with the Helm SDK: no helm binary needed.",
			"Enter shows the revision history of a release, where you can roll back to a revision; v shows its values, user-supplied or computed.",
		},
		Highlights: []string{
			"Release list with chart, version and status.",
			"Revision history (Enter) and rollback to a revision.",
			"User-supplied and computed values (v).",
			"Uninstall with confirmation; protected contexts require typing the release name.",
		},
		Keys: []keyHint{{"Enter", "release history"}, {"v", "values"}, {":helm", "open releases"}},
		Shot: "helm.png", ShotAlt: "Helm release values next to the release list",
	},
	{
		Slug: "xray", Name: "XRay",
		Title:       "Kubernetes dependency graph (k9s XRay) — Kubeon",
		Description: "See what a Kubernetes deployment, statefulset, daemonset, service or pod depends on: the k9s XRay tree as a collapsible tree or a pannable graph.",
		Intro: []string{
			"XRay is the k9s view of everything a workload is made of: replicasets, pods, containers, config maps, secrets, service accounts and volumes. Press x on a deployment, statefulset, daemonset, replicaset, service or pod to open it.",
			"Kubeon shows the tree as a collapsible list or as a graph you can pan, with the status of every node, and you can open the details of any node.",
		},
		Highlights: []string{
			"Works on deployments, statefulsets, daemonsets, replicasets, services and pods.",
			"Collapsible tree or pannable graph.",
			"Status colors per node: spot the failing pod or the missing secret at a glance.",
		},
		Keys: []keyHint{{"x", "XRay of the selected resource"}, {":xray deploy", "XRay of every deployment"}},
		Shot: "xray.png", ShotAlt: "XRay dependency graph of a deployment",
	},
	{
		Slug: "yaml-editor", Name: "YAML editor",
		Title:       "Edit Kubernetes YAML with diff and dry run — Kubeon",
		Description: "Edit Kubernetes resources in place: YAML highlighting, live validation, a diff of your changes and a server-side dry run before you apply them.",
		Intro: []string{
			"Press y to read the YAML of a resource and e to edit it in place. The editor highlights YAML and validates it as you type, so a broken indentation is caught before it reaches the API server.",
			"Saving does not apply right away: Kubeon shows the diff of your changes and runs a server-side dry run, and applies only when you confirm.",
		},
		Highlights: []string{
			"YAML highlighting and live validation.",
			"Diff of the changes before applying.",
			"Server-side dry run: admission errors show up before the change is made.",
			"Describe view next to YAML in the same drawer.",
			"Protected contexts ask you to type the resource name; read-only contexts block edits.",
		},
		Keys: []keyHint{{"y", "YAML"}, {"e", "edit"}, {"d", "describe"}},
		Shot: "diff.png", ShotAlt: "YAML edit reviewed as a diff with a server-side dry run",
	},
	{
		Slug: "production-safety", Name: "Production safety",
		Title:       "Safe on production Kubernetes clusters — Kubeon",
		Description: "Protected and read-only kube contexts: typed confirmation for every change, a red frame and PRODUCTION badge, shells and edits blocked when read-only.",
		Intro: []string{
			"Each kube context has a policy. Protected contexts, detected automatically for names like prod, production, prd or live, get a red frame and a PRODUCTION badge, and every change asks you to type the name of the resource first.",
			"Read-only contexts block edits, deletes, scaling, shells and assistant actions in the UI, whatever your Kubernetes permissions allow.",
		},
		Highlights: []string{
			"Protected contexts auto-detected from the context name, or set by hand.",
			"Typed confirmation of changes on protected contexts: deletes, scaling, edits, rollbacks, drains, shells and approved assistant proposals.",
			"Read-only contexts: edits, deletes, scaling, shells and assistant actions blocked.",
			"Protected contexts ask before opening a port-forward tunnel.",
			"The assistant can be disabled per context.",
		},
		Notes: []string{"Policies are a guard rail in the client, not a security boundary: Kubernetes RBAC remains the authority on what your credentials can do."},
		Shot:  "protected.png", ShotAlt: "Typed confirmation of a delete on a protected context",
	},
	{
		Slug: "rbac-explorer", Name: "RBAC explorer",
		Title:       "Kubernetes RBAC explorer: who can do what — Kubeon",
		Description: "Explore Kubernetes RBAC: effective rules of users, groups and service accounts with their bindings, can-i checks and who-can queries.",
		Intro: []string{
			"The RBAC Explorer lists every subject the cluster's bindings mention (users, groups and service accounts) with its effective rules, each one linked to the binding and the role that grants it.",
			"Ask whether a subject can perform a verb on a resource: Kubeon evaluates the rules locally and confirms with a SubjectAccessReview on the API server. Or turn the question around and list who can do it.",
		},
		Highlights: []string{
			"Effective rules per subject with their binding and role.",
			"can-i checks evaluated locally and confirmed by the API server (SubjectAccessReview).",
			"who-can: every subject allowed to perform a verb on a resource.",
			"Namespaced bindings are never applied to cluster-scoped resources, like the Kubernetes authorizer.",
		},
		Shot: "rbac.png", ShotAlt: "RBAC explorer with the effective rules of a subject",
	},
	{
		Slug: "sanitizer", Name: "Sanitizer",
		Title:       "Kubernetes cluster sanitizer and linter — Kubeon",
		Description: "Find what is wrong in a namespace: failing pods, missing config maps and secrets, services without endpoints, risky containers. Scored by category.",
		Intro: []string{
			"The Sanitizer scans the current namespace (or all of them) and lists what needs attention, from errors to hygiene hints, with a score per category and an overall grade.",
			"Every finding links to its resource: click it to open the resource with its details.",
		},
		Highlights: []string{
			"Pods: failing, pending, restarting, not managed by a controller, referencing config maps or secrets that don't exist.",
			"Workloads: deployments with no or too few available replicas, degraded statefulsets.",
			"Services whose selector matches no pod or whose targetPort is not a port of the pods; ingresses pointing to missing services.",
			"Security: privileged containers, host namespaces; images with floating tags; missing requests, limits and probes.",
			"Unused config maps, secrets and claims; unbound claims; nodes not ready, under pressure or cordoned.",
		},
		Shot: "sanitizer.png", ShotAlt: "Sanitizer report with scores per category",
	},
	{
		Slug: "ai-assistant", Name: "AI assistant",
		Title:       "AI assistant for Kubernetes troubleshooting — Kubeon",
		Description: "Troubleshoot Kubernetes with Claude, OpenAI-compatible models or a local Ollama model: read-only tools, redacted secrets, changes run only after approval.",
		Intro: []string{
			"The assistant panel troubleshoots the cluster with read-only tools: overview, list, YAML, describe, events, logs (multi-pod, previous, grep) and XRay. Ask it why a pod crashes or a service has no endpoints and it looks for itself.",
			"It never changes anything on its own: scale, restart, delete, apply, cordon and uncordon are proposed as cards that run only after you approve them in a confirmation dialog.",
		},
		Highlights: []string{
			"Anthropic (Claude) or any OpenAI-compatible provider: OpenAI, Gemini, Mistral, Groq, OpenRouter, DeepSeek, xAI.",
			"Local models through Ollama, LM Studio, vLLM, llama.cpp or LocalAI.",
			"Secret values are never sent; tokens, passwords, JWTs and private keys are redacted from every tool output.",
			"Changes are only proposed and run after approval, under the context's policy.",
			"Can be disabled per context.",
		},
		Notes: []string{"Optional and off until you configure a provider. API keys can stay in environment variables; a key saved in settings is stored with mode 600."},
		Keys:  []keyHint{{"a", "open the assistant"}, {":ai <question>", "ask from the command palette"}},
		Shot:  "assistant.png", ShotAlt: "AI assistant proposing a change that runs only after approval",
	},
}

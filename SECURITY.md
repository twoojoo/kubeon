# Security policy

Kubeon uses your kubeconfig credentials, opens shells and port-forwards into clusters, reads Secrets
and can send cluster data to an AI provider: security reports are taken seriously.

## Reporting a vulnerability

Please **do not open a public issue**. Report it privately through
[GitHub security advisories](https://github.com/twoojoo/kubeon/security/advisories/new)
with the affected version, the steps to reproduce and the impact you expect.

You will get an answer within a week. Once a fix is released the advisory is published, crediting
you unless you prefer otherwise.

## Supported versions

Only the latest release receives security fixes; Kubeon updates itself to it.

## Scope

Examples of what we want to hear about: Secret values or tokens leaking to logs, files or the AI
provider (redaction bypasses); bypasses of read-only or protected contexts; assistant actions that run
without confirmation; self-update installing a release not signed with the Kubeon release key.

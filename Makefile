# kubeon — native Kubernetes IDE (Gio UI + k9s engine)
#
# `make run` / `make test-int` build with the `dev` tag: the binary is locked
# to the disposable kind cluster in .dev/kubeconfig and refuses any other
# kubeconfig or context. Release builds (`make build`) behave like kubectl.

# Set GIO_TAGS= (empty) once X11/Vulkan dev headers are installed:
#   sudo apt install libx11-dev libx11-xcb-dev libxkbcommon-x11-dev libxcursor-dev libxfixes-dev libvulkan-dev
GIO_TAGS ?= nox11,novulkan
DEV_TAGS := $(GIO_TAGS),dev

.PHONY: build build-dev run test test-int dev-up dev-down vendor-k9s

build:
	go build -tags '$(GIO_TAGS)' -trimpath -ldflags '-s -w' -o kubeon ./cmd/kubeon

build-dev:
	go build -tags '$(DEV_TAGS)' -o .dev/kubeon-dev ./cmd/kubeon

run: build-dev
	env -u KUBECONFIG .dev/kubeon-dev --kubeconfig .dev/kubeconfig $(ARGS)

test:
	go vet -tags '$(DEV_TAGS)' ./internal/... ./cmd/...
	go test -tags '$(DEV_TAGS)' ./internal/...

# integration + headless UI screenshots (in .dev/shots) against kind only
test-int:
	env -u KUBECONFIG go test -tags '$(DEV_TAGS),integration' -count=1 -timeout 300s ./internal/...

dev-up:
	./scripts/dev-cluster.sh up

dev-down:
	./scripts/dev-cluster.sh down

vendor-k9s:
	./scripts/vendor-k9s.sh && go mod tidy

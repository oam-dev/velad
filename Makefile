include makefiles/dependency.mk

K3S_VERSION ?= v1.27.2+k3s1
STATIC_DIR := pkg/resources/static
VELA_VERSION ?= v1.10.1
VELAUX_VERSION ?= v1.9.4
VELA_VERSION_NO_V := $(subst v,,$(VELA_VERSION))
VELAUX_IMAGE_VERSION ?= v1.9.4
LDFLAGS= "-X github.com/oam-dev/velad/version.VelaUXVersion=${VELAUX_VERSION} -X github.com/oam-dev/velad/version.VelaVersion=${VELA_VERSION}"

UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S), Linux)
OS ?= linux
else
OS ?= darwin
endif
UNAME_M := $(shell uname -m)
ifeq ($(UNAME_M), arm64)
ARCH ?= arm64
else
ARCH ?= amd64
endif

.DEFAULT_GOAL := build
build:
	echo "Building for ${OS}/${ARCH}"
	OS=${OS} ARCH=${ARCH} make $(OS)-$(ARCH)


linux-amd64 linux-arm64: download_vela_images_addons download_k3s_bin_script download_k3s_images
	$(eval OS := $(word 1, $(subst -, ,$@)))
	$(eval ARCH := $(word 2, $(subst -, ,$@)))
	echo "Compiling for ${OS}/${ARCH}"

	GOOS=${OS} GOARCH=${ARCH} \
	go build -o bin/velad-${OS}-${ARCH} \
	-ldflags=${LDFLAGS} \
	github.com/oam-dev/velad/cmd/velad

darwin-amd64 darwin-arm64 windows-amd64: download_vela_images_addons download_k3d  download_k3s_images
	$(eval OS := $(word 1, $(subst -, ,$@)))
	$(eval ARCH := $(word 2, $(subst -, ,$@)))
	echo "Compiling for ${OS}/${ARCH}"

	GOOS=${OS} GOARCH=${ARCH} \
	go build -o bin/velad-${OS}-${ARCH} \
	-ldflags=${LDFLAGS} \
	github.com/oam-dev/velad/cmd/velad

CHART_DIR := ${STATIC_DIR}/vela/charts
download_vela_chart:
	mkdir -p ${CHART_DIR}
	curl -L -o ${CHART_DIR}/vela-core.tgz https://kubevela.github.io/charts/vela-core-${VELA_VERSION_NO_V}.tgz

download_vela_images_addons: download_vela_chart
	tar -xzf ${CHART_DIR}/vela-core.tgz -C ${CHART_DIR}
	./hack/download_vela_images.sh ${VELA_VERSION} ${VELAUX_IMAGE_VERSION} ${ARCH}
	./hack/download_addons.sh ${VELAUX_VERSION}
	rm -rf ${CHART_DIR}/vela-core


download_k3d:
	./hack/download_k3d_images.sh ${ARCH}

download_k3s_bin_script:
	mkdir -p ${STATIC_DIR}/k3s/other
	curl -Lo ${STATIC_DIR}/k3s/other/k3s https://github.com/k3s-io/k3s/releases/download/${K3S_VERSION}/k3s
	curl -Lo ${STATIC_DIR}/k3s/other/setup.sh https://get.k3s.io

download_k3s_images:
	mkdir -p ${STATIC_DIR}/k3s/images
	curl -Lo ${STATIC_DIR}/k3s/images/k3s-airgap-images.tar.gz https://github.com/k3s-io/k3s/releases/download/${K3S_VERSION}/k3s-airgap-images-${ARCH}.tar.gz


.PHONY: clean
clean:
	rm -f ${CHART_DIR}/vela-core.tgz
	rm -f bin/velad

lint: golangci
	$(GOLANGCILINT) run ./...

staticcheck: staticchecktool
	$(STATICCHECK) ./...

fmt: goimports
	$(GOIMPORTS) -local github.com/kubevela/velad -w $$(go list -f {{.Dir}} ./...)

go-check:
	go fmt ./...
	go vet ./...

reviewable: lint staticcheck fmt go-check
	go mod tidy

check-diff: reviewable
	git --no-pager diff
	git diff --quiet || (echo please run 'make reviewable' to include all changes && false)
	echo branch is clean

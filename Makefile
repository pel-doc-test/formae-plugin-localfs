PLUGIN_NAME  = localfs
VERSION      = 0.1.0
BINARY_NAME  = $(PLUGIN_NAME)
INSTALL_DIR  = $(HOME)/.pel/formae/plugins/$(PLUGIN_NAME)/v$(VERSION)

.PHONY: build install clean conformance-test conformance-test-crud conformance-test-discovery

build:
	@echo "Building $(PLUGIN_NAME) plugin..."
	go build -o bin/$(BINARY_NAME) .

install: build
	@echo "Installing to $(INSTALL_DIR)..."
	mkdir -p $(INSTALL_DIR)/schema/pkl
	cp bin/$(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)
	chmod +x $(INSTALL_DIR)/$(BINARY_NAME)
	cp formae-plugin.pkl $(INSTALL_DIR)/formae-plugin.pkl
	cp schema/pkl/PklProject $(INSTALL_DIR)/schema/pkl/PklProject
	cp schema/pkl/localfs.pkl $(INSTALL_DIR)/schema/pkl/localfs.pkl

conformance-test: install
	FORMAE_BINARY=/opt/pel/bin/formae FORMAE_VERSION=0.84.0 LOCALFS_BASE_PATH=$$(mktemp -d) go test -tags conformance -v -count=1 -timeout 600s ./...

conformance-test-crud: install
	FORMAE_BINARY=/opt/pel/bin/formae FORMAE_VERSION=0.84.0 LOCALFS_BASE_PATH=$$(mktemp -d) go test -tags conformance -v -count=1 -timeout 300s -run TestPluginConformance ./...

conformance-test-discovery: install
	FORMAE_BINARY=/opt/pel/bin/formae FORMAE_VERSION=0.84.0 LOCALFS_BASE_PATH=$$(mktemp -d) go test -tags conformance -v -count=1 -timeout 300s -run TestPluginDiscovery ./...

clean:
	rm -rf bin/

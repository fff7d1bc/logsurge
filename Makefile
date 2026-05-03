APP := logsurge
BUILD_DIR := $(CURDIR)/build
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
PLATFORM := $(GOOS)-$(GOARCH)
PLATFORM_BUILD_DIR := $(BUILD_DIR)/$(PLATFORM)
BIN_DIR := $(PLATFORM_BUILD_DIR)/bin
BIN := $(BIN_DIR)/$(APP)
PERF_DIR := $(PLATFORM_BUILD_DIR)/perf
GO_SOURCES := $(wildcard *.go)
GOCACHE := $(PLATFORM_BUILD_DIR)/gocache
GOMODCACHE := $(PLATFORM_BUILD_DIR)/gomodcache
GOPATH := $(PLATFORM_BUILD_DIR)/gopath
GOTMPDIR := $(PLATFORM_BUILD_DIR)/tmp
GOTELEMETRYDIR := $(PLATFORM_BUILD_DIR)/telemetry
GOENV := off
GOFLAGS := -modcacherw

export GOCACHE
export GOMODCACHE
export GOPATH
export GOTMPDIR
export GOTELEMETRYDIR
export GOENV
export GOFLAGS
export GOTELEMETRY=off

.PHONY: all build test perf perf-adhoc-smoke flood-smoke run install install-mutable clean

all: build

build: $(BIN)

test:
	mkdir -p "$(GOCACHE)" "$(GOMODCACHE)" "$(GOPATH)" "$(GOTMPDIR)" "$(GOTELEMETRYDIR)"
	go test .
	go test -race .

perf: build
	rm -rf "$(PERF_DIR)"
	mkdir -p "$(PERF_DIR)" "$(GOTMPDIR)"
	go test -run '^$$' -bench 'Benchmark' -benchmem .
	go test -run '^$$' -bench 'Benchmark' -benchmem \
		-cpuprofile "$(PERF_DIR)/bench.cpu.pprof" \
		-memprofile "$(PERF_DIR)/bench.mem.pprof" .
	@echo "benchmark profiles: $(PERF_DIR)"

perf-adhoc-smoke: build
	rm -rf "$(PERF_DIR)/adhoc"
	mkdir -p "$(PERF_DIR)/adhoc"
	"$(BIN)" --output stdout \
		--debug-cpuprofile "$(PERF_DIR)/adhoc/plain.cpu.pprof" \
		--debug-memprofile "$(PERF_DIR)/adhoc/plain.mem.pprof" \
		-- "$(BIN)" __test-producer --records 1000000 --line-bytes 120 >/dev/null
	"$(BIN)" --format json --output stdout \
		--debug-cpuprofile "$(PERF_DIR)/adhoc/json.cpu.pprof" \
		--debug-memprofile "$(PERF_DIR)/adhoc/json.mem.pprof" \
		-- "$(BIN)" __test-producer --records 1000000 --line-bytes 120 >/dev/null
	"$(BIN)" --format json --metadata source,line_end,continued --output stdout \
		--debug-cpuprofile "$(PERF_DIR)/adhoc/json-metadata.cpu.pprof" \
		--debug-memprofile "$(PERF_DIR)/adhoc/json-metadata.mem.pprof" \
		-- "$(BIN)" __test-producer --records 1000000 --line-bytes 120 >/dev/null
	"$(BIN)" --ansi strip --output stdout \
		--debug-cpuprofile "$(PERF_DIR)/adhoc/ansi-utf8.cpu.pprof" \
		--debug-memprofile "$(PERF_DIR)/adhoc/ansi-utf8.mem.pprof" \
		-- "$(BIN)" __test-producer --records 1000000 --line-bytes 120 --charset utf8 >/dev/null
	@echo "ad-hoc profiles: $(PERF_DIR)/adhoc"

flood-smoke: build
	rm -rf "$(PLATFORM_BUILD_DIR)/flood-smoke"
	mkdir -p "$(PLATFORM_BUILD_DIR)/flood-smoke/logs"
	"$(BIN)" --format json \
		--metadata source,line_end \
		--output "dir=$(PLATFORM_BUILD_DIR)/flood-smoke/logs" \
		--queue-records 65536 \
		--queue-bytes 64M \
		--debug-cpuprofile "$(PLATFORM_BUILD_DIR)/flood-smoke/cpu.pprof" \
		--debug-memprofile "$(PLATFORM_BUILD_DIR)/flood-smoke/mem.pprof" \
		-- "$(BIN)" __test-producer --records 1000000 --line-bytes 120
	@echo "logs: $(PLATFORM_BUILD_DIR)/flood-smoke/logs"
	@echo "cpu profile: $(PLATFORM_BUILD_DIR)/flood-smoke/cpu.pprof"
	@echo "mem profile: $(PLATFORM_BUILD_DIR)/flood-smoke/mem.pprof"

$(BIN): go.mod $(GO_SOURCES)
	mkdir -p "$(BIN_DIR)" "$(GOCACHE)" "$(GOMODCACHE)" "$(GOPATH)" "$(GOTMPDIR)" "$(GOTELEMETRYDIR)"
	go build -trimpath -o "$(BIN)" .

run: build
	"$(BIN)" $(ARGS)

install: build
	if [ "$$(id -u)" -eq 0 ]; then \
		mkdir -p /usr/local/bin; \
		cp "$(BIN)" "/usr/local/bin/$(APP)"; \
	else \
		mkdir -p "$$HOME/.local/bin"; \
		cp "$(BIN)" "$$HOME/.local/bin/$(APP)"; \
	fi

install-mutable: build
	if [ "$$(id -u)" -eq 0 ]; then \
		mkdir -p /usr/local/bin; \
		ln -sfn "$(BIN)" "/usr/local/bin/$(APP)"; \
	else \
		mkdir -p "$$HOME/.local/bin"; \
		ln -sfn "$(BIN)" "$$HOME/.local/bin/$(APP)"; \
	fi

clean:
	chmod -R u+w "$(BUILD_DIR)" 2>/dev/null || true
	rm -rf "$(BUILD_DIR)"

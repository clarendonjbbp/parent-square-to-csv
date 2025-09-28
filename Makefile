DIR := ${CURDIR}
E:=@
ifeq ($(V),1)
	E=
endif

############################################################################
# Vars
############################################################################

build_dir := $(DIR)/.build/

golangci_lint_version = v2.5.0
golangci_lint_dir = $(build_dir)/golangci_lint/$(golangci_lint_version)
golangci_lint_bin = $(golangci_lint_dir)/golangci-lint
golangci_lint_cache = $(golangci_lint_dir)/cache

############################################################################
# Install toolchain
############################################################################

$(golangci_lint_bin):
	@echo "Installing golangci-lint $(golangci_lint_version)..."
	$(E)rm -rf $(dir $(golangci_lint_dir))
	$(E)mkdir -p $(golangci_lint_dir)
	$(E)mkdir -p $(golangci_lint_cache)
	$(E)curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b $(golangci_lint_dir) $(golangci_lint_version)

#############################################################################
# Code cleanliness
#############################################################################

.PHONY: tidy tidy-check lint lint-code
tidy:
	$(E)go mod tidy

lint: lint-code

lint-code: $(golangci_lint_bin)
	$(E)if $(golangci_lint_bin) run ./...; then \
		: ; \
	else \
		ecode=$$?; \
		echo 1>&2 "golangci-lint failed with $$ecode; try make lint-fix or use $(golangci_lint_bin) to investigate"; \
		exit $$ecode; \
	fi

lint-fix: $(golangci_lint_bin)
	$(E)$(golangci_lint_bin) run --fix ./...

############################################################################
# Build targets
############################################################################

.PHONY: build

build:
	$(E)go build -o parent-square-to-csv ./cmd/parent-square-to-csv
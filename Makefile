E:=@
ifeq ($(V),1)
	E=
endif

.PHONY: build

build:
	$(E)go build -o parent-square-to-csv ./cmd/parent-square-to-csv
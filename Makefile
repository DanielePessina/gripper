.PHONY: build test install clean fmt vet

BINDIR ?= $(HOME)/.local/bin

build:
	go build -o bin/gripper ./cmd/gripper-tui

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

install: build
	install -d $(BINDIR)
	install -m 0755 bin/gripper $(BINDIR)/gripper
	install -m 0755 bin/gripper-fzf $(BINDIR)/gripper-fzf

clean:
	rm -f bin/gripper

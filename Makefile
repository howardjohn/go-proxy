build:
	@go generate ./...
	@go build -o bin ./pump ./bpf-server ./dump

.PHONY: bpf-server
bpf-server: build
	sudo ./bin/bpf-server

.PHONY:
envoy:
	@go build -o /tmp/grun $1 && echo "build complete" >&2 && sudo -u istio-proxy /tmp/grun

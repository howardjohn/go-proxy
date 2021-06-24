# go-proxy

## Benchmarks

8 connections (1 per core):
Direct: 2m
Envoy-tcp: 1.7m
Envoy-http: 34k


```
$GOPATH/src/istio.io/istio/out/linux_amd64/release/envoy -c envoy-http.yaml
curl -X POST -s "http://localhost:15000/cpuprofiler?enable=y"; sleep 15; curl -X POST -s "http://localhost:15000/cpuprofiler?enable=n"
pprof -http=localhost:8000 $GOPATH/src/istio.io/istio/out/linux_amd64/release/envoy /tmp/envoy.prof
```

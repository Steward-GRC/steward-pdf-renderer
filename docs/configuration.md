# Configuration

Both binaries read their settings from environment variables. A bad value stops the process at start
with a message naming the variable. `LOG_LEVEL` and `LOG_FORMAT` follow go-log (`trace` to `error`;
`json` or `console`).

## Operator

| Variable | Default | Meaning |
| --- | --- | --- |
| `RENDERER_IMAGE` | required | the renderer image every Job runs; set it to the image you publish |
| `S3_SECRET_NAME` | `steward-pdf-renderer-s3` | the Secret each Job loads its object-storage settings from (see the renderer below); the operator never reads it |
| `JOB_BACKOFF_LIMIT` | `3` | retries per Job before the render is `Failed` |
| `JOB_TTL_SECONDS` | `600` | how long a finished Job and its pod are kept |
| `WATCH_NAMESPACE` | empty | the one namespace to watch; empty watches every namespace and needs a ClusterRole |
| `LEADER_ELECT` | `true` | leader election between replicas |
| `PROBE_PORT` | `8080` | plain HTTP for `/livez` and `/readyz` |
| `METRICS_PORT` | `9090` | controller metrics; `0` turns them off |

The Kubernetes connection comes from the in-cluster service account, or `KUBECONFIG` outside a
cluster.

## Build arguments

Both Dockerfiles take `VERSION` (the image tag) and `COMMIT` (the full source SHA) and stamp them into
go-buildinfo:

```bash
docker build -f Dockerfile.operator \
  --build-arg VERSION=v0.1.0 --build-arg COMMIT="$(git rev-parse HEAD)" .
```

An unstamped build reports `dev`, and its commit falls back to Go's VCS stamp, then `unknown`.

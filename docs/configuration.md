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
| `JOB_SERVICE_ACCOUNT` | `steward-pdf-renderer` | the service account render pods run as (`deployments/operator/renderer-serviceaccount.yaml`, no RBAC); steward-delivery knows it as the caller `pdf-renderer` |
| `WATCH_NAMESPACE` | empty | the one namespace to watch; empty watches every namespace and needs a ClusterRole |
| `LEADER_ELECT` | `true` | leader election between replicas |
| `PROBE_PORT` | `8080` | plain HTTP for `/livez` and `/readyz` |
| `METRICS_PORT` | `9090` | controller metrics; `0` turns them off |

The Kubernetes connection comes from the in-cluster service account, or `KUBECONFIG` outside a
cluster.

## Renderer

The operator sets the render's variables on each Job; the object-storage ones come from the Secret
named by `S3_SECRET_NAME`, which the adopter creates in the render namespace.

| Variable | Set by | Meaning |
| --- | --- | --- |
| `FETCH_URL` | operator, required | `spec.fetchURL` |
| `OUTPUT_BUCKET` | operator, required | `spec.outputBucket` |
| `OUTPUT_KEY` | operator, required | `spec.outputKey` |
| `PV_ID` | operator | `spec.policyVersionId`, logged |
| `SENSITIVITY` | operator | `spec.sensitivity`; `sensitive` adds the watermark |
| `REQUESTED_BY` | operator | `spec.requestedBy`, printed in the watermark |
| `TRACE_ID` | operator | `spec.trace`, logged |
| `AWS_S3_ENDPOINT` | Secret | an S3-compatible endpoint; empty for AWS S3 |
| `AWS_REGION` | Secret | default `us-east-1` |
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` | Secret | static keys; when both are empty the SDK's default chain is used (workload identity, instance roles) |
| `AWS_S3_FORCE_PATH_STYLE` | Secret | `true` for path-style addressing (most self-hosted stores) |
| `WORKLOAD_TOKEN_FILE` | operator, required | `/var/run/secrets/steward/token`, the Job's projected service-account token (audience `steward`), sent on the HTML fetch |
| `WORKLOAD_AUTH` | by hand | `disabled` fetches with no token, for local runs only; it's the only accepted value |

## Workload token

steward-delivery's HTML endpoint needs the renderer's workload token. The operator gives every render
pod one:
- the pod runs as `JOB_SERVICE_ACCOUNT` and gets no Kubernetes API token;
- a projected service-account token with audience `steward`, valid for an hour and rotated by the
  kubelet, is mounted read-only at `/var/run/secrets/steward/token`;
- `WORKLOAD_TOKEN_FILE` points at it.

The renderer reads the file on every fetch and sends `Authorization: Bearer <token>`. A missing or
empty file fails the render before any request goes out. Without `WORKLOAD_TOKEN_FILE` the renderer
won't start, unless `WORKLOAD_AUTH=disabled`.

The fetch times out after 30 seconds and accepts at most 8 MiB; the render times out after 120
seconds. The renderer logs its version and commit when it starts.

## Build arguments

Both Dockerfiles take `VERSION` (the image tag) and `COMMIT` (the full source SHA) and stamp them into
go-buildinfo:

```bash
docker build -f Dockerfile.operator \
  --build-arg VERSION=v0.1.0 --build-arg COMMIT="$(git rev-parse HEAD)" .
```

An unstamped build reports `dev`, and its commit falls back to Go's VCS stamp, then `unknown`.

# Runbook

## Install

1. Apply the CRD: `kubectl apply -f deployments/crd/`.
2. Create the object-storage Secret the Jobs load (see [configuration](configuration.md)) in the
   operator's namespace.
3. Set the images in `deployments/operator/configmap.yaml` and `deployment.yaml`, then
   `kubectl apply -f deployments/operator/`.

The manifests are namespace-scoped: the operator watches its own namespace, and callers create
`PdfRender` resources there.

## Probes

- **`GET /livez`** on `PROBE_PORT` (8080) is 200 while the process is up. It never checks a
  dependency, so an API server outage doesn't restart the pods.
- **`GET /readyz`** is 503 while a required dependency is down and 200 otherwise. The one dependency
  is the Kubernetes API server (`kubernetes`, required), checked through its `/version` endpoint with
  a 2-second timeout and the result reused for 5 seconds. Readiness comes back by itself once the API
  server answers again.
- The `/readyz` body lists each dependency's state (`ok`, `degraded` or `down`), whether it's
  required, the error class, the last check time and its version (the API server's `gitVersion`).
- There is no `/health` and no `/healthz`.

## Build version

Both probe responses carry `Steward-Version` and `Steward-Commit`, and `/readyz` also carries
`Steward-Dep-Kubernetes` (the API server version) and `Steward-Depstate-Kubernetes`. Read them with:

```bash
kubectl port-forward deploy/steward-pdf-renderer-operator 8080 &
curl -si localhost:8080/readyz
```

The operator logs its version and commit when it starts.

## Calling other services

None. steward-delivery creates the `PdfRender` resources and reads their status through the
Kubernetes API, and the renderer fetches whatever `spec.fetchURL` points at (delivery's internal HTML
endpoint). Neither side imports the other's Go module; the contract is the CRD
([PdfRender](pdfrender.md)).

## A render is stuck or failed

- `kubectl get pdfrenders` shows each render's phase.
- `Failed` with an error: read the Job pod's logs (`kubectl logs job/<name>`) before the Job's TTL
  (`JOB_TTL_SECONDS`) removes it.
- A Job deleted while `Pending` is created again; one deleted while `Running` marks the render
  `Failed` ("underlying Job disappeared mid-render"), since the render's outcome is unknown.
- Deleting a `PdfRender` cancels it and removes its Job.
- In the renderer's log, the last message before the error says which step failed: `fetching html`,
  `rendered pdf` or `uploaded pdf`.

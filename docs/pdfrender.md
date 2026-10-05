# The PdfRender resource

`PdfRender` (group `renders.steward-grc.com`, version `v1alpha1`, namespaced) asks for one document
version to be rendered to PDF. One resource is one Job, one pod and one PDF.

steward-delivery creates the resources and watches their status through the Kubernetes API. The CRD
manifest is generated from the Go types in `api/v1alpha1` into
`deployments/crd/renders.steward-grc.com_pdfrenders.yaml`; install that file before the operator.

```yaml
apiVersion: renders.steward-grc.com/v1alpha1
kind: PdfRender
metadata:
  name: render-0001
spec:
  policyVersionId: pv-42
  fetchURL: http://delivery.example.org/versions/pv-42/html
  outputBucket: steward-artifacts
  outputKey: artifacts/pv-42/render-0001.pdf
  sensitivity: standard
  requestedBy: erin
status:
  phase: Succeeded
  outputURL: s3://steward-artifacts/artifacts/pv-42/render-0001.pdf
```

## Spec

| Field | Required | Meaning |
| --- | --- | --- |
| `policyVersionId` | yes | the document version being rendered; recorded for correlation, not fetched |
| `fetchURL` | yes | the URL the renderer GETs for the HTML; it must be reachable from the Job's pod |
| `outputBucket` | yes | the object-storage bucket for the PDF |
| `outputKey` | yes | the object key for the PDF; the caller keeps it unique |
| `sensitivity` | no | `standard` (default, no watermark) or `sensitive` (the requester and the time drawn diagonally on every page) |
| `requestedBy` | no | the requester's user id, printed in the sensitive watermark |
| `trace` | no | the caller's trace id, logged by the renderer |

The operator reads the spec once, when it creates the Job. Deleting the resource cancels the render
and removes the Job with it.

## Status

| Field | Meaning |
| --- | --- |
| `phase` | empty until first reconciled, then `Pending`, `Running`, and `Succeeded` or `Failed` |
| `outputURL` | `s3://<outputBucket>/<outputKey>` once `Succeeded` |
| `error` | a short reason once `Failed`; the detail is in the Job pod's logs |
| `startTime` | when the Job was created |
| `completeTime` | when the render reached `Succeeded` or `Failed` |
| `conditions` | `Progressing` while the Job runs, `Ready` when it ends (`True` on success) |

## Changing the types

Run `task generate` after editing `api/v1alpha1`. The `CRD` workflow regenerates the deepcopy code and
the manifest on every pull request and fails if they differ from what's committed.
`api/v1alpha1/crd_test.go` pins the names and fields steward-delivery relies on.

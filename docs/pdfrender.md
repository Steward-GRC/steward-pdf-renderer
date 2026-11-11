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
  organisation: Example Organisation
  documentNumber: POL-FACILITIES-000001
  documentVersion: v1.0.0
  documentStatus: Published
  documentOwner: Dave
  paperSize: A4
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
| `organisation` | no | the organisation's name, with its initials as a mark, in the running header |
| `documentNumber`, `documentVersion`, `documentStatus` | no | printed as "number · version · status" on the right of the running header |
| `documentOwner` | no | printed in the running footer as "owner · Printed date" |
| `paperSize` | no | `A4` (default) or `Letter` |
| `trace` | no | the caller's trace id, logged by the renderer |

Every page has 20 mm top and bottom and 18 mm left and right margins, a running header and a running
footer ("Steward · Page n of N" on the right). A field left empty is left out. The footer's print time
is in UTC.

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

# steward-pdf-renderer 🖨️

> 🧭 PDF render operator for Steward: a PdfRender CRD and a per-render headless Chromium job

steward-delivery asks for a PDF by creating a `PdfRender` resource. The operator turns each one into
a Kubernetes Job, and the Job's renderer fetches the document's HTML, prints it with headless
Chromium (with a watermark naming the requester and the time when the document is sensitive),
uploads the PDF to object storage and exits. The operator writes the outcome back to the
resource's status.

```text
steward-delivery ──creates──▶ PdfRender ──watched by──▶ operator ──creates──▶ Job ──runs──▶ renderer
```

One resource is one Job, one pod and one PDF. The operator calls no other Steward service.

## 🚀 Run

```bash
kubectl apply -f deployments/crd/
kubectl apply -f deployments/operator/   # set the two images first
kubectl apply -f deployments/samples/pdfrender.yaml
kubectl get pdfrenders -w
```

Build the images with `docker build -f Dockerfile.operator` and `-f Dockerfile.renderer`, passing
`--build-arg VERSION=<tag> --build-arg COMMIT=$(git rev-parse HEAD)`. Settings are in
[configuration](docs/configuration.md); the version and commit show up in the `Steward-Version` and
`Steward-Commit` headers of the operator's probes ([runbook](docs/runbook.md#probes)).

## 📚 Docs

- [The PdfRender resource](docs/pdfrender.md): spec, status and phases.
- [Configuration](docs/configuration.md): the operator and the renderer.
- [Runbook](docs/runbook.md): install, probes and failed renders.

- [Contributing](https://github.com/Steward-GRC/.github/blob/main/.github/CONTRIBUTING.md) and
  [security](https://github.com/Steward-GRC/.github/blob/main/.github/SECURITY.md)

## 🛠 Develop

```bash
task build     # go build ./...
task test      # go test ./... (the render test uses a local Chromium or Chrome when there is one)
task generate  # regenerate the deepcopy code and the CRD manifest from api/
task lint      # gofmt check + golangci-lint + yamllint
task license   # check Apache-2.0 headers (golic)
```

## 🙏 Acknowledgements

Steward was originally written by [@Bugs5382](https://github.com/Bugs5382).

## ⚖️ License

Apache-2.0 (c) 2026 The Steward Authors

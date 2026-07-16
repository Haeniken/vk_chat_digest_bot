# Repository instructions

These instructions apply to the entire repository.

## Before every commit

Run all checks from the repository root. Do not commit while any check fails.

1. Format all Go files and review the resulting diff:

   ```bash
   find . -type f -name '*.go' -not -path './.git/*' -print0 | xargs -0 gofmt -w
   git diff --check
   ```

2. Run the Go test and static-analysis suite:

   ```bash
   go test ./...
   go vet ./...
   golangci-lint run --timeout=10m --max-issues-per-linter=0 --max-same-issues=0
   ```

3. Re-run the checks after applying automatic or manual fixes.

Use the Go version declared by `go.mod` and the same `golangci-lint` minor version as `.github/workflows/quality.yml`. If the tools are not installed on the host, run them in disposable Docker containers.

For changes to dependencies, `Dockerfile`, or `docker-compose.yml`, also validate Compose, rebuild the application image, and run Trivy before committing:

```bash
docker compose config --quiet
docker compose build --pull app
trivy fs --scanners vuln,misconfig,secret --severity HIGH,CRITICAL .
trivy image --scanners vuln --severity HIGH,CRITICAL bot-summary-vk-app:latest
```

Never commit `.env`, credentials, tokens, database dumps, Trivy caches, or generated reports containing sensitive data.

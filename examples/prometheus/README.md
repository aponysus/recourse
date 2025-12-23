# Prometheus observer example

This example exposes recourse metrics via Prometheus and runs a sample call every two seconds.

## Run

```bash
go run .
```

Then scrape metrics:

```bash
curl -s http://localhost:2112/metrics | rg recourse
```

## Notes
- The example uses a local module replace to the repo root. Remove the `replace` directive in `go.mod` if you want to use the released module instead.

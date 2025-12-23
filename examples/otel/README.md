# OpenTelemetry observer example

This example emits a span per recourse call, including per-attempt events.
The exporter is configured to write spans to stdout.

## Run

```bash
go run .
```

You should see a span printed to stdout after the sample call completes.

## Notes
- The example uses a local module replace to the repo root. Remove the `replace` directive in `go.mod` if you want to use the released module instead.

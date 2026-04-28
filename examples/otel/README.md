# OpenTelemetry integration example

This example uses the first-class `github.com/aponysus/recourse/integrations/otel`
observer to emit a span per recourse call, including per-attempt events. The
exporter is configured to write spans to stdout.

## Run

```bash
go run .
```

You should see a span printed to stdout after the sample call completes.

## Notes
- The example uses local module replacements to the repo root and OTel integration.
  Remove the `replace` directives in `go.mod` if you want to use released modules instead.

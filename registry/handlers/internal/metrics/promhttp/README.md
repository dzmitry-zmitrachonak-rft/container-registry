This package contains a partial and modified version of the [github.com/prometheus/client_golang/prometheus/promhttp](https://pkg.go.dev/github.com/prometheus/client_golang@v1.10.0/prometheus/promhttp) package, version 1.10.0. This is a _temporary_ requirement (for the duration of [https://gitlab.com/groups/gitlab-org/-/epics/5523](https://gitlab.com/groups/gitlab-org/-/epics/5523)) so that we can inject a custom dynamic per-request label (`migration_path`) in all exported HTTP metrics.

We questioned the upstream `promhttp` package authors in their mailing list ([link](https://groups.google.com/g/prometheus-users/c/KzSDxJ5i1mI/m/hwnE-Y1uAwAJ)) on whether there was a way or a desire to make this customization possible. As we expected, the upstream `promhttp` package is meant to be a reference/non-customizable implementation for the most common use case (no per-request labels). Therefore this functionality is not supported/desired. As suggested in the linked mailing list thread, the alternative would be to reinvent the wheel and implement all of this in the registry application using the base Prometheus primitives. This would be too time consuming and error prone for something that is only required for a limited amount of time. Therefore, we decided to temporarily clone and modify the required files from the upstream package to meet our requirements.

See [https://gitlab.com/gitlab-org/container-registry/-/merge_requests/662](https://gitlab.com/gitlab-org/container-registry/-/merge_requests/662) for more details.

The files and modifications (if any) included in this package are described below:

- `delegator.go`: Copied from [https://github.com/prometheus/client_golang/blob/v1.10.0/prometheus/promhttp/delegator.go](https://github.com/prometheus/client_golang/blob/v1.10.0/prometheus/promhttp/delegator.go). Unmodified. This is required so that we can reference its non-exported functions/variables from `instrument_server.go`;

- `instrument_server.go`: Copied from [https://github.com/prometheus/client_golang/blob/v1.10.0/prometheus/promhttp/instrument_server.go](https://github.com/prometheus/client_golang/blob/v1.10.0/prometheus/promhttp/instrument_server.go). Modified: `InstrumentHandlerDuration`, `InstrumentHandlerCounter`, `InstrumentHandlerTimeToWriteHeader`, `InstrumentHandlerRequestSize` and `InstrumentHandlerResponseSize` functions add an additional label to exported metrics, named `migration_path`, whose value is dynamic (changes per request) and read from a custom `GitLab-Migration-Path` response header. The `checkLabels` and `labels` functions were changed accordingly to support this new label:
  
    ```diff
    13a14,23
    > //
    > // MODIFICATIONS TO UPSTREAM SOURCE:
    > //
    > // Copied from https://github.com/prometheus/client_golang/blob/v1.10.0/prometheus/promhttp/instrument_server.go and
    > // modified as follows: `InstrumentHandlerDuration`, `InstrumentHandlerCounter`, `InstrumentHandlerTimeToWriteHeader`,
    > // `InstrumentHandlerRequestSize` and `InstrumentHandlerResponseSize` functions were modified so that they add an
    > // additional label, named `migration_path`, whose value is dynamic (changes per request) and read from a custom
    > // `GitLab-Migration-Path` response header. The `checkLabels` and `labels` functions were changed accordingly to support
    > // this new label.
    >
    22a33,34
    >       "github.com/docker/distribution/registry/internal/migration"
    >
    70c82,83
    <                       obs.With(labels(code, method, r.Method, d.Status())).Observe(time.Since(now).Seconds())
    ---
    >                       migrationPath := w.Header().Get(migration.CodePathHeader)
    >                       obs.With(labels(code, method, r.Method, d.Status(), migrationPath)).Observe(time.Since(now).Seconds())
    77c90,91
    <               obs.With(labels(code, method, r.Method, 0)).Observe(time.Since(now).Seconds())
    ---
    >               migrationPath := w.Header().Get(migration.CodePathHeader)
    >               obs.With(labels(code, method, r.Method, 0, migrationPath)).Observe(time.Since(now).Seconds())
    102c116,117
    <                       counter.With(labels(code, method, r.Method, d.Status())).Inc()
    ---
    >                       migrationPath := w.Header().Get(migration.CodePathHeader)
    >                       counter.With(labels(code, method, r.Method, d.Status(), migrationPath)).Inc()
    108c123,124
    <               counter.With(labels(code, method, r.Method, 0)).Inc()
    ---
    >               migrationPath := w.Header().Get(migration.CodePathHeader)
    >               counter.With(labels(code, method, r.Method, 0, migrationPath)).Inc()
    137c153,154
    <                       obs.With(labels(code, method, r.Method, status)).Observe(time.Since(now).Seconds())
    ---
    >                       migrationPath := w.Header().Get(migration.CodePathHeader)
    >                       obs.With(labels(code, method, r.Method, status, migrationPath)).Observe(time.Since(now).Seconds())
    167c184,185
    <                       obs.With(labels(code, method, r.Method, d.Status())).Observe(float64(size))
    ---
    >                       migrationPath := w.Header().Get(migration.CodePathHeader)
    >                       obs.With(labels(code, method, r.Method, d.Status(), migrationPath)).Observe(float64(size))
    174c192,193
    <               obs.With(labels(code, method, r.Method, 0)).Observe(float64(size))
    ---
    >               migrationPath := w.Header().Get(migration.CodePathHeader)
    >               obs.With(labels(code, method, r.Method, 0, migrationPath)).Observe(float64(size))
    199c218,219
    <               obs.With(labels(code, method, r.Method, d.Status())).Observe(float64(d.Written()))
    ---
    >               migrationPath := w.Header().Get(migration.CodePathHeader)
    >               obs.With(labels(code, method, r.Method, d.Status(), migrationPath)).Observe(float64(d.Written()))
    261a282
    >               case "migration_path":
    293c314
    < func labels(code, method bool, reqMethod string, status int) prometheus.Labels {
    ---
    > func labels(code, method bool, reqMethod string, status int, migrationPath string) prometheus.Labels {
    304a326,328
    >       if migrationPath != "" {
    >               labels["migration_path"] = migrationPath
    >       }
    ```

- `NOTICE`: Copied from [https://github.com/prometheus/client_golang/blob/v1.10.0/NOTICE](https://github.com/prometheus/client_golang/blob/v1.10.0/NOTICE) for compliance reasons. Unmodified.

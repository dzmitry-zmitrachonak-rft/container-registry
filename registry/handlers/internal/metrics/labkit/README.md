This package contains a partial and modified version of the [gitlab.com/gitlab-org/labkit/metrics](https://pkg.go.dev/gitlab.com/gitlab-org/labkit/metrics) package, version 1.5.0. This is a _temporary_ requirement (for the duration of [https://gitlab.com/groups/gitlab-org/-/epics/5523](https://gitlab.com/groups/gitlab-org/-/epics/5523)) so that we can inject a custom dynamic per-request label (`migration_path`) in all exported HTTP metrics.

See [https://gitlab.com/gitlab-org/container-registry/-/merge_requests/662](https://gitlab.com/gitlab-org/container-registry/-/merge_requests/662) for more details.

The files and modifications (if any) included in this package are described below:

- `handler_factory_options.go`: Copied from [https://gitlab.com/gitlab-org/labkit/-/blob/v1.5.0/metrics/handler_factory_options.go](https://gitlab.com/gitlab-org/labkit/-/blob/v1.5.0/metrics/handler_factory_options.go). Unmodified. Included so that their contents can be referenced from `handler.go`;

- `handler_options.go`: Copied from [https://gitlab.com/gitlab-org/labkit/-/blob/v1.5.0/metrics/handler_options.go](https://gitlab.com/gitlab-org/labkit/-/blob/v1.5.0/metrics/handler_options.go). Unmodified. Included so that their contents can be referenced from `handler.go`;

- `handler.go`: Copied from [https://gitlab.com/gitlab-org/labkit/-/blob/v1.5.0/metrics/handler.go](https://gitlab.com/gitlab-org/labkit/-/blob/v1.5.0/metrics/handler.go). Modified. The `github.com/prometheus/client_golang/prometheus/promhttp` import was replaced with `github.com/docker/distribution/registry/handlers/internal/metrics/promhttp`:

    ```diff
    1c1
    < package metrics
    ---
    > package labkit
    5a6
    > 	"github.com/docker/distribution/registry/handlers/internal/metrics/promhttp"
    7d7
    < 	"github.com/prometheus/client_golang/prometheus/promhttp"
    ```

See [registry/handlers/internal/metrics/promhttp/README.md](../promhttp/README.md) for an explanation of the changes behind `github.com/docker/distribution/registry/handlers/internal/metrics/promhttp`.
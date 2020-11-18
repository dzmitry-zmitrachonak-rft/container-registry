## [Unreleased]
## Deprecated
- configuration: Deprecate NewRelic support, to be removed by January 22nd, 2021
- configuration: Deprecate logstash and combined log formats, to be removed by January 22nd, 2021
- registry/api: Deprecate Docker Schema v1 compatibility, to be removed by January 22nd, 2021
- configuration: Deprecate TLS 1.0 and TLS 1.1 support, to be removed by January 22nd, 2021

## Added
- registry: Experimental PostgreSQL metadata database (incomplete, in progress)
- registry: Use GitLab LabKit for HTTP metrics collection
- registry/storage/cache/redis: Add Prometheus metrics for Redis cache store
- registry: Add support for a pprof monitoring server
- registry: Add TLS support for Redis
- registry: Add support for Redis Sentinel
- registry: Add support for error reporting with Sentry

### Changed
- configuration: Cloudfront middleware `ipfilteredby` setting is now optional

### Fixed
- registry/api/v2: Text-charset selector removed from `application/json` content-type
- registry/storage: Swift path generation now generates multiple directories as intended
- registry/client/auth: OAuth token authentication now returns a `ErrNoToken` if a token is not found in the response
- registry/storage: Fix custom User-Agent header on S3 requests

## [Prerelease]
## Deprecated
- configuration: Deprecate log hooks, to be removed by January 22nd, 2021
- configuration: Deprecate Bugsnag support, to be removed by January 22nd, 2021

## [v2.11.0-gitlab] - 2020-09-08
## Added
- registry: Add new configuration for changing the output for logs and the access logs format

## Changed
- registry: Use GitLab LabKit for correlation and logging
- registry: Normalize log messages

## [v2.10.0-gitlab] - 2020-08-05
## Added
- registry: Add support for continuous profiling with Google Stackdriver

## [v2.9.1-gitlab] - 2020-05-05
## Added
- registry/api/v2: Show version and supported extra features in custom headers

## Changed
- registry/handlers: Encapsulate the value of err.detail in logs in a JSON object

### Fixed
- registry/storage: Fix panic during uploads purge

## [v2.9.0-gitlab] - 2020-04-07
### Added
- notifications: Notification related Prometheus metrics
- registry: Make minimum TLS version user configurable
- registry/storage: Support BYOK for OSS storage driver

### Changed
- Upgrade to Go 1.13
- Switch to Go Modules for dependency management
- registry/handlers: Log authorized username in push/pull requests

### Fixed
- configuration: Fix pointer initialization in configuration parser
- registry/handlers: Process Accept header MIME types in case-insensitive way

## [v2.8.2-gitlab] - 2020-03-13
### Changed
- registry/storage: Improve performance of the garbage collector for GCS
- registry/storage: Gracefully handle missing tags folder during garbage collection
- registry/storage: Cache repository tags during the garbage collection mark phase
- registry/storage: Upgrade the GCS SDK to v1.2.1
- registry/storage: Provide an estimate of how much storage will be removed on garbage collection
- registry/storage: Make the S3 driver log level configurable
- registry/api/v2: Return not found error when getting a manifest by tag with a broken link

### Fixed
- registry/storage: Fix PathNotFoundError not being ignored in repository enumeration during garbage collection when WalkParallel is enabled

## v2.8.1-gitlab

- registry/storage: Improve consistency of garbage collection logs

## v2.8.0-gitlab

- registry/api/v2: Add tag delete route

## v2.7.8-gitlab

- registry/storage: Improve performance of the garbage collection algorithm for S3

## v2.7.7-gitlab

- registry/storage: Handle bad link files gracefully during garbage collection
- registry/storage: AWS SDK v1.26.3 update
- registry: Include build info on Prometheus metrics

## v2.7.6-gitlab

- CI: Add integration tests for the S3 driver
- registry/storage: Add compatibilty for S3v1 ListObjects key counts

## v2.7.5-gitlab

- registry/storage: Log a message if PutContent is called with 0 bytes

## v2.7.4-gitlab

- registry/storage: Fix Google Cloud Storage client authorization with non-default credentials
- registry/storage: Fix error handling of GCS Delete() call when object does not exist

## v2.7.3-gitlab

- registry/storage: Update to Google SDK v0.47.0 and latest storage driver (v1.1.1)

## v2.7.2-gitlab

- registry/storage: Use MD5 checksums in the registry's Google storage driver

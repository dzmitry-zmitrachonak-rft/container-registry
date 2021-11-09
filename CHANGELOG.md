## [3.14.3](https://gitlab.com/gitlab-org/container-registry/compare/v3.14.2-gitlab...v3.14.3-gitlab) (2021-11-09)


### Bug Fixes

* **datastore:** use "safe find or create" instead of "create or find" for namespaces ([0feff9e](https://gitlab.com/gitlab-org/container-registry/commit/0feff9edcd99fd7afc9a5c5e71fc1e161915e36d))
* **datastore:** use "safe find or create" instead of "create or find" for repositories ([7b73cc9](https://gitlab.com/gitlab-org/container-registry/commit/7b73cc986d8855e42d8890801d62bb82b9c07df5))

## [3.14.2](https://gitlab.com/gitlab-org/container-registry/compare/v3.14.1-gitlab...v3.14.2-gitlab) (2021-11-03)


### Bug Fixes

* **gc:** commit database transaction when no task was found ([2f4e2f9](https://gitlab.com/gitlab-org/container-registry/commit/2f4e2f949b3194358eb9dd3a0b5eb49a8b0d9398))

## [3.14.1](https://gitlab.com/gitlab-org/container-registry/compare/v3.14.0-gitlab...v3.14.1-gitlab) (2021-10-29)


### Performance Improvements

* **handlers:** improve performance of repository existence check for GCS ([e31e5ed](https://gitlab.com/gitlab-org/container-registry/commit/e31e5ed5993ca8496e6801a4f833100e85f5f005))

# [3.14.0](https://gitlab.com/gitlab-org/container-registry/compare/v3.13.0-gitlab...v3.14.0-gitlab) (2021-10-28)


### Bug Fixes

* **handlers:** do not log when blob or manifest HEAD requests return not found errors ([0f407e3](https://gitlab.com/gitlab-org/container-registry/commit/0f407e3cebc933cc908109a52417ff89501998fa))
* **handlers:** use 503 Service Unavailable for DB connection failures ([fecb78d](https://gitlab.com/gitlab-org/container-registry/commit/fecb78d804d3b5717c93567da3fe4a000dc68630))


### Features

* **handlers:** log when migration status is determined ([75b8230](https://gitlab.com/gitlab-org/container-registry/commit/75b8230ad3080d576f6b7564c29e435c3f0e1d0e))
* **handlers/configuration:** enable enforcing manifest reference limits ([2154e73](https://gitlab.com/gitlab-org/container-registry/commit/2154e7308f863ec27a8c454ca60a859afc9b4fd5))

# [3.13.0](https://gitlab.com/gitlab-org/container-registry/compare/v3.12.0-gitlab...v3.13.0-gitlab) (2021-10-14)


### Bug Fixes

* update Dockerfile dependencies to allow successful builds ([3dc2f1a](https://gitlab.com/gitlab-org/container-registry/commit/3dc2f1a29270534a65daff4b986a75ff2bbd87f7))


### Features

* **configuration:** use structured logging in configuration parser ([49d7d10](https://gitlab.com/gitlab-org/container-registry/commit/49d7d10116836eacdacc44738f7123f6ceebe5ae))
* **datastore/handlers:** cache repository objects in memory for manifest PUT requests ([66bd599](https://gitlab.com/gitlab-org/container-registry/commit/66bd599dd16a2fc3046f58325c165be312783088))

# [3.12.0](https://gitlab.com/gitlab-org/container-registry/compare/v3.11.1-gitlab...v3.12.0-gitlab) (2021-10-11)


### Bug Fixes

* **handlers:** only log that a manifest/blob was downloaded if the method is GET ([19d9f60](https://gitlab.com/gitlab-org/container-registry/commit/19d9f608960990dbfe12209f5743ac30776b1988))


### Features

* **datastore:** add created_at timestamp to online GC review queue tables ([3a38a0a](https://gitlab.com/gitlab-org/container-registry/commit/3a38a0aef96d607730b8ba0d728d702477c32331))
* **datastore:** calculate total manifest size on creation ([4c38d53](https://gitlab.com/gitlab-org/container-registry/commit/4c38d53adc3fd09fd9f899f90ab0c34b375143be))
* **handlers:** log metadata when a new blob is uploaded ([83cb07e](https://gitlab.com/gitlab-org/container-registry/commit/83cb07e98c55ee1eee00c5c31b9c668d59a7ba22))

## [3.11.1](https://gitlab.com/gitlab-org/container-registry/compare/v3.11.0-gitlab...v3.11.1-gitlab) (2021-09-20)


### Bug Fixes

* **api/errcode:** extract enclosed error from a storage driver catch-all error ([800a15e](https://gitlab.com/gitlab-org/container-registry/commit/800a15e282f79ed41ef0c3606a3303224cd176d1))
* **api/errcode:** propagate 503 Service Unavailable status thrown by CGS ([21041fe](https://gitlab.com/gitlab-org/container-registry/commit/21041fef6f3e863d598d6bb97476815ae2518e38))
* **gc:** always propagate correlation ID from agent to workers ([8de9c93](https://gitlab.com/gitlab-org/container-registry/commit/8de9c936d52e4ab84bb40efae63ea15371d70bc3))
* **gc/worker:** delete task if dangling manifest no longer exists on database ([dffdd72](https://gitlab.com/gitlab-org/container-registry/commit/dffdd72d6527dfc037fc6bcbda2530ac83c9fe4b))
* **handlers:** ignore tag not found errors when deleting a manifest ([e740416](https://gitlab.com/gitlab-org/container-registry/commit/e74041697ccfb178749bd1b89395c2f07b2aee02))

# [3.11.0](https://gitlab.com/gitlab-org/container-registry/compare/v3.10.1-gitlab...v3.11.0-gitlab) (2021-09-10)


### Bug Fixes

* **handlers:** use 400 Bad Request status for canceled requests ([30428c6](https://gitlab.com/gitlab-org/container-registry/commit/30428c69c3670ed5102e4692abf51b98ae2cf6c2))
* **log:** use same logger key between both logging packages ([04e2f68](https://gitlab.com/gitlab-org/container-registry/commit/04e2f68a6796222103da8a43eb6dfd06440f24cb))
* **storage:** provide detailed error when blob enumeration cannot parse digest from path ([f8d9d40](https://gitlab.com/gitlab-org/container-registry/commit/f8d9d40a5f664dc39c4b3182c5de601df6d17897))


### Features

* use ISO 8601 with millisecond precision as timestamp format ([2c56935](https://gitlab.com/gitlab-org/container-registry/commit/2c56935a69b04d21abc32dc6352ff7cb08e7b8c6))
* **handlers:** log metadata when a blob is downloaded ([ca37bff](https://gitlab.com/gitlab-org/container-registry/commit/ca37bffbe0d6803eb2c4f625375638bbf2df4fc0))
* **handlers:** use structured logging throughout ([97975de](https://gitlab.com/gitlab-org/container-registry/commit/97975dee9197f7e6ba078ca8cb5b64cbcd44fc7b))
* configurable expiry delay for storage backend presigned URLs ([820052a](https://gitlab.com/gitlab-org/container-registry/commit/820052a2ce9eed84746e80802c8fee37f2019394))

## [3.10.1](https://gitlab.com/gitlab-org/container-registry/compare/v3.10.0-gitlab...v3.10.1-gitlab) (2021-09-03)


### Bug Fixes

* set prepared statements option for the CLI DB client ([1cc1716](https://gitlab.com/gitlab-org/container-registry/commit/1cc1716f8eb92b55cc462f23e32ff3f346ee6575))
* **configuration:** require rootdirectory to be set when in migration mode ([6701c98](https://gitlab.com/gitlab-org/container-registry/commit/6701c98e71a1bff26f30492f4965fc8d91754163))
* **gc:** improve handling of database errors and review postponing ([e359925](https://gitlab.com/gitlab-org/container-registry/commit/e3599255528f29f0a0cc8d75c6ee0b16122fbce2))

# [3.10.0](https://gitlab.com/gitlab-org/container-registry/compare/v3.9.0-gitlab...v3.10.0-gitlab) (2021-08-23)


### Features

* **handlers:** log metadata when a manifest is uploaded or downloaded ([e078c9f](https://gitlab.com/gitlab-org/container-registry/commit/e078c9f5b2440193157c04ddbd101b5e04fddd32))
* **handlers:** log warning when eligibility flag is not set in migration mode ([fe78327](https://gitlab.com/gitlab-org/container-registry/commit/fe78327bc212b8089b2a8479eb458ec5a111c747))

# [3.9.0](https://gitlab.com/gitlab-org/container-registry/compare/v3.8.0-gitlab...v3.9.0-gitlab) (2021-08-18)


### Features

* **handlers:** enable migration mode to be paused ([9d43c34](https://gitlab.com/gitlab-org/container-registry/commit/9d43c34b6323533ae99e53de81286a392a5a9635))

# [3.8.0](https://gitlab.com/gitlab-org/container-registry/compare/v3.7.0-gitlab...v3.8.0-gitlab) (2021-08-17)


### Bug Fixes

* **handlers:** deny pushes for manifest lists with blob references except manifest cache images ([79e854a](https://gitlab.com/gitlab-org/container-registry/commit/79e854aabd25278760eb17e9e5507b180cf89cf0))
* **handlers:** enable cross repository blob mounts without FS mirroring ([98fe521](https://gitlab.com/gitlab-org/container-registry/commit/98fe521be56204aaaf330294a4e9bb7ccb2fb875))
* **handlers:** handle blob not found errors when serving head requests ([2492b4e](https://gitlab.com/gitlab-org/container-registry/commit/2492b4e4f060adec5215f17c8bf65a196edbd73b))
* **storage:** never write blob links when FS mirroring is disabled ([0786b77](https://gitlab.com/gitlab-org/container-registry/commit/0786b77573ea94d9b206012d2395d395470dabae))


### Features

* **datastore:** allow removing a connection from the pool after being idle for a period of time ([0352cc3](https://gitlab.com/gitlab-org/container-registry/commit/0352cc3fba180f277a9203a33cd936dc13ffd976))
* **storage/driver/s3-aws:** add IRSA auth support ([de69331](https://gitlab.com/gitlab-org/container-registry/commit/de693316925e3327a3b2ddf990233e8db640d6f7))


### Performance Improvements

* **handlers:** lookup single blob link instead of looping over all ([46f1642](https://gitlab.com/gitlab-org/container-registry/commit/46f16420d2fdffd96c230db6e36ef17f9750e9d5))

# [3.7.0](https://gitlab.com/gitlab-org/container-registry/compare/v3.6.2-gitlab...v3.7.0-gitlab) (2021-08-06)


### Bug Fixes

* **auth/token:** fix migration eligibility validation for read requests ([6576897](https://gitlab.com/gitlab-org/container-registry/commit/657689735237b769b95eeb5b91e466675578473d))
* **handlers:** handle Buildkit index as an OCI manifest when using the database ([556ab04](https://gitlab.com/gitlab-org/container-registry/commit/556ab04690b8962bfc2aa386fefb0bd3a3a12d06))
* use MR diff SHA in commitlint job ([f66bccb](https://gitlab.com/gitlab-org/container-registry/commit/f66bccb9ef2449833f3eaa7327a0519c2776f42b))
* **handlers:** default to the schema 2 parser instead of schema 1 for manifest uploads ([0c62ea4](https://gitlab.com/gitlab-org/container-registry/commit/0c62ea47f801716ec3ee62eb7a37af2fd4b73115))
* **handlers:** display error details when invalid media types are detected during a manifest push ([f750297](https://gitlab.com/gitlab-org/container-registry/commit/f750297224ecd1f7549af484ef5a380caaf8aef4))
* **handlers:** fallback to OCI media type for manifests with no payload media type ([854f3ad](https://gitlab.com/gitlab-org/container-registry/commit/854f3adc937cd089e04926c1a4212249a17de84b))
* **handlers:** migration_path label should be logged as string ([524a614](https://gitlab.com/gitlab-org/container-registry/commit/524a6141201f15ada63bcfeae5d74375e0d7f558))
* **handlers:** return 400 Bad Request when saving a manifest with unknown media types on the DB ([0a39980](https://gitlab.com/gitlab-org/container-registry/commit/0a39980a0a6d08d040487a025746f5bed8e9c7de))
* **storage/driver/azure:** give deeply nested errors more context ([3388e1d](https://gitlab.com/gitlab-org/container-registry/commit/3388e1dc8ce1ae122e1069fa546ef165c1c13c52))


### Features

* **storage:** instrument blob download size and redirect option ([f091ff9](https://gitlab.com/gitlab-org/container-registry/commit/f091ff99dd5b9cf51c52964174dcfbe65f93c43a))


### Performance Improvements

* **storage:** do not descend into hashstates directors during upload purge ([b46d563](https://gitlab.com/gitlab-org/container-registry/commit/b46d56383df4a4814c9f2ef9a5612db27fd66ae9))

## [3.6.2](https://gitlab.com/gitlab-org/container-registry/compare/v3.6.1-gitlab...v3.6.2-gitlab) (2021-07-29)

### Bug Fixes

* **handlers:** always add the migration_path label to HTTP metrics ([b152880](https://gitlab.com/gitlab-org/container-registry/commit/b1528807556dee9c92541256dc6688ded1a4979e))
* **handlers:** reduce noise from client disconnected errors during uploads ([61478d7](https://gitlab.com/gitlab-org/container-registry/commit/61478d74aa512c16bf1ff74282e3a23ee86566ea))
* **handlers:** set correct config media type when saving manifest on DB ([00f2c95](https://gitlab.com/gitlab-org/container-registry/commit/00f2c95901d59f36d781e98feaa9e30a0912686f))
* **storage:** return ErrManifestEmpty when zero-lenth manifest content is encountered ([1ad342b](https://gitlab.com/gitlab-org/container-registry/commit/1ad342becb5f7e2f93e1600c9842e99b7efa474a))

### Performance Improvements

* **handlers:** only read config from storage if manifest does not exist in DB ([8851793](https://gitlab.com/gitlab-org/container-registry/commit/8851793f2b06b1a15e908c897c32cae8ac318b36))

### Build System

* upgrade aliyungo dependency ([2d44f17](https://gitlab.com/gitlab-org/container-registry/commit/2d44f176f396013a27ecccf6367d1aefe5ce11a2))
* upgrade aws-sdk-go dependency to 1.40.7 ([e48843c](https://gitlab.com/gitlab-org/container-registry/commit/e48843c6716ce0c2ba9bd1bc6f3a43bde40ee8ef))
* upgrade backoff/v4 dependency to 4.1.1 ([abcb620](https://gitlab.com/gitlab-org/container-registry/commit/abcb6205f55f44ab18c0c5953fd2d3bbdcf8b41d))
* upgrade clock dependency to 1.1.0 ([15c8463](https://gitlab.com/gitlab-org/container-registry/commit/15c8463a45fb9465d06e440a04c214b9ff1949e6))
* upgrade cobra dependency to 1.2.1 ([fed057e](https://gitlab.com/gitlab-org/container-registry/commit/fed057e3a713da4027ff13862f82d038207e3a29))
* upgrade docker/libtrust dependency ([16adbf0](https://gitlab.com/gitlab-org/container-registry/commit/16adbf06d575ffe7a062654cfa8a3e0676ca170c))
* upgrade go-metrics dependency to 0.0.1 ([3b4eae0](https://gitlab.com/gitlab-org/container-registry/commit/3b4eae06c28990c068869ec64b2401310c80a487))
* upgrade golang.org/x/time dependency ([15b708c](https://gitlab.com/gitlab-org/container-registry/commit/15b708c07fe427460530664edcc6f2c05fc18177))
* upgrade labkit dependency to 1.6.0 ([d56a536](https://gitlab.com/gitlab-org/container-registry/commit/d56a5364eaab13afda721512093475c65ab77a92))
* upgrade opencontainer image-spec dependency to 1.0.1 ([c750921](https://gitlab.com/gitlab-org/container-registry/commit/c7509218195bcafd7492c3af1ec077860eb0ba6b))
* upgrade pgconn dependency to 1.10.0 ([756cf1b](https://gitlab.com/gitlab-org/container-registry/commit/756cf1b7c3ed38893bc3d29d0b489e1d3d3b1cb8))
* upgrade pgx/v4 dependency to 4.13.0 ([b3ed0df](https://gitlab.com/gitlab-org/container-registry/commit/b3ed0df30bda917aee9adc4021d3601e0a2edf0d))
* upgrade sentry-go dependency to 0.11.0 ([b1ec39f](https://gitlab.com/gitlab-org/container-registry/commit/b1ec39f9769ce608032b3bcd82254a72615944c3))
* upgrade sql-migrate dependency ([d00429e](https://gitlab.com/gitlab-org/container-registry/commit/d00429e453faadef4d57fa3965506da82cdf612a))

## [v3.6.1-gitlab] - 2021-07-23
### Changed
- registry/storage: Upgrade the GCS SDK to v1.16.0

### Fixed
- registry/storage: Offline garbage collection will continue if it cannot find a manifest referenced by a manifest list.

## [v3.6.0-gitlab] - 2021-07-20
### Changed
- registry/api/v2: Return 400 - Bad Request when client closes the connection, rather than returning 500 - Internal Server Error
- registry/storage: Upgrade Amazon S3 SDK to v1.40.3

## [v3.5.2-gitlab] - 2021-07-13
### Fixed
- registry/api/v2: Attempting to read a config through the manifests endpoint will now return a not found error instead of an internal server error.

## [v3.5.1-gitlab] - 2021-07-09
### Removed
- configuration: Remove proxy configuration migration section
- registry: Remove ability to migrate to remote registry

### Fixed
- registry/storage: Offline garbage collection now appropriately handles docker buildx cache manifests

### Added
- registry/api/v2: Log a warning when encountering a manifest list with blob references

## [v3.5.0-gitlab] - 2021-06-10
### Changed
- registry/datastore: Partitioning by top-level namespace

### Fixed
- registry/storage: Offline garbage collection no longer inappropriately removes untagged manifests referenced by a manifest list

### Added
- registry/storage: S3 Driver will now use Exponential backoff to retry failed requests

## [v3.4.1-gitlab] - 2021-05-11
### Fixed
- registry/storage: S3 driver now respects rate limits in all cases

### Changed
- registry/storage: Upgrade Amazon S3 SDK to v1.38.26
- registry/storage: Upgrade golang.org/x/time to v0.0.0-20210220033141-f8bda1e9f3ba
- registry: Upgrade github.com/opencontainers/go-digest to v1.0.0
- registry/storage: Upgrade Azure SDK to v54.1.0

## [v3.4.0-gitlab] - 2021-04-26
### Changed
- registry/datastore: Switch from 1 to 64 partitions per table

### Fixed
- registry: Log operating system quit signal as string

### Added
- registry/gc: Add Prometheus counter and histogram for online GC runs
- registry/gc: Add Prometheus counter and histogram for online GC deletions
- registry/gc: Add Prometheus counter for online GC deleted bytes
- registry/gc: Add Prometheus counter for online GC review postpones
- registry/gc: Add Prometheus histogram for sleep durations between online GC runs
- registry/gc: Add Prometheus gauge for the online GC review queues size

## [v3.3.0-gitlab] - 2021-04-09
### Added
- registry: Add Prometheus counter for database queries

### Changed
- registry/storage: Upgrade Azure SDK to v52.5.0

## [v3.2.1-gitlab] - 2021-03-17
### Fixed
- configuration: Don't require storage section for the database migrate CLI

## [v3.2.0-gitlab] - 2021-03-15
### Added
- configuration: Add `rootdirectory` option to the azure storage driver
- configuration: Add `trimlegacyrootprefix` option to the azure storage driver

## [v3.1.0-gitlab] - 2021-02-25
### Added
- configuration: Add `preparedstatements` option to toggle prepared statements for the metadata database
- configuration: Add `draintimeout` to database stanza to set optional connection close timeout on shutdown
- registry/api/v2: Disallow manifest delete if referenced by manifest lists (metadata database only).
- registry: Add CLI flag to facilitate programmatic state checks for database migrations
- registry: Add continuous online garbage collection

### Changed
- registry/datastore: Metadata database does not use prepared statements by default

## [v3.0.0-gitlab] - 2021-01-20
### Added
- registry: Experimental PostgreSQL metadata database (disabled by default)
- registry/storage/cache/redis: Add size and maxlifetime pool settings

### Changed
- registry/storage: Upgrade Swift client to v1.0.52

### Fixed
- registry/api: Fix tag delete response body

### Removed
- configuration: Drop support for TLS 1.0 and 1.1 and default to 1.2
- registry/storage/cache/redis: Remove maxidle and maxactive pool settings
- configuration: Drop support for logstash and combined log formats and default to json
- configuration: Drop support for log hooks
- configuration: Drop NewRelic reporting support
- configuration: Drop Bugsnag reporting support
- registry/api/v2: Drop support for schema 1 manifests and default to schema 2

## [v2.13.1-gitlab] - 2021-01-13
### Fixed
- registry: Fix HTTP request duration and byte size Prometheus metrics buckets

## [v2.13.0-gitlab] - 2020-12-15
### Added
- registry: Add support for a pprof monitoring server
- registry: Use GitLab LabKit for HTTP metrics collection
- registry: Expose build info through the Prometheus metrics

### Changed
- configuration: Improve error reporting when `storage.redirect` section is misconfigured
- registry/storage: Upgrade the GCS SDK to v1.12.0

### Fixed
- registry: Fix support for error reporting with Sentry

## [v2.12.0-gitlab] - 2020-11-23
### Deprecated
- configuration: Deprecate log hooks, to be removed by January 22nd, 2021
- configuration: Deprecate Bugsnag support, to be removed by January 22nd, 2021
- configuration: Deprecate NewRelic support, to be removed by January 22nd, 2021
- configuration: Deprecate logstash and combined log formats, to be removed by January 22nd, 2021
- registry/api: Deprecate Docker Schema v1 compatibility, to be removed by January 22nd, 2021
- configuration: Deprecate TLS 1.0 and TLS 1.1 support, to be removed by January 22nd, 2021

### Added
- registry: Add support for error reporting with Sentry
- registry/storage/cache/redis: Add Prometheus metrics for Redis cache store
- registry: Add TLS support for Redis
- registry: Add support for Redis Sentinel
- registry: Enable toggling redirects to storage backends on a per-repository basis

### Changed
- configuration: Cloudfront middleware `ipfilteredby` setting is now optional

### Fixed
- registry/storage: Swift path generation now generates multiple directories as intended
- registry/client/auth: OAuth token authentication now returns a `ErrNoToken` if a token is not found in the response
- registry/storage: Fix custom User-Agent header on S3 requests
- registry/api/v2: Text-charset selector removed from `application/json` content-type

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

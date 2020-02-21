package main

import (
	_ "net/http/pprof"

	"gitlab.com/gitlab-org/container-registry/registry"
	_ "gitlab.com/gitlab-org/container-registry/registry/auth/htpasswd"
	_ "gitlab.com/gitlab-org/container-registry/registry/auth/silly"
	_ "gitlab.com/gitlab-org/container-registry/registry/auth/token"
	_ "gitlab.com/gitlab-org/container-registry/registry/proxy"
	_ "gitlab.com/gitlab-org/container-registry/registry/storage/driver/azure"
	_ "gitlab.com/gitlab-org/container-registry/registry/storage/driver/filesystem"
	_ "gitlab.com/gitlab-org/container-registry/registry/storage/driver/gcs"
	_ "gitlab.com/gitlab-org/container-registry/registry/storage/driver/inmemory"
	_ "gitlab.com/gitlab-org/container-registry/registry/storage/driver/middleware/cloudfront"
	_ "gitlab.com/gitlab-org/container-registry/registry/storage/driver/middleware/redirect"
	_ "gitlab.com/gitlab-org/container-registry/registry/storage/driver/oss"
	_ "gitlab.com/gitlab-org/container-registry/registry/storage/driver/s3-aws"
	_ "gitlab.com/gitlab-org/container-registry/registry/storage/driver/swift"
)

func main() {
	registry.RootCmd.Execute()
}

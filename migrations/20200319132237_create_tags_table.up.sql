CREATE TABLE IF NOT EXISTS tags
(
    id               bigint                   NOT NULL GENERATED BY DEFAULT AS IDENTITY,
    repository_id    bigint                   NOT NULL,
    manifest_id      bigint,
    created_at       timestamp with time zone NOT NULL DEFAULT now(),
    updated_at       timestamp with time zone,
    name             text                     NOT NULL,
    CONSTRAINT pk_tags PRIMARY KEY (id),
    CONSTRAINT fk_tags_repository_id_repositories FOREIGN KEY (repository_id)
        REFERENCES repositories (id) ON DELETE CASCADE,
    CONSTRAINT fk_tags_manifest_id_manifests FOREIGN KEY (manifest_id)
        REFERENCES manifests (id) ON DELETE CASCADE,
    CONSTRAINT uq_tags_name_repository_id UNIQUE (name, repository_id),
    CONSTRAINT ck_tags_name_length CHECK ((char_length(name) <= 255))
);
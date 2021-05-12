CREATE TABLE top_level_namespaces (
  id bigint NOT NULL GENERATED BY DEFAULT AS IDENTITY,
  created_at timestamp WITH time zone NOT NULL DEFAULT now(),
  updated_at timestamp WITH time zone,
  name text NOT NULL,
  CONSTRAINT pk_top_level_namespaces PRIMARY KEY (id),
  CONSTRAINT unique_top_level_namespaces_name UNIQUE (name),
  CONSTRAINT check_top_level_namespaces_name_length CHECK ((char_length(name) <= 255))
);

CREATE TABLE repositories (
    id bigint NOT NULL GENERATED BY DEFAULT AS IDENTITY,
    top_level_namespace_id bigint NOT NULL,
    parent_id bigint,
    created_at timestamp WITH time zone NOT NULL DEFAULT now(),
    updated_at timestamp WITH time zone,
    name text NOT NULL,
    path text NOT NULL,
    CONSTRAINT pk_repositories PRIMARY KEY (top_level_namespace_id, id),
    CONSTRAINT fk_repositories_top_level_namespace_id_top_level_namespaces FOREIGN KEY (top_level_namespace_id) REFERENCES top_level_namespaces (id) ON DELETE CASCADE,
    CONSTRAINT fk_repositories_top_lvl_namespace_id_and_parent_id_repositories FOREIGN KEY (top_level_namespace_id, parent_id) REFERENCES repositories (top_level_namespace_id, id) ON DELETE CASCADE,
    CONSTRAINT unique_repositories_top_level_namespace_id_and_path UNIQUE (top_level_namespace_id, path),
    CONSTRAINT check_repositories_name_length CHECK ((char_length(name) <= 255)),
    CONSTRAINT check_repositories_path_length CHECK ((char_length(path) <= 255))
);

CREATE INDEX index_repositories_on_top_level_namespace_id ON repositories USING btree (top_level_namespace_id);

CREATE INDEX index_repositories_on_top_level_namespace_id_and_parent_id ON repositories USING btree (top_level_namespace_id, parent_id);

CREATE TABLE media_types (
    created_at timestamp WITH time zone NOT NULL DEFAULT now(),
    id smallint NOT NULL GENERATED BY DEFAULT AS IDENTITY,
    media_type text NOT NULL,
    CONSTRAINT pk_media_types PRIMARY KEY (id),
    CONSTRAINT unique_media_types_type UNIQUE (media_type),
    CONSTRAINT check_media_types_type_length CHECK ((char_length(media_type) <= 255))
);

CREATE TABLE blobs (
    size bigint NOT NULL,
    created_at timestamp WITH time zone NOT NULL DEFAULT now(),
    media_type_id smallint NOT NULL,
    digest bytea NOT NULL,
    CONSTRAINT pk_blobs PRIMARY KEY (digest),
    CONSTRAINT fk_blobs_media_type_id_media_types FOREIGN KEY (media_type_id) REFERENCES media_types (id)
)
PARTITION BY HASH (digest);

CREATE INDEX index_blobs_on_media_type_id ON blobs USING btree (media_type_id);

CREATE TABLE repository_blobs (
    id bigint NOT NULL GENERATED BY DEFAULT AS IDENTITY,
    top_level_namespace_id bigint NOT NULL,
    repository_id bigint NOT NULL,
    created_at timestamp WITH time zone NOT NULL DEFAULT now(),
    blob_digest bytea NOT NULL,
    CONSTRAINT pk_repository_blobs PRIMARY KEY (top_level_namespace_id, repository_id, id),
    CONSTRAINT fk_repository_blobs_top_lvl_nmspc_id_and_rpstry_id_repositories FOREIGN KEY (top_level_namespace_id, repository_id) REFERENCES repositories (top_level_namespace_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_repository_blobs_blob_digest_blobs FOREIGN KEY (blob_digest) REFERENCES blobs (digest) ON DELETE CASCADE,
    CONSTRAINT unique_repository_blobs_tp_lvl_nmspc_id_and_rpstry_id_blb_dgst UNIQUE (top_level_namespace_id, repository_id, blob_digest)
)
PARTITION BY HASH (top_level_namespace_id);

CREATE INDEX index_repository_blobs_on_top_lvl_nmspc_id_and_repository_id ON repository_blobs USING btree (top_level_namespace_id, repository_id);

CREATE INDEX index_repository_blobs_on_blob_digest ON repository_blobs USING btree (blob_digest);

CREATE TABLE manifests (
    id bigint NOT NULL GENERATED BY DEFAULT AS IDENTITY,
    top_level_namespace_id bigint NOT NULL,
    repository_id bigint NOT NULL,
    created_at timestamp WITH time zone NOT NULL DEFAULT now(),
    schema_version smallint NOT NULL,
    media_type_id smallint NOT NULL,
    configuration_media_type_id smallint,
    configuration_payload bytea,
    configuration_blob_digest bytea,
    digest bytea NOT NULL,
    payload bytea NOT NULL,
    CONSTRAINT pk_manifests PRIMARY KEY (top_level_namespace_id, repository_id, id),
    CONSTRAINT fk_manifests_top_lvl_nmespace_id_and_repository_id_repositories FOREIGN KEY (top_level_namespace_id, repository_id) REFERENCES repositories (top_level_namespace_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_manifests_media_type_id_media_types FOREIGN KEY (media_type_id) REFERENCES media_types (id),
    CONSTRAINT fk_manifests_configuration_media_type_id_media_types FOREIGN KEY (configuration_media_type_id) REFERENCES media_types (id),
    CONSTRAINT fk_manifests_configuration_blob_digest_blobs FOREIGN KEY (configuration_blob_digest) REFERENCES blobs (digest),
    CONSTRAINT unique_manifests_top_lvl_nmspc_id_and_repository_id_and_digest UNIQUE (top_level_namespace_id, repository_id, digest),
    CONSTRAINT unique_manifests_tp_lvl_nmspc_id_and_cfg_blob_dgst_repo_id_id UNIQUE (top_level_namespace_id, configuration_blob_digest, repository_id, id)
)
PARTITION BY HASH (top_level_namespace_id);

CREATE INDEX index_manifests_on_namespace_id_and_repository_id ON manifests USING btree (top_level_namespace_id, repository_id);

CREATE INDEX index_manifests_on_media_type_id ON manifests USING btree (media_type_id);

CREATE INDEX index_manifests_on_configuration_media_type_id ON manifests USING btree (configuration_media_type_id);

CREATE INDEX index_manifests_on_configuration_blob_digest ON manifests USING btree (configuration_blob_digest);

CREATE TABLE manifest_references (
    id bigint NOT NULL GENERATED BY DEFAULT AS IDENTITY,
    top_level_namespace_id bigint NOT NULL,
    repository_id bigint NOT NULL,
    parent_id bigint NOT NULL,
    child_id bigint NOT NULL,
    created_at timestamp WITH time zone NOT NULL DEFAULT now(),
    CONSTRAINT pk_manifest_references PRIMARY KEY (top_level_namespace_id, repository_id, id),
    CONSTRAINT fk_manifest_references_tp_lvl_nmspc_id_rpsty_id_prnt_id_mnfsts FOREIGN KEY (top_level_namespace_id, repository_id, parent_id) REFERENCES manifests (top_level_namespace_id, repository_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_manifest_references_tp_lvl_nmspc_id_rpsty_id_chld_id_mnfsts FOREIGN KEY (top_level_namespace_id, repository_id, child_id) REFERENCES manifests (top_level_namespace_id, repository_id, id),
    CONSTRAINT unique_manifest_references_tp_lvl_nmspc_id_rpy_id_prt_id_chd_id UNIQUE (top_level_namespace_id, repository_id, parent_id, child_id),
    CONSTRAINT check_manifest_references_parent_id_and_child_id_differ CHECK (parent_id <> child_id)
)
PARTITION BY HASH (top_level_namespace_id);

CREATE INDEX index_manifest_references_on_tp_lvl_nmspc_id_rpstry_id_prnt_id ON manifest_references USING btree (top_level_namespace_id, repository_id, parent_id);

CREATE INDEX index_manifest_references_on_tp_lvl_nmspc_id_rpstry_id_chld_id ON manifest_references USING btree (top_level_namespace_id, repository_id, child_id);

CREATE TABLE layers (
    id bigint NOT NULL GENERATED BY DEFAULT AS IDENTITY,
    top_level_namespace_id bigint NOT NULL,
    repository_id bigint NOT NULL,
    manifest_id bigint NOT NULL,
    size bigint NOT NULL,
    created_at timestamp WITH time zone NOT NULL DEFAULT now(),
    media_type_id smallint NOT NULL,
    digest bytea NOT NULL,
    CONSTRAINT pk_layers PRIMARY KEY (top_level_namespace_id, repository_id, id),
    CONSTRAINT fk_layers_top_lvl_nmspc_id_and_repo_id_and_manifst_id_manifests FOREIGN KEY (top_level_namespace_id, repository_id, manifest_id) REFERENCES manifests (top_level_namespace_id, repository_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_layers_media_type_id_media_types FOREIGN KEY (media_type_id) REFERENCES media_types (id),
    CONSTRAINT fk_layers_digest_blobs FOREIGN KEY (digest) REFERENCES blobs (digest),
    CONSTRAINT unique_layers_tp_lvl_nmspc_id_rpstry_id_and_mnfst_id_and_digest UNIQUE (top_level_namespace_id, repository_id, manifest_id, digest),
    CONSTRAINT unique_layers_digest_and_top_lvl_nmspc_id_and_rpstory_id_and_id UNIQUE (digest, top_level_namespace_id, repository_id, id)
)
PARTITION BY HASH (top_level_namespace_id);

CREATE INDEX index_layers_on_top_lvl_nmspc_id_and_rpstory_id_and_manifest_id ON layers USING btree (top_level_namespace_id, repository_id, manifest_id);

CREATE INDEX index_layers_on_media_type_id ON layers USING btree (media_type_id);

CREATE INDEX index_layers_on_digest ON layers USING btree (digest);

CREATE TABLE tags (
    id bigint NOT NULL GENERATED BY DEFAULT AS IDENTITY,
    top_level_namespace_id bigint NOT NULL,
    repository_id bigint NOT NULL,
    manifest_id bigint NOT NULL,
    created_at timestamp WITH time zone NOT NULL DEFAULT now(),
    updated_at timestamp WITH time zone,
    name text NOT NULL,
    CONSTRAINT pk_tags PRIMARY KEY (top_level_namespace_id, repository_id, id),
    CONSTRAINT fk_tags_repository_id_and_manifest_id_manifests FOREIGN KEY (top_level_namespace_id, repository_id, manifest_id) REFERENCES manifests (top_level_namespace_id, repository_id, id) ON DELETE CASCADE,
    CONSTRAINT unique_tags_top_level_namespace_id_and_repository_id_and_name UNIQUE (top_level_namespace_id, repository_id, name),
    CONSTRAINT check_tags_name_length CHECK ((char_length(name) <= 255))
)
PARTITION BY HASH (top_level_namespace_id);

CREATE INDEX index_tags_on_top_lvl_nmspc_id_and_rpository_id_and_manifest_id ON tags USING btree (top_level_namespace_id, repository_id, manifest_id);

CREATE TABLE gc_blobs_layers (
    id bigint NOT NULL GENERATED BY DEFAULT AS IDENTITY,
    top_level_namespace_id bigint NOT NULL,
    repository_id bigint NOT NULL,
    layer_id bigint NOT NULL,
    digest bytea NOT NULL,
    CONSTRAINT pk_gc_blobs_layers PRIMARY KEY (digest, id),
    CONSTRAINT fk_gc_blobs_layers_digest_repository_id_and_layer_id_layers FOREIGN KEY (digest, top_level_namespace_id, repository_id, layer_id) REFERENCES layers (digest, top_level_namespace_id, repository_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_gc_blobs_layers_digest_blobs FOREIGN KEY (digest) REFERENCES blobs (digest) ON DELETE CASCADE,
    CONSTRAINT unique_gc_blobs_layers_digest_and_layer_id UNIQUE (digest, layer_id)
)
PARTITION BY HASH (digest);

CREATE INDEX index_gc_blobs_layers_on_dgst_tp_lvl_nmspc_id_rpstry_id_lyr_id ON gc_blobs_layers USING btree (digest, top_level_namespace_id, repository_id, layer_id);

CREATE TABLE gc_blobs_configurations (
    id bigint NOT NULL GENERATED BY DEFAULT AS IDENTITY,
    top_level_namespace_id bigint NOT NULL,
    repository_id bigint NOT NULL,
    manifest_id bigint NOT NULL,
    digest bytea NOT NULL,
    CONSTRAINT pk_gc_blobs_configurations PRIMARY KEY (digest, id),
    CONSTRAINT fk_gc_blobs_configurations_tp_lvl_nspc_id_dgst_r_id_m_id_mnfsts FOREIGN KEY (top_level_namespace_id, digest, repository_id, manifest_id) REFERENCES manifests (top_level_namespace_id, configuration_blob_digest, repository_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_gc_blobs_configurations_digest_blobs FOREIGN KEY (digest) REFERENCES blobs (digest) ON DELETE CASCADE,
    CONSTRAINT unique_gc_blobs_configurations_digest_and_manifest_id UNIQUE (digest, manifest_id)
)
PARTITION BY HASH (digest);

CREATE INDEX index_gc_blobs_configurations_on_tp_lvl_nmspc_id_dgst_r_id_m_id ON gc_blobs_configurations USING btree (top_level_namespace_id, digest, repository_id, manifest_id);

CREATE TABLE gc_tmp_blobs_manifests (
    created_at timestamp WITH time zone NOT NULL DEFAULT now(),
    digest bytea NOT NULL,
    CONSTRAINT pk_gc_tmp_blobs_manifests PRIMARY KEY (digest)
);

CREATE TABLE gc_blob_review_queue (
    review_after timestamp WITH time zone NOT NULL DEFAULT now() + interval '1 day',
    review_count integer NOT NULL DEFAULT 0,
    digest bytea NOT NULL,
    CONSTRAINT pk_gc_blob_review_queue PRIMARY KEY (digest)
);

CREATE INDEX index_gc_blob_review_queue_on_review_after ON gc_blob_review_queue USING btree (review_after);

CREATE TABLE gc_manifest_review_queue (
    top_level_namespace_id bigint NOT NULL,
    repository_id bigint NOT NULL,
    manifest_id bigint NOT NULL,
    review_after timestamp WITH time zone NOT NULL DEFAULT now() + interval '1 day',
    review_count integer NOT NULL DEFAULT 0,
    CONSTRAINT pk_gc_manifest_review_queue PRIMARY KEY (top_level_namespace_id, repository_id, manifest_id),
    CONSTRAINT fk_gc_manifest_review_queue_tp_lvl_nspc_id_rp_id_mfst_id_mnfsts FOREIGN KEY (top_level_namespace_id, repository_id, manifest_id) REFERENCES manifests (top_level_namespace_id, repository_id, id) ON DELETE CASCADE
);

CREATE INDEX index_gc_manifest_review_queue_on_review_after ON gc_manifest_review_queue USING btree (review_after);

CREATE TABLE gc_review_after_defaults (
    event text NOT NULL,
    value interval NOT NULL,
    CONSTRAINT pk_gc_review_after_defaults PRIMARY KEY (event),
    CONSTRAINT check_gc_review_after_defaults_event_length CHECK ((char_length(event) <= 255))
);

CREATE FUNCTION gc_review_after (e text)
    RETURNS timestamp WITH time zone VOLATILE
    AS $$
DECLARE
    result timestamp WITH time zone;
BEGIN
    SELECT
        (now() + value) INTO result
    FROM
        gc_review_after_defaults
    WHERE
        event = e;
    IF result IS NULL THEN
        RETURN now() + interval '1 day';
    ELSE
        RETURN result;
    END IF;
END;
$$
LANGUAGE plpgsql;

CREATE FUNCTION gc_track_blob_uploads ()
    RETURNS TRIGGER
    AS $$
BEGIN
    INSERT INTO gc_blob_review_queue (digest, review_after)
        VALUES (NEW.digest, gc_review_after('blob_upload'))
    ON CONFLICT (digest)
        DO UPDATE SET
            review_after = gc_review_after('blob_upload');
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

CREATE TRIGGER gc_track_blob_uploads_trigger
    AFTER INSERT ON blobs
    FOR EACH ROW
    EXECUTE PROCEDURE gc_track_blob_uploads ();

CREATE FUNCTION gc_track_manifest_uploads ()
    RETURNS TRIGGER
    AS $$
BEGIN
    INSERT INTO gc_manifest_review_queue (top_level_namespace_id, repository_id, manifest_id, review_after)
        VALUES (NEW.top_level_namespace_id, NEW.repository_id, NEW.id, gc_review_after('manifest_upload'));
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

CREATE TRIGGER gc_track_manifest_uploads_trigger
    AFTER INSERT ON manifests
    FOR EACH ROW
    EXECUTE PROCEDURE gc_track_manifest_uploads ();

CREATE FUNCTION gc_track_configuration_blobs ()
    RETURNS TRIGGER
    AS $$
BEGIN
    IF NEW.configuration_blob_digest IS NOT NULL THEN
        INSERT INTO gc_blobs_configurations (top_level_namespace_id, repository_id, manifest_id, digest)
            VALUES (NEW.top_level_namespace_id, NEW.repository_id, NEW.id, NEW.configuration_blob_digest)
        ON CONFLICT (digest, manifest_id)
            DO NOTHING;
    END IF;
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

CREATE TRIGGER gc_track_configuration_blobs_trigger
    AFTER INSERT ON manifests
    FOR EACH ROW
    EXECUTE PROCEDURE gc_track_configuration_blobs ();

CREATE FUNCTION gc_track_layer_blobs ()
    RETURNS TRIGGER
    AS $$
BEGIN
    INSERT INTO gc_blobs_layers (top_level_namespace_id, repository_id, layer_id, digest)
        VALUES (NEW.top_level_namespace_id, NEW.repository_id, NEW.id, NEW.digest)
    ON CONFLICT (digest, layer_id)
        DO NOTHING;
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

CREATE TRIGGER gc_track_layer_blobs_trigger
    AFTER INSERT ON layers
    FOR EACH ROW
    EXECUTE PROCEDURE gc_track_layer_blobs ();

CREATE FUNCTION gc_track_tmp_blobs_manifests ()
    RETURNS TRIGGER
    AS $$
BEGIN
    INSERT INTO gc_tmp_blobs_manifests (digest)
        VALUES (NEW.digest)
    ON CONFLICT (digest)
        DO NOTHING;
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

CREATE TRIGGER gc_track_tmp_blobs_manifests_trigger
    AFTER INSERT ON manifests
    FOR EACH ROW
    EXECUTE PROCEDURE gc_track_tmp_blobs_manifests ();

CREATE FUNCTION gc_track_deleted_manifests ()
    RETURNS TRIGGER
    AS $$
BEGIN
    IF OLD.configuration_blob_digest IS NOT NULL THEN
        INSERT INTO gc_blob_review_queue (digest, review_after)
            VALUES (OLD.configuration_blob_digest, gc_review_after('manifest_delete'))
        ON CONFLICT (digest)
            DO UPDATE SET
                review_after = gc_review_after('manifest_delete');
    END IF;
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

CREATE TRIGGER gc_track_deleted_manifests_trigger
    AFTER DELETE ON manifests
    FOR EACH ROW
    EXECUTE PROCEDURE gc_track_deleted_manifests ();

CREATE FUNCTION gc_track_deleted_layers ()
    RETURNS TRIGGER
    AS $$
BEGIN
    INSERT INTO gc_blob_review_queue (digest, review_after)
        VALUES (OLD.digest, gc_review_after('layer_delete'))
    ON CONFLICT (digest)
        DO UPDATE SET
            review_after = gc_review_after('layer_delete');
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

CREATE TRIGGER gc_track_deleted_layers_trigger
    AFTER DELETE ON layers
    FOR EACH ROW
    EXECUTE PROCEDURE gc_track_deleted_layers ();

CREATE FUNCTION gc_track_deleted_manifest_lists ()
    RETURNS TRIGGER
AS $$
BEGIN
    INSERT INTO gc_manifest_review_queue (top_level_namespace_id, repository_id, manifest_id, review_after)
        VALUES (OLD.top_level_namespace_id, OLD.repository_id, OLD.child_id, gc_review_after('manifest_list_delete'))
    ON CONFLICT (top_level_namespace_id, repository_id, manifest_id)
        DO UPDATE SET
            review_after = gc_review_after('manifest_list_delete');
    RETURN NULL;
END;
$$
    LANGUAGE plpgsql;

CREATE TRIGGER gc_track_deleted_manifest_lists_trigger
    AFTER DELETE ON manifest_references
    FOR EACH ROW
EXECUTE PROCEDURE gc_track_deleted_manifest_lists ();

CREATE FUNCTION gc_track_switched_tags ()
    RETURNS TRIGGER
    AS $$
BEGIN
    INSERT INTO gc_manifest_review_queue (top_level_namespace_id, repository_id, manifest_id, review_after)
        VALUES (OLD.top_level_namespace_id, OLD.repository_id, OLD.manifest_id, gc_review_after('tag_switch'))
    ON CONFLICT (top_level_namespace_id, repository_id, manifest_id)
        DO UPDATE SET
            review_after = gc_review_after('tag_switch');
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

CREATE TRIGGER gc_track_switched_tags_trigger
    AFTER UPDATE OF manifest_id ON tags
    FOR EACH ROW
    EXECUTE PROCEDURE gc_track_switched_tags ();

CREATE FUNCTION gc_track_deleted_tags ()
    RETURNS TRIGGER
    AS $$
BEGIN
    IF EXISTS (
        SELECT
            1
        FROM
            manifests
        WHERE
            repository_id = OLD.repository_id
            AND id = OLD.manifest_id) THEN
        INSERT INTO gc_manifest_review_queue (top_level_namespace_id, repository_id, manifest_id, review_after)
            VALUES (OLD.top_level_namespace_id, OLD.repository_id, OLD.manifest_id, gc_review_after('tag_delete'))
        ON CONFLICT (top_level_namespace_id, repository_id, manifest_id)
            DO UPDATE SET
                review_after = gc_review_after('tag_delete');
    END IF;
    RETURN NULL;
END;
$$
LANGUAGE plpgsql;

CREATE TRIGGER gc_track_deleted_tags_trigger
    AFTER DELETE ON tags
    FOR EACH ROW
    EXECUTE PROCEDURE gc_track_deleted_tags ();

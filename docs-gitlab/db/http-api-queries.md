## HTTP API Queries

### Common Queries

Here we list queries used across several operations and refer to them from each operation to avoid repetitions.

#### Check if repository with a given path exists and grab its ID

   ```sql
SELECT
    id
FROM
    repositories
WHERE
    path = $1;
   ```

#### Find manifest by digest in repository

```sql
SELECT
    m.id,
    m.repository_id,
    m.created_at,
    m.schema_version,
    mt.type,
    m.digest,
    m.payload
FROM
    manifests AS m
    JOIN media_types AS mt ON mt.id = m.media_type_id
WHERE
    m.repository_id = $1
    AND m.digest = decode($2, 'hex');
```

#### Check if manifest exists in repository

```sql
SELECT
    EXISTS (
        SELECT
            1
        FROM
            manifests
        WHERE
            repository_id = $1
            AND digest = decode($2, 'hex'));
```

#### Find blob by digest in repository

```sql
SELECT
    b.digest,
    mt.type,
    b.size,
    b.created_at
FROM
    blobs AS b
    JOIN media_types AS mt ON mt.id = b.media_type_id
    JOIN repository_blobs AS rb ON rb.blob_digest = b.digest
WHERE
    rb.repository_id = $1
    AND b.digest = decode($2, 'hex');
```

#### Check if blob exists in repository

```sql
SELECT
    EXISTS (
        SELECT
            1
        FROM
            repository_blobs
        WHERE
            repository_id = $1
            AND blob_digest = decode($2, 'hex'));
```

#### Link blob to repository

This operation is idempotent.

```sql
INSERT INTO repository_blobs (repository_id, blob_digest)
    VALUES ($1, decode($2, 'hex'))
ON CONFLICT (repository_id, blob_digest)
    DO NOTHING;
```

### Repositories

#### List

[API docs](https://gitlab.com/gitlab-org/container-registry/-/blob/67bf50f4358c845d3e93a7bfd1318afb7c19786b/docs/spec/api.md#get-catalog)

```
GET /v2/_catalog 
```

1. Find all non empty repositories (with at least one manifest), lexicographically paginated and sorted by `path`:

```sql
SELECT
    r.id,
    r.name,
    r.path,
    r.parent_id,
    r.created_at,
    r.updated_at
FROM
    repositories AS r
WHERE
    EXISTS (
        SELECT
        FROM
            manifests AS m
        WHERE
            m.repository_id = r.id) -- ignore repositories that have no manifests (empty)
    AND r.path > $1 -- pagination marker (lexicographic)
ORDER BY
    r.path
LIMIT $2; -- pagination limit
```

### Blobs

#### Pull

[API docs](https://gitlab.com/gitlab-org/container-registry/-/blob/67bf50f4358c845d3e93a7bfd1318afb7c19786b/docs/spec/api.md#get-blob)

```
GET /v2/<name>/blobs/<digest>
```

1. [Check if repository with `path` `<name>` exists and grab its ID](#check-if-repository-with-a-given-path-exists-and-grab-its-id);
2. [Find blob with digest `<digest>` in repository `<name>`](#find-blob-by-digest-in-repository).

#### Check existance

[API docs](https://gitlab.com/gitlab-org/container-registry/-/blob/67bf50f4358c845d3e93a7bfd1318afb7c19786b/docs/spec/api.md#existing-layers)

```
HEAD /v2/<name>/blobs/<digest>
```

Same as for pull operation. Although we're just checking for existance, the HTTP response includes headers with metadata, so we need to retrieve it from the database.

#### Push

[API docs](https://gitlab.com/gitlab-org/container-registry/-/blob/67bf50f4358c845d3e93a7bfd1318afb7c19786b/docs/spec/api.md#put-blob-upload)

```
PUT /v2/<name>/blobs/uploads/<uuid>?digest=<digest>
```

1. [Check if repository with `path` `<name>` exists and grab its ID](#check-if-repository-with-a-given-path-exists-and-grab-its-id);

2. "*Create or find*" blob with digest `<digest>` in repository `<name>`. We avoid a "*find or create*" because it's prone to race conditions on inserts and this is a concurrent operation:

   ```sql
   INSERT INTO blobs (digest, media_type_id, size)
       VALUES (decode($1, 'hex'), $2, $3)
   ON CONFLICT (digest)
       DO NOTHING
   RETURNING
       created_at;
   ```

   If the resultset is empty, we know the blob already exists and we [find it by digest](#find-blob-by-digest-in-repository). Otherwise, we get the attributes initialized by the database during the insert and proceed.

3. [Link blob with digest `<digest>` to repository `<name>`](#link-blob-to-repository).

#### Cross repository mount

[API docs](https://gitlab.com/gitlab-org/container-registry/-/blob/67bf50f4358c845d3e93a7bfd1318afb7c19786b/docs/spec/api.md#mount-blob)

```
POST /v2/<name>/blobs/uploads/?mount=<digest>&from=<repository name> 
```

1. [Check if repository with `path` `<name>` exists and grab its ID](#check-if-repository-with-a-given-path-exists-and-grab-its-id);
2. [Check if *source* repository with `path` `<repository name>` exists and grab its ID](#check-if-repository-with-a-given-path-exists-and-grab-its-id);
3. [Check if blob with digest `<digest>` exists and is linked to the *source* `<repository name>` repository](#check-if-blob-exists-in-repository);
4. [Link blob with digest `<digest>` to *target* `<name>` repository](#link-blob-to-repository).

#### Delete blob link

[API docs](https://gitlab.com/gitlab-org/container-registry/-/blob/67bf50f4358c845d3e93a7bfd1318afb7c19786b/docs/spec/api.md#delete-blob)

```
DELETE /v2/<name>/blobs/<digest> 
```

1. [Check if repository with `path` `<name>` exists and grab its ID](#check-if-repository-with-a-given-path-exists-and-grab-its-id);

2. Delete link for blob with digest `<digest>` in repository `<name>`:

   ```sql
   DELETE FROM repository_blobs
   WHERE repository_id = $1
   		AND blob_digest = decode($2, 'hex')
   RETURNING
       1;
   ```

   If the resultset has no rows we know the blob link does not exist and raise the corresponding error. This avoids the need for a separate preceeding `SELECT` to find if the link exists.

### Manifests

#### Pull

[API docs](https://gitlab.com/gitlab-org/container-registry/-/blob/67bf50f4358c845d3e93a7bfd1318afb7c19786b/docs/spec/api.md#get-manifest)

```
GET /v2/<name>/manifests/<reference>
```

A manifest can be pulled by digest or tag.

##### By digest

1. [Check if repository with `path` `<name>` exists and grab its ID](#check-if-repository-with-a-given-path-exists-and-grab-its-id);
2. [Find manifest with digest `<reference>` in repository `<name>`](#find-manifest-by-digest-in-repository);

##### By tag

1. [Check if repository with `path` `<name>` exists and grab its ID](#check-if-repository-with-a-given-path-exists-and-grab-its-id);

2. Find manifest with tag name `<reference>`  within repository `<name>`:
   ```sql
   SELECT
       m.id,
       m.repository_id,
       m.created_at,
       m.schema_version,
       mt.type,
       m.digest,
       m.payload
   FROM
       manifests AS m
       JOIN media_types AS mt ON mt.id = m.media_type_id
       JOIN tags AS t ON t.repository_id = m.repository_id
            AND t.manifest_id = m.id
   WHERE
       m.repository_id = $2
       AND t.name = $1;
   ```

#### Check existance

[API docs](https://gitlab.com/gitlab-org/container-registry/-/blob/67bf50f4358c845d3e93a7bfd1318afb7c19786b/docs/spec/api.md#existing-manifests)

```
HEAD /v2/<name>/manifests/<reference>
```

Same as for pull operation. Although we're just checking for existance, the HTTP response includes headers with metadata, so we need to retrieve it from the database.

#### Push

[API docs](https://gitlab.com/gitlab-org/container-registry/-/blob/67bf50f4358c845d3e93a7bfd1318afb7c19786b/docs/spec/api.md#put-manifest)

```
PUT /v2/<name>/manifests/<reference> 
```

A manifest can be either an atomic/indivisible manifest or a manifest list (e.g. multi-arch image). Regardless, a manifest can be either pushed by digest (untagged) or by tag (tagged).

##### "Atomic" manifests

###### By digest

1. [Check if repository with `path` `<name>` exists and grab its ID](#check-if-repository-with-a-given-path-exists-and-grab-its-id);

2. For each referenced artifact in the manifest payload (configuration, layer and/or other manifest):

   - Configuration or layer: [Check if blob exists in repository `<name>`](#check-if-blob-exists-in-repository);

   - Manifest: [Check if manifest exists in repository `<name>`](#check-if-manifest-exists-in-repository).

3. "*Create or find*" manifest in repository `<name>`. We avoid a "*find or create*" because it's prone to race conditions on inserts and this is a concurrent operation:

   ```sql
   INSERT INTO manifests (repository_id, schema_version, media_type_id, digest, payload, configuration_payload, configuration_blob_digest)
       VALUES ($1, $2, $3, decode($4, 'hex'), $5, $6, decode($7, 'hex'))
   ON CONFLICT (repository_id, digest)
       DO NOTHING
   RETURNING
       id, created_at;
   ```

   If the resultset is empty, we know the manifest already exists and [find it by digest](#find-manifest-by-digest-in-repository). Otherwise, we get the attributes initialized by the database during the insert and proceed.

4. For each layer in the manifest payload do:

   1. [Check if blob exists in repository `<name>`](#check-if-blob-exists-in-repository);

   2. Create layer record:

      ```sql
      INSERT INTO layers (repository_id, manifest_id, digest, size, media_type_id)
          VALUES ($1, $2, decode($3, 'hex'), $4, $5);
      ```

###### By tag

1. Same steps as by digest;

2. Upsert tag with name `<reference>` in repository `<name>`. A tag with name `<reference>` may: not exist; already exist and point to the same manifest; already exist but point to a different manifest.

   If the tag doesn't exist we insert it, if the tag already exists we update it, but only if the current manifest that it points to is different (to avoid "empty" updates that may trigger unwanted/unnecessary actions in the database):

   ```sql
   INSERT INTO tags (repository_id, manifest_id, name)
       VALUES ($1, $2, $3)
   ON CONFLICT (repository_id, name)
       DO UPDATE SET
           manifest_id = EXCLUDED.manifest_id, updated_at = now()
       WHERE
           tags.manifest_id <> excluded.manifest_id; -- only update if target manifest differs
   ```

##### Manifest lists

###### By digest

1. [Check if repository with `path` `<name>` exists and grab its ID](#check-if-repository-with-a-given-path-exists-and-grab-its-id);

2. For each manifest referenced in the list, [check if manifest exists in repository `<name>`](#check-if-manifest-exists-in-repository);

3. "*Create or find*" manifest list in repository `<name>`. We avoid a "*find or create*" because it's prone to race conditions on inserts and this is a concurrent operation:

   ```sql
   INSERT INTO manifests (repository_id, schema_version, media_type_id, digest, payload, configuration_payload, configuration_blob_digest)
       VALUES ($1, $2, $3, decode($4, 'hex'), $5, $6, decode($7, 'hex'))
   ON CONFLICT (repository_id, digest)
       DO NOTHING
   RETURNING
       id, created_at;
   ```

   If the resultset is empty, we know the manifest list already exists and [find it by digest](#find-manifest-by-digest-in-repository). Otherwise, we get the attributes initialized by the database during the insert and proceed.

4. Create a relationship record for each manifest referenced in the manifest list payload, where `parent_id` is the manifest list ID and `child_id` is the referenced manifest ID (bulk insert):

   ```sql
   INSERT INTO manifest_references (repository_id, parent_id, child_id)
          VALUES ($1, $2, $3);
    ```

###### By tag

1. Same steps as by digest;
2. Upsert tag with name `<reference>` like it's done for atomic manifests.

#### Delete

[API docs](https://gitlab.com/gitlab-org/container-registry/-/blob/67bf50f4358c845d3e93a7bfd1318afb7c19786b/docs/spec/api.md#delete-manifest)

```
DELETE /v2/<name>/manifests/<reference> 
```

1. [Check if repository with `path` `<name>` exists and grab its ID](#check-if-repository-with-a-given-path-exists-and-grab-its-id);

2. Delete manifest with digest `<reference>` in repository `<name>`:

   ```sql
   DELETE FROM manifests
   WHERE repository_id = $1
       AND digest = decode($2, 'hex')
   RETURNING
       id;
   ```

   If the resultset has no rows we know the manifest does not exist and raise the corresponding error. This avoids the need for a separate preceeding `SELECT` to find if the manifest exists.

### Tags

#### List

[API docs](https://gitlab.com/gitlab-org/container-registry/-/blob/67bf50f4358c845d3e93a7bfd1318afb7c19786b/docs/spec/api.md#get-tags)

```
GET /v2/<name>/tags/list 
```

1. Find all tags in repository `<name>`, lexicographically paginated and sorted by tag name:

   ```sql
   SELECT
       id,
       name,
       repository_id,
       manifest_id,
       created_at,
       updated_at
   FROM
       tags
   WHERE
       repository_id = $1
       AND name > $2 -- pagination marker (lexicographic)
   ORDER BY
       name
   LIMIT $3; -- pagination limit
   ```

#### Delete

[API docs](https://gitlab.com/gitlab-org/container-registry/-/blob/67bf50f4358c845d3e93a7bfd1318afb7c19786b/docs/spec/api.md#delete-tags)

```
DELETE /v2/<name>/tags/reference/<reference> 
```

1. [Check if repository with `path` `<name>` exists and grab its ID](#check-if-repository-with-a-given-path-exists-and-grab-its-id);

2. Delete tag `<reference>` in repository `<name>`. If the resultset has no rows we know the tag does not exist:

   ```sql
   DELETE FROM tags
   WHERE repository_id = $1
       AND name = $2
   RETURNING
       manifest_id;
   ```

   If the resultset has no rows we know the tag does not exist and raise the corresponding error. This avoids the need for a separate preceeding `SELECT` to find if the tag exists.

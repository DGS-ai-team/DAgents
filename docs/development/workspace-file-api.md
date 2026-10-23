# Agent workspace file API

The Node exposes local workspace browsing and file editing for an existing, non-archived Agent. Paths are workspace-relative and URL encoded. `.dagents` is private Agent state and is hidden from these endpoints. Directories, special files, paths outside the workspace, Windows drive/ADS/UNC forms, and files larger than 16 MiB are rejected or omitted.

* `GET /v1/agents/{agent_id}/workspace?path=...` returns `{ "path": "...", "entries": [...] }`. Each entry has `name`, `path`, `is_dir`, `size`, `modified_at`; regular files also have an absolute Node-side `file_ref`. The listing does not read file contents or calculate revisions.
* `GET /v1/agents/{agent_id}/workspace/file?path=...` returns raw bytes as `application/octet-stream`, with a quoted SHA-256 `ETag`.
* `PUT /v1/agents/{agent_id}/workspace/file?path=...` writes raw bytes. Existing files require `If-Match: "<sha256>"`; new files require `If-None-Match: *`. Missing preconditions return 428; stale revisions and create collisions return 412. Success returns JSON `{path,name,size,revision}` and the new quoted `ETag`.
* `POST /v1/agents/{agent_id}/workspace/upload?path=...` accepts raw bytes and creates a new file only; it returns 200 with `{path,name,size,revision,file_ref}` and an ETag.

The upload response `file_ref` and regular-file entry `file_ref` are absolute Node-side paths and can be passed to model tools. File operations use a root-bound filesystem handle so path traversal and symlink swaps cannot escape the selected workspace. The Node serializes file writes through its shared workspace coordinator so Agents sharing a workspace coordinate with tool writes.

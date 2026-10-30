use sha2::{Digest, Sha256};
use std::fs::{self, File, OpenOptions};
use std::io::{Read, Seek, SeekFrom};
use std::path::{Component, Path, PathBuf};

pub const CHUNK_LIMIT: usize = 256 * 1024;
pub const FULL_LIMIT: u64 = 16 * 1024 * 1024;

#[derive(Debug, Clone, serde::Serialize, PartialEq, Eq)]
pub struct ReadResult {
    pub status: u16,
    pub content_type: String,
    pub etag: String,
    pub file_size: u64,
    pub next_offset: u64,
    pub body_base64: String,
}

#[tauri::command]
pub async fn desktop_workspace_read_file(
    window: tauri::WebviewWindow,
    agent_id: String,
    path: String,
    if_none_match: Option<String>,
    shared: tauri::State<'_, std::sync::Arc<super::Shared>>,
) -> Result<ReadResult, String> {
    super::ensure_mvp_window(&window)?;
    let client = std::sync::Arc::clone(&shared.node_client);
    super::spawn_node_blocking(move || {
        let location = client.workspace_location(&agent_id)?;
        read_file(Path::new(&location.root), &path, if_none_match.as_deref())
    })
    .await?
}

fn validate_relative_path(raw: &str) -> Result<PathBuf, String> {
    if raw.is_empty() || raw.contains('\0') || raw.contains('\\') || raw.starts_with('/') {
        return Err("工作区路径必须是安全的相对路径".into());
    }
    let mut relative = PathBuf::new();
    for segment in raw.split('/') {
        if segment.is_empty() || segment == "." || segment == ".." || segment.contains(':') {
            return Err("工作区路径必须是安全的相对路径".into());
        }
        if relative.as_os_str().is_empty()
            && segment
                .trim_end_matches(['.', ' '])
                .eq_ignore_ascii_case(".dagents")
        {
            return Err("工作区私有目录不可读取".into());
        }
        relative.push(segment);
    }
    if relative
        .components()
        .any(|part| !matches!(part, Component::Normal(_)))
    {
        return Err("工作区路径必须是安全的相对路径".into());
    }
    Ok(relative)
}

pub fn read_chunk(
    root: &Path,
    raw_path: &str,
    offset: u64,
    limit: usize,
    expected_revision: Option<&str>,
    text: bool,
) -> Result<ReadResult, String> {
    if limit == 0 || limit > CHUNK_LIMIT {
        return Ok(error_result(
            400,
            "workspace_limit_invalid",
            "limit must be between 1 and 262144",
        ));
    }
    let (mut file, canonical_root) = match open_workspace_file(root, raw_path) {
        Ok(value) => value,
        Err(error) if error.starts_with("WORKSPACE_NOT_FOUND:") => {
            return Ok(error_result(
                404,
                "workspace_file_not_found",
                "file not found",
            ));
        }
        Err(error) => return Err(error),
    };
    let file_meta = file
        .metadata()
        .map_err(|e| format!("无法检查工作区文件: {e}"))?;
    if !file_meta.is_file() {
        return Err("工作区路径不是普通文件".into());
    }
    verify_open_file_within_root(&file, &canonical_root)?;
    let file_size = file_meta.len();
    if offset > file_size {
        return Ok(error_result_with_revision(
            416,
            "workspace_offset_invalid",
            "offset is beyond end of file",
            String::new(),
            file_size,
            offset,
        ));
    }

    // Chunk reads use Node's weak size/mtime token so a large file is not
    // scanned repeatedly. The complete-file endpoint below uses SHA-256.
    let revision = weak_revision(&file_meta)?;
    if let Some(expected) = expected_revision.filter(|v| !v.is_empty()) {
        if expected != revision {
            return Ok(error_result_with_revision(
                409,
                "workspace_preview_changed",
                "file changed while reading preview",
                revision,
                file_size,
                offset,
            ));
        }
    }
    file.seek(SeekFrom::Start(offset))
        .map_err(|e| format!("定位工作区文件失败: {e}"))?;
    let mut bytes = vec![0; limit.min((file_size - offset) as usize)];
    file.read_exact(&mut bytes)
        .map_err(|e| format!("读取工作区分段失败: {e}"))?;
    let latest = file
        .metadata()
        .map_err(|e| format!("无法检查工作区文件: {e}"))?;
    if weak_revision(&latest)? != revision {
        return Ok(error_result_with_revision(
            409,
            "workspace_preview_changed",
            "file changed while reading preview",
            revision,
            file_size,
            offset,
        ));
    }
    if text {
        // Keep valid UTF-8 boundaries so the WebView decoder never sees a
        // partial code point. The next request begins at the returned offset.
        if let Err(error) = std::str::from_utf8(&bytes) {
            if error.error_len().is_none() {
                bytes.truncate(error.valid_up_to());
            }
        }
    }
    let next_offset = offset + bytes.len() as u64;
    let encoded = base64::Engine::encode(&base64::engine::general_purpose::STANDARD, &bytes);
    Ok(ReadResult {
        status: 200,
        content_type: content_type(raw_path),
        etag: revision,
        file_size,
        next_offset,
        body_base64: encoded,
    })
}

pub fn read_file(
    root: &Path,
    raw_path: &str,
    if_none_match: Option<&str>,
) -> Result<ReadResult, String> {
    let (mut file, _canonical_root) = match open_workspace_file(root, raw_path) {
        Ok(value) => value,
        Err(error) if error.starts_with("WORKSPACE_NOT_FOUND:") => {
            return Ok(error_result(
                404,
                "workspace_file_not_found",
                "file not found",
            ));
        }
        Err(error) => return Err(error),
    };
    let metadata = file
        .metadata()
        .map_err(|e| format!("无法检查工作区文件: {e}"))?;
    let file_size = metadata.len();
    if file_size > FULL_LIMIT {
        return Ok(error_result(
            413,
            "workspace_file_too_large",
            "file exceeds 16 MiB",
        ));
    }
    let revision = match hash_open_file_limited(&mut file, FULL_LIMIT) {
        Ok(revision) => revision,
        Err(error) if error.contains("16 MiB") => {
            return Ok(error_result(
                413,
                "workspace_file_too_large",
                "file exceeds 16 MiB",
            ));
        }
        Err(error) => return Err(error),
    };
    let after_hash = file
        .metadata()
        .map_err(|e| format!("无法检查工作区文件: {e}"))?;
    if after_hash.len() != file_size || after_hash.modified().ok() != metadata.modified().ok() {
        return Err("文件在读取期间发生变化，请重新载入".into());
    }
    let etag = format!("\"{revision}\"");
    if if_none_match.is_some_and(|value| if_none_match_matches(value, &etag)) {
        let stable = hash_open_file_limited(&mut file, FULL_LIMIT)?;
        if stable != revision {
            return Err("文件在读取期间发生变化，请重新载入".into());
        }
        return Ok(ReadResult {
            status: 304,
            content_type: content_type(raw_path),
            etag,
            file_size,
            next_offset: 0,
            body_base64: String::new(),
        });
    }
    file.seek(SeekFrom::Start(0))
        .map_err(|e| format!("定位工作区文件失败: {e}"))?;
    let mut bytes = Vec::with_capacity(file_size as usize);
    (&mut file)
        .take(FULL_LIMIT + 1)
        .read_to_end(&mut bytes)
        .map_err(|e| format!("读取工作区文件失败: {e}"))?;
    if bytes.len() as u64 > FULL_LIMIT {
        return Ok(error_result(
            413,
            "workspace_file_too_large",
            "file exceeds 16 MiB",
        ));
    }
    let after_read = file
        .metadata()
        .map_err(|e| format!("无法检查工作区文件: {e}"))?;
    if bytes.len() as u64 != file_size
        || after_read.len() != file_size
        || hex::encode(Sha256::digest(&bytes)) != revision
    {
        return Err("文件在读取期间发生变化，请重新载入".into());
    }
    let body_base64 = base64::Engine::encode(&base64::engine::general_purpose::STANDARD, &bytes);
    Ok(ReadResult {
        status: 200,
        content_type: content_type(raw_path),
        etag,
        file_size,
        next_offset: file_size,
        body_base64,
    })
}

pub fn open_workspace_file(root: &Path, raw_path: &str) -> Result<(File, PathBuf), String> {
    let relative = validate_relative_path(raw_path)?;
    let canonical_root = fs::canonicalize(root).map_err(|e| format!("工作区不可用: {e}"))?;
    if !same_authorized_root(root, &canonical_root) {
        return Err("Node 授权的工作区根目录已改变".into());
    }
    let target = canonical_root.join(&relative);
    reject_reparse_components(&canonical_root, &relative)?;
    let mut options = OpenOptions::new();
    options.read(true);
    #[cfg(windows)]
    {
        use std::os::windows::fs::OpenOptionsExt;
        options.custom_flags(0x0020_0000); // FILE_FLAG_OPEN_REPARSE_POINT
    }
    let file = options.open(&target).map_err(|e| {
        if e.kind() == std::io::ErrorKind::NotFound {
            format!("WORKSPACE_NOT_FOUND: {e}")
        } else {
            format!("无法读取工作区文件: {e}")
        }
    })?;
    let metadata = file
        .metadata()
        .map_err(|e| format!("无法检查工作区文件: {e}"))?;
    if !metadata.is_file() {
        return Err("工作区路径不是普通文件".into());
    }
    #[cfg(windows)]
    {
        use std::os::windows::fs::MetadataExt;
        const FILE_ATTRIBUTE_REPARSE_POINT: u32 = 0x400;
        if metadata.file_attributes() & FILE_ATTRIBUTE_REPARSE_POINT != 0 {
            return Err("工作区重解析点不可读取".into());
        }
    }
    verify_open_file_within_root(&file, &canonical_root)?;
    Ok((file, canonical_root))
}

/// Resolve an authorized workspace directory for opening it in the native
/// file manager. Empty paths refer to the already-authorized workspace root.
pub fn resolve_workspace_directory(root: &Path, raw_path: &str) -> Result<PathBuf, String> {
    let canonical_root = fs::canonicalize(root).map_err(|e| format!("工作区不可用: {e}"))?;
    if !same_authorized_root(root, &canonical_root) {
        return Err("Node 授权的工作区根目录已改变".into());
    }
    if raw_path.is_empty() {
        return Ok(canonical_root);
    }

    let relative = validate_relative_path(raw_path)?;
    reject_reparse_components(&canonical_root, &relative)?;
    let target = canonical_root.join(relative);
    let metadata = fs::symlink_metadata(&target).map_err(|e| format!("无法访问工作区目录: {e}"))?;
    if is_reparse_point(&metadata) {
        return Err("工作区符号链接不可读取".into());
    }
    if !metadata.is_dir() {
        return Err("工作区路径不是目录".into());
    }
    let resolved = fs::canonicalize(&target).map_err(|e| format!("无法解析工作区目录: {e}"))?;
    if !path_is_within_root(&resolved, &canonical_root) {
        return Err("工作区目录指向授权根目录之外".into());
    }
    Ok(resolved)
}

#[cfg(windows)]
fn path_is_within_root(path: &Path, root: &Path) -> bool {
    let path = normalize_windows_final_path(path.to_string_lossy().into_owned());
    let root = normalize_windows_final_path(root.to_string_lossy().into_owned());
    path_is_within_windows(&path, &root)
}

#[cfg(not(windows))]
fn path_is_within_root(path: &Path, root: &Path) -> bool {
    path.starts_with(root)
}

/// Validate the Agent-authorized root and a safe workspace file path while
/// allowing the file or any later path component to be missing. Every
/// existing ancestor is still checked for reparse points. This supports
/// exporting a retained editor draft after its file or parent was deleted.
pub fn validate_workspace_file_path(
    root: &Path,
    raw_path: &str,
) -> Result<(PathBuf, PathBuf), String> {
    let relative = validate_relative_path(raw_path)?;
    let canonical_root = fs::canonicalize(root).map_err(|e| format!("工作区不可用: {e}"))?;
    if !same_authorized_root(root, &canonical_root) {
        return Err("Node 授权的工作区根目录已改变".into());
    }
    reject_reparse_components_allow_missing_suffix(&canonical_root, &relative)?;
    Ok((canonical_root.join(relative), canonical_root))
}

#[cfg(windows)]
fn same_authorized_root(requested: &Path, resolved: &Path) -> bool {
    let requested = normalize_windows_final_path(requested.to_string_lossy().into_owned());
    let resolved = normalize_windows_final_path(resolved.to_string_lossy().into_owned());
    requested == resolved
}

#[cfg(not(windows))]
fn same_authorized_root(requested: &Path, resolved: &Path) -> bool {
    requested == resolved
}

fn weak_revision(metadata: &fs::Metadata) -> Result<String, String> {
    let modified = metadata
        .modified()
        .map_err(|e| format!("无法读取工作区修改时间: {e}"))?;
    let nanos = match modified.duration_since(std::time::UNIX_EPOCH) {
        Ok(duration) => duration.as_nanos() as i64,
        Err(error) => -(error.duration().as_nanos() as i64),
    };
    Ok(format!("W/\"{}-{nanos:x}\"", metadata.len()))
}

fn if_none_match_matches(header: &str, etag: &str) -> bool {
    if header.trim() == "*" {
        return true;
    }
    header.split(',').any(|part| {
        let part = part.trim();
        let tag = part.strip_prefix("W/").unwrap_or(part);
        tag == etag
    })
}

fn error_result(status: u16, code: &str, message: &str) -> ReadResult {
    error_result_with_revision(status, code, message, String::new(), 0, 0)
}

fn error_result_with_revision(
    status: u16,
    code: &str,
    message: &str,
    etag: String,
    file_size: u64,
    next_offset: u64,
) -> ReadResult {
    let body = serde_json::json!({ "error": { "code": code, "message": message } }).to_string();
    let body_base64 =
        base64::Engine::encode(&base64::engine::general_purpose::STANDARD, body.as_bytes());
    ReadResult {
        status,
        content_type: "application/json".into(),
        etag,
        file_size,
        next_offset,
        body_base64,
    }
}

fn hash_open_file_limited(file: &mut File, limit: u64) -> Result<String, String> {
    file.seek(SeekFrom::Start(0))
        .map_err(|e| format!("定位工作区文件失败: {e}"))?;
    let mut hasher = Sha256::new();
    let mut buffer = [0u8; 256 * 1024];
    let mut total = 0u64;
    loop {
        let count = file
            .read(&mut buffer)
            .map_err(|e| format!("读取工作区文件失败: {e}"))?;
        if count == 0 {
            break;
        }
        total += count as u64;
        if total > limit {
            return Err("工作区完整读取上限为 16 MiB".into());
        }
        hasher.update(&buffer[..count]);
    }
    Ok(hex::encode(hasher.finalize()))
}

fn content_type(path: &str) -> String {
    match Path::new(path)
        .extension()
        .and_then(|e| e.to_str())
        .unwrap_or("")
        .to_ascii_lowercase()
        .as_str()
    {
        "md" | "txt" | "csv" | "json" | "js" | "ts" | "html" | "htm" | "xml" | "yaml" | "yml"
        | "py" | "go" | "rs" | "toml" | "css" | "sql" | "sh" => "text/plain",
        "png" => "image/png",
        "jpg" | "jpeg" => "image/jpeg",
        "gif" => "image/gif",
        "webp" => "image/webp",
        "pdf" => "application/pdf",
        _ => "application/octet-stream",
    }
    .into()
}

fn reject_reparse_components(root: &Path, relative: &Path) -> Result<(), String> {
    let mut current = root.to_path_buf();
    for component in relative.components() {
        let Component::Normal(part) = component else {
            return Err("工作区路径无效".into());
        };
        current.push(part);
        match fs::symlink_metadata(&current) {
            Ok(metadata) if is_reparse_point(&metadata) => {
                return Err("工作区符号链接不可读取".into())
            }
            Ok(_) => {}
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
                return Err(format!("WORKSPACE_NOT_FOUND: {error}"))
            }
            Err(error) => return Err(format!("检查工作区路径失败: {error}")),
        }
    }
    Ok(())
}

fn reject_reparse_components_allow_missing_suffix(
    root: &Path,
    relative: &Path,
) -> Result<(), String> {
    let components = relative.components().collect::<Vec<_>>();
    let mut current = root.to_path_buf();
    for (index, component) in components.iter().enumerate() {
        let Component::Normal(part) = component else {
            return Err("工作区路径无效".into());
        };
        current.push(part);
        match fs::symlink_metadata(&current) {
            Ok(metadata) if is_reparse_point(&metadata) => {
                return Err("工作区符号链接不可读取".into())
            }
            Ok(metadata) if index + 1 == components.len() && !metadata.is_file() => {
                return Err("工作区路径不是普通文件".into())
            }
            Ok(metadata) if index + 1 < components.len() && !metadata.is_dir() => {
                return Err("工作区路径的父组件不是目录".into())
            }
            Ok(_) => {}
            // Once a component is missing, all following components are
            // necessarily missing too. Lexical validation already covered
            // their names, while the existing ancestors above were checked.
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(()),
            Err(error) => return Err(format!("检查工作区路径失败: {error}")),
        }
    }
    Ok(())
}

#[cfg(windows)]
fn is_reparse_point(metadata: &fs::Metadata) -> bool {
    use std::os::windows::fs::MetadataExt;
    metadata.file_attributes() & 0x400 != 0 // FILE_ATTRIBUTE_REPARSE_POINT
}

#[cfg(not(windows))]
fn is_reparse_point(metadata: &fs::Metadata) -> bool {
    metadata.file_type().is_symlink()
}

#[cfg(windows)]
fn verify_open_file_within_root(file: &File, root: &Path) -> Result<(), String> {
    use std::os::windows::io::AsRawHandle;
    use windows::Win32::Foundation::HANDLE;
    use windows::Win32::Storage::FileSystem::{
        GetFinalPathNameByHandleW, GETFINALPATHNAMEBYHANDLE_FLAGS,
    };
    let handle = HANDLE(file.as_raw_handle() as *mut _);
    let mut buffer = vec![0u16; 32768];
    let count = unsafe {
        GetFinalPathNameByHandleW(handle, &mut buffer, GETFINALPATHNAMEBYHANDLE_FLAGS(0))
    };
    if count == 0 || count as usize >= buffer.len() {
        return Err("无法验证工作区文件句柄路径".into());
    }
    buffer.truncate(count as usize);
    let final_path = normalize_windows_final_path(String::from_utf16_lossy(&buffer));
    let root_path = normalize_windows_final_path(root.to_string_lossy().into_owned());
    if !path_is_within_windows(&final_path, &root_path) {
        return Err("文件句柄指向工作区之外".into());
    }
    let suffix = final_path[root_path.len()..].trim_start_matches('\\');
    if suffix.split('\\').next().is_some_and(|part| {
        part.trim_end_matches(['.', ' '])
            .eq_ignore_ascii_case(".dagents")
    }) {
        return Err("工作区私有目录不可读取".into());
    }
    Ok(())
}

#[cfg(not(windows))]
fn verify_open_file_within_root(file: &File, root: &Path) -> Result<(), String> {
    // The supported Desktop target is Windows. Keep local builds usable while
    // still validating the opened file's resolved path on other platforms.
    use std::os::unix::io::AsRawFd;
    let actual = fs::canonicalize(format!("/proc/self/fd/{}", file.as_raw_fd()))
        .map_err(|e| format!("无法验证工作区文件句柄路径: {e}"))?;
    let actual_text = actual.to_string_lossy().to_ascii_lowercase();
    let root_text = root.to_string_lossy().to_ascii_lowercase();
    if !actual_text.starts_with(&(root_text.clone() + std::path::MAIN_SEPARATOR_STR))
        && actual_text != root_text
    {
        return Err("文件句柄指向工作区之外".into());
    }
    let relative = actual
        .strip_prefix(root)
        .map_err(|_| "文件句柄指向工作区之外".to_string())?;
    if relative.components().next().is_some_and(|part| {
        part.as_os_str()
            .to_string_lossy()
            .trim_end_matches(['.', ' '])
            .eq_ignore_ascii_case(".dagents")
    }) {
        return Err("工作区私有目录不可读取".into());
    }
    Ok(())
}

#[cfg(windows)]
fn normalize_windows_final_path(value: String) -> String {
    let value = value.replace('/', "\\");
    let value = if let Some(unc) = value.strip_prefix("\\\\?\\UNC\\") {
        format!("\\\\{unc}")
    } else if let Some(path) = value.strip_prefix("\\\\?\\") {
        path.to_string()
    } else {
        value
    };
    value.trim_end_matches('\\').to_lowercase()
}

#[cfg(windows)]
fn path_is_within_windows(path: &str, root: &str) -> bool {
    let path = path.to_lowercase();
    let root = root.to_lowercase();
    path == root
        || path
            .strip_prefix(&root)
            .is_some_and(|tail| tail.starts_with('\\'))
}

#[cfg(test)]
mod tests {
    use super::*;
    use base64::Engine;
    use std::io::Write;

    #[test]
    fn chunk_revision_is_node_compatible_and_rejects_stale_reads() {
        let dir = tempfile::tempdir().unwrap();
        let root = fs::canonicalize(dir.path()).unwrap();
        let content = "你好，workspace".as_bytes();
        let mut file = File::create(dir.path().join("note.txt")).unwrap();
        file.write_all(content).unwrap();
        let first = read_chunk(&root, "note.txt", 0, 5, None, false).unwrap();
        assert!(first.etag.starts_with("W/\""));
        let same_revision = read_chunk(&root, "note.txt", 5, 5, Some(&first.etag), false).unwrap();
        assert_eq!(same_revision.status, 200);
        assert_eq!(first.file_size, content.len() as u64);
        let stale = read_chunk(&root, "note.txt", 0, 5, Some("\"stale\""), false).unwrap();
        assert_eq!(stale.status, 409);
    }

    #[test]
    fn full_read_returns_sha256_etag_and_honors_if_none_match() {
        let dir = tempfile::tempdir().unwrap();
        let root = fs::canonicalize(dir.path()).unwrap();
        let content = "你好，workspace".as_bytes();
        fs::write(dir.path().join("note.txt"), content).unwrap();
        let full = read_file(&root, "note.txt", None).unwrap();
        let expected = format!("\"{}\"", hex::encode(Sha256::digest(content)));
        assert_eq!(full.etag, expected);
        let bytes = base64::engine::general_purpose::STANDARD
            .decode(full.body_base64.clone())
            .unwrap();
        assert_eq!(bytes, content);
        let cached = read_file(&root, "note.txt", Some(&full.etag)).unwrap();
        assert_eq!(cached.status, 304);
        assert!(cached.body_base64.is_empty());
        let weak_cached =
            read_file(&root, "note.txt", Some(&format!("\"stale\", W/{expected}"))).unwrap();
        assert_eq!(weak_cached.status, 304);
    }

    #[test]
    fn full_read_rejects_large_files_and_chunk_text_stops_on_utf8_boundary() {
        let dir = tempfile::tempdir().unwrap();
        let root = fs::canonicalize(dir.path()).unwrap();
        let oversized = vec![b'x'; FULL_LIMIT as usize + 1];
        fs::write(dir.path().join("large.txt"), oversized).unwrap();
        assert_eq!(read_file(&root, "large.txt", None).unwrap().status, 413);

        let mut text = vec![b'a'; CHUNK_LIMIT - 1];
        text.extend_from_slice("界".as_bytes());
        fs::write(dir.path().join("utf8.txt"), &text).unwrap();
        let first = read_chunk(&root, "utf8.txt", 0, CHUNK_LIMIT, None, true).unwrap();
        assert_eq!(first.next_offset, (CHUNK_LIMIT - 1) as u64);
        let second = read_chunk(
            &root,
            "utf8.txt",
            first.next_offset,
            CHUNK_LIMIT,
            Some(&first.etag),
            true,
        )
        .unwrap();
        let bytes = base64::engine::general_purpose::STANDARD
            .decode(second.body_base64)
            .unwrap();
        assert_eq!(bytes, "界".as_bytes());
    }

    #[test]
    fn refuses_private_and_traversal_paths_and_limits() {
        let dir = tempfile::tempdir().unwrap();
        let root = fs::canonicalize(dir.path()).unwrap();
        for path in ["../secret", ".dagents/hidden"] {
            assert!(
                read_chunk(&root, path, 0, 10, None, false).is_err(),
                "{path}"
            );
        }
        fs::create_dir_all(dir.path().join("sub/.DaGeNtS")).unwrap();
        fs::write(dir.path().join("sub/.DaGeNtS/ordinary.txt"), b"allowed").unwrap();
        assert_eq!(
            read_file(&root, "sub/.DaGeNtS/ordinary.txt", None)
                .unwrap()
                .status,
            200
        );
        assert_eq!(
            read_chunk(&root, "x", 0, CHUNK_LIMIT + 1, None, false)
                .unwrap()
                .status,
            400
        );
    }

    #[test]
    fn resolves_only_authorized_workspace_directories_for_native_open() {
        let dir = tempfile::tempdir().unwrap();
        let root = fs::canonicalize(dir.path()).unwrap();
        fs::create_dir(dir.path().join("reports")).unwrap();
        fs::write(dir.path().join("note.txt"), b"content").unwrap();

        assert_eq!(resolve_workspace_directory(&root, "").unwrap(), root);
        assert_eq!(
            resolve_workspace_directory(&root, "reports").unwrap(),
            fs::canonicalize(dir.path().join("reports")).unwrap()
        );
        assert!(resolve_workspace_directory(&root, "note.txt").is_err());
        assert!(resolve_workspace_directory(&root, "../outside").is_err());
        assert!(resolve_workspace_directory(&root, ".dagents").is_err());
    }

    #[cfg(windows)]
    #[test]
    fn rejects_reparse_point_targets_even_when_target_is_inside_workspace() {
        use std::os::windows::fs::{symlink_dir, symlink_file};
        let dir = tempfile::tempdir().unwrap();
        let root = fs::canonicalize(dir.path()).unwrap();
        let outside = tempfile::tempdir().unwrap();
        fs::write(dir.path().join("actual.txt"), b"content").unwrap();
        fs::write(outside.path().join("secret.txt"), b"secret").unwrap();
        if symlink_file(dir.path().join("actual.txt"), dir.path().join("linked.txt")).is_err() {
            return; // Windows Developer Mode or elevated symlink privilege is optional.
        }
        assert!(read_file(&root, "linked.txt", None).is_err());
        if symlink_dir(outside.path(), dir.path().join("escape")).is_ok() {
            assert!(read_file(&root, "escape/secret.txt", None).is_err());
        }
    }

    #[cfg(windows)]
    #[test]
    fn windows_path_containment_checks_component_boundary_and_unc_volume() {
        assert!(path_is_within_windows(
            r"c:\users\me\workspace\file.txt",
            r"C:\Users\Me\Workspace"
        ));
        assert!(!path_is_within_windows(
            r"c:\users\me\workspace-escape\file.txt",
            r"C:\Users\Me\Workspace"
        ));
        assert!(path_is_within_windows(
            r"c:\users\ä\workspace\file.txt",
            r"C:\Users\Ä\Workspace"
        ));
        let root = normalize_windows_final_path(r"\\?\UNC\server\share\workspace".into());
        let child = normalize_windows_final_path(r"\\?\UNC\server\share\workspace\file.txt".into());
        let other_share =
            normalize_windows_final_path(r"\\?\UNC\server\elsewhere\workspace\file.txt".into());
        assert!(path_is_within_windows(&child, &root));
        assert!(!path_is_within_windows(&other_share, &root));
        assert!(same_authorized_root(
            Path::new(r"D:\Workspace"),
            Path::new(r"\\?\d:\workspace")
        ));
        assert!(!same_authorized_root(
            Path::new(r"D:\Workspace"),
            Path::new(r"D:\External\Workspace")
        ));
    }
}

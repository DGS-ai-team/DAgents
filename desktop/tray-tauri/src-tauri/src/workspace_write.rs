use crate::nodeclient::{Client, DesktopApiError, WorkspaceWriteResult};
use sha2::{Digest, Sha256};
use std::collections::HashMap;
use std::sync::{
    atomic::{AtomicBool, Ordering},
    Mutex, OnceLock,
};
use std::{
    fs::{self, OpenOptions},
    io::{Read, Seek, SeekFrom, Write},
    path::Path,
    sync::Arc,
};
struct ExactFileReader {
    file: fs::File,
    expected_size: u64,
}
impl Read for ExactFileReader {
    fn read(&mut self, buffer: &mut [u8]) -> std::io::Result<usize> {
        let n = self.file.read(buffer)?;
        if n == 0 && self.file.metadata()?.len() != self.expected_size {
            return Err(std::io::Error::other(
                "source file size changed during import",
            ));
        }
        Ok(n)
    }
}

const TEXT_LIMIT: u64 = 16 * 1024 * 1024;
const IMPORT_LIMIT: u64 = 1024 * 1024 * 1024;
pub const INLINE_IMPORT_LIMIT: usize = 16 * 1024 * 1024;
pub fn validate_inline_import_size(size: usize) -> Result<(), String> {
    if size > INLINE_IMPORT_LIMIT {
        Err("拖放或剪贴板导入不能超过 16 MiB，请使用原生文件选择器导入大文件".into())
    } else {
        Ok(())
    }
}
fn write_limit(kind: &str) -> u64 {
    if kind == "import" {
        IMPORT_LIMIT
    } else {
        TEXT_LIMIT
    }
}
fn size_error(limit: u64) -> CommandResult {
    CommandResult {
        status: "error".into(),
        path: None,
        name: None,
        size: None,
        revision: None,
        file_ref: None,
        message: Some(format!("文件超过 {limit} 字节限制")),
        http_status: Some(413),
        error_code: Some("workspace_file_too_large".into()),
    }
}
static ACTIVE: OnceLock<Mutex<HashMap<String, (String, String, Arc<AtomicBool>)>>> =
    OnceLock::new();

pub struct ActiveTask {
    request_id: String,
    cancelled: Arc<AtomicBool>,
}
impl ActiveTask {
    pub fn begin(agent: &str, request_id: &str) -> Result<Self, String> {
        let cancelled = Arc::new(AtomicBool::new(false));
        let mut active = ACTIVE
            .get_or_init(|| Mutex::new(HashMap::new()))
            .lock()
            .unwrap();
        if active.contains_key(request_id) {
            return Err("requestId 已有进行中的工作区写入".into());
        }
        active.insert(
            request_id.into(),
            (agent.into(), String::new(), Arc::clone(&cancelled)),
        );
        Ok(Self {
            request_id: request_id.into(),
            cancelled,
        })
    }
    pub fn is_cancelled(&self) -> bool {
        self.cancelled.load(Ordering::SeqCst)
    }
    fn set_operation(&self, operation: &str) {
        if let Some(entry) = ACTIVE
            .get()
            .unwrap()
            .lock()
            .unwrap()
            .get_mut(&self.request_id)
        {
            entry.1 = operation.into();
        }
    }
}
impl Drop for ActiveTask {
    fn drop(&mut self) {
        if let Some(active) = ACTIVE.get() {
            let mut active = active.lock().unwrap();
            if active
                .get(&self.request_id)
                .is_some_and(|entry| Arc::ptr_eq(&entry.2, &self.cancelled))
            {
                active.remove(&self.request_id);
            }
        }
    }
}

pub fn cancel(client: Arc<Client>, request_id: &str) -> Result<(), String> {
    let active = ACTIVE
        .get_or_init(|| Mutex::new(HashMap::new()))
        .lock()
        .unwrap()
        .get(request_id)
        .cloned();
    let Some((agent, operation, cancelled)) = active else {
        return Err("写入任务尚未准备好或已结束".into());
    };
    cancelled.store(true, Ordering::SeqCst);
    if operation.is_empty() {
        return Ok(());
    }
    handle_cancel_result(client.workspace_write_cancel(&agent, &operation))
}

fn cancel_is_commit_busy(error: &DesktopApiError) -> bool {
    error.status == Some(409) && error.code.as_deref() == Some("workspace_operation_busy")
}
fn handle_cancel_result(result: Result<serde_json::Value, DesktopApiError>) -> Result<(), String> {
    match result {
        Ok(_) => Ok(()),
        Err(error) if cancel_is_commit_busy(&error) => Ok(()),
        Err(error) => Err(error.message),
    }
}

#[derive(serde::Serialize)]
#[serde(rename_all = "camelCase")]
pub struct CommandResult {
    pub status: String,
    pub path: Option<String>,
    pub name: Option<String>,
    pub size: Option<u64>,
    pub revision: Option<String>,
    pub file_ref: Option<serde_json::Value>,
    pub message: Option<String>,
    pub http_status: Option<u16>,
    pub error_code: Option<String>,
}
impl CommandResult {
    fn error(e: DesktopApiError) -> Self {
        let status = if e.status == Some(412) {
            "conflict"
        } else {
            "error"
        };
        Self {
            status: status.into(),
            path: None,
            name: None,
            size: None,
            revision: None,
            file_ref: None,
            message: Some(e.message),
            http_status: e.status,
            error_code: e.code,
        }
    }
    fn unknown(e: String) -> Self {
        Self {
            status: "unknown".into(),
            path: None,
            name: None,
            size: None,
            revision: None,
            file_ref: None,
            message: Some(e),
            http_status: None,
            error_code: Some("commit_result_unknown".into()),
        }
    }
    fn saved(r: WorkspaceWriteResult) -> Self {
        Self {
            status: "saved".into(),
            path: Some(r.path),
            name: Some(r.name),
            size: Some(r.size),
            revision: Some(r.revision),
            file_ref: Some(r.file_ref),
            message: None,
            http_status: Some(200),
            error_code: None,
        }
    }
}

pub fn write_bytes(
    app: tauri::AppHandle,
    client: Arc<Client>,
    agent: &str,
    path: &str,
    content: &[u8],
    expected: Option<&str>,
    create: bool,
    kind: &str,
    request_id: &str,
) -> CommandResult {
    let limit = write_limit(kind);
    if content.len() as u64 > limit {
        return size_error(limit);
    }
    let task = match ActiveTask::begin(agent, request_id) {
        Ok(t) => t,
        Err(e) => return CommandResult::error(api_error(e)),
    };
    write_reader(
        app,
        client,
        agent,
        path,
        std::io::Cursor::new(content),
        content.len() as u64,
        expected,
        None,
        create,
        kind,
        &task,
    )
}

fn write_reader<R: Read>(
    app: tauri::AppHandle,
    client: Arc<Client>,
    agent: &str,
    path: &str,
    mut source: R,
    size: u64,
    expected: Option<&str>,
    expected_source_hash: Option<String>,
    create: bool,
    kind: &str,
    task: &ActiveTask,
) -> CommandResult {
    let request_id = task.request_id.as_str();
    let cancelled = Arc::clone(&task.cancelled);
    let limit = write_limit(kind);
    if size > limit {
        return CommandResult {
            status: "error".into(),
            path: None,
            name: None,
            size: None,
            revision: None,
            file_ref: None,
            message: Some(format!("文件超过 {limit} 字节限制")),
            http_status: Some(413),
            error_code: Some("workspace_file_too_large".into()),
        };
    }
    let prepared = match client.workspace_write_prepare(
        agent,
        path,
        if create { "create" } else { "replace" },
        kind,
        expected,
        size,
    ) {
        Ok(v) => v,
        Err(e) => {
            return CommandResult::error(e);
        }
    };
    task.set_operation(&prepared.operation_id);
    if cancelled.load(Ordering::SeqCst) {
        let _ = client.workspace_write_cancel(agent, &prepared.operation_id);
        return CommandResult {
            status: "cancelled".into(),
            path: None,
            name: None,
            size: None,
            revision: None,
            file_ref: None,
            message: Some("写入已取消".into()),
            http_status: None,
            error_code: Some("cancelled".into()),
        };
    }
    let mut commit_started = false;
    let result = (|| -> Result<WorkspaceWriteResult, DesktopApiError> {
        let mut file = open_authorized_staging(
            &prepared.staging_root,
            &prepared.staging_path,
            &prepared.operation_id,
            &prepared.staging_identity,
        )?;
        file.set_len(0)
            .map_err(|e| api_error(format!("清空暂存文件失败: {e}")))?;
        let mut written = 0u64;
        let mut hash = Sha256::new();
        let mut buffer = vec![0u8; 256 * 1024];
        loop {
            if cancelled.load(Ordering::SeqCst) {
                return Err(api_error("写入已取消".into()));
            }
            let count = source
                .read(&mut buffer)
                .map_err(|e| api_error(format!("读取源文件失败: {e}")))?;
            if count == 0 {
                break;
            }
            if written + (count as u64) > size {
                return Err(api_error("源文件在读取期间增长".into()));
            }
            file.write_all(&buffer[..count])
                .map_err(|e| api_error(format!("写入暂存文件失败: {e}")))?;
            hash.update(&buffer[..count]);
            written += count as u64;
            use tauri::Emitter;
            let _=app.emit("workspace-write-progress",serde_json::json!({"requestId":request_id,"operationId":prepared.operation_id,"bytes":written,"total":size,"phase":"staging"}));
        }
        if written != size {
            return Err(api_error("源文件在读取期间缩短".into()));
        }
        let digest = hex::encode(hash.finalize());
        if expected_source_hash
            .as_ref()
            .is_some_and(|initial| !source_content_matches(initial, &digest))
        {
            return Err(api_error("源文件在导入期间发生变化，请重新选择".into()));
        }
        file.sync_all()
            .map_err(|e| api_error(format!("同步暂存文件失败: {e}")))?;
        drop(file);
        commit_started = true;
        use tauri::Emitter;
        let _=app.emit("workspace-write-progress",serde_json::json!({"requestId":request_id,"operationId":prepared.operation_id,"bytes":size,"total":size,"phase":"committing"}));
        client.workspace_write_commit(agent, &prepared.operation_id, size, &digest)
    })();
    let command_result = match result {
        Ok(r) => CommandResult::saved(r),
        Err(e) => {
            let unknown_commit =
                commit_started && (e.status.is_none() || e.status.is_some_and(|s| s >= 500));
            if !unknown_commit {
                let _ = client.workspace_write_cancel(agent, &prepared.operation_id);
            }
            if unknown_commit {
                CommandResult::unknown(e.message)
            } else if e.message.contains("已取消") {
                CommandResult {
                    status: "cancelled".into(),
                    path: None,
                    name: None,
                    size: None,
                    revision: None,
                    file_ref: None,
                    message: Some(e.message),
                    http_status: None,
                    error_code: Some("cancelled".into()),
                }
            } else {
                CommandResult::error(e)
            }
        }
    };
    task.set_operation("");
    command_result
}

#[cfg(unix)]
fn same_file_identity(a: &fs::Metadata, b: &fs::Metadata) -> bool {
    use std::os::unix::fs::MetadataExt;
    a.dev() == b.dev() && a.ino() == b.ino()
}
#[cfg(windows)]
fn same_file_identity(a: &fs::Metadata, b: &fs::Metadata) -> bool {
    let _ = (a, b);
    true
}
#[cfg(not(any(unix, windows)))]
fn same_file_identity(a: &fs::Metadata, b: &fs::Metadata) -> bool {
    a.len() == b.len() && a.modified().ok() == b.modified().ok()
}
#[cfg(unix)]
fn identity_string(file: &fs::File) -> Result<String, DesktopApiError> {
    use std::os::unix::fs::MetadataExt;
    let m = file.metadata().map_err(|e| api_error(e.to_string()))?;
    Ok(format!("unix:{:x}:{:x}", m.dev(), m.ino()))
}
#[cfg(windows)]
fn identity_string(file: &fs::File) -> Result<String, DesktopApiError> {
    use std::os::windows::io::AsRawHandle;
    use windows::Win32::{
        Foundation::HANDLE,
        Storage::FileSystem::{GetFileInformationByHandle, BY_HANDLE_FILE_INFORMATION},
    };
    let mut info = BY_HANDLE_FILE_INFORMATION::default();
    unsafe { GetFileInformationByHandle(HANDLE(file.as_raw_handle() as *mut _), &mut info) }
        .map_err(|e| api_error(e.to_string()))?;
    Ok(format!(
        "win:{:08x}:{:08x}:{:08x}",
        info.dwVolumeSerialNumber, info.nFileIndexHigh, info.nFileIndexLow
    ))
}

pub fn import_file(
    app: tauri::AppHandle,
    client: Arc<Client>,
    agent: &str,
    directory: &str,
    source: &Path,
    filename: &str,
    task: &ActiveTask,
) -> CommandResult {
    let safe = Path::new(filename)
        .file_name()
        .filter(|n| n.to_string_lossy() == filename)
        .map(|n| n.to_string_lossy().into_owned());
    let Some(name) = safe else {
        return CommandResult::error(api_error("文件名无效".into()));
    };
    let path = if directory.is_empty() {
        name
    } else {
        format!("{}/{}", directory.trim_end_matches('/'), name)
    };
    let mut file = match fs::File::open(source) {
        Ok(f) => f,
        Err(e) => return CommandResult::error(api_error(format!("无法打开所选文件: {e}"))),
    };
    let size = match file.metadata() {
        Ok(m) if m.is_file() => m.len(),
        Ok(_) => return CommandResult::error(api_error("所选对象不是普通文件".into())),
        Err(e) => return CommandResult::error(api_error(e.to_string())),
    };
    if size > IMPORT_LIMIT {
        return CommandResult::error(DesktopApiError {
            status: Some(413),
            code: Some("workspace_file_too_large".into()),
            message: "导入文件超过 1 GiB".into(),
            details: None,
        });
    }
    let mut before = Sha256::new();
    let mut buf = [0u8; 256 * 1024];
    let cancelled = Arc::clone(&task.cancelled);
    let request_id = task.request_id.as_str();
    let mut hashed = 0u64;
    let mut limited = Read::by_ref(&mut file).take(size.saturating_add(1));
    loop {
        if cancelled.load(Ordering::SeqCst) {
            return CommandResult {
                status: "cancelled".into(),
                path: None,
                name: None,
                size: None,
                revision: None,
                file_ref: None,
                message: Some("导入已取消".into()),
                http_status: None,
                error_code: Some("cancelled".into()),
            };
        }
        let n = match limited.read(&mut buf) {
            Ok(n) => n,
            Err(e) => return CommandResult::error(api_error(e.to_string())),
        };
        if n == 0 {
            break;
        }
        if hashed + (n as u64) > size {
            return size_error(IMPORT_LIMIT);
        }
        before.update(&buf[..n]);
        hashed += n as u64;
        use tauri::Emitter;
        let _=app.emit("workspace-write-progress",serde_json::json!({"requestId":request_id,"operationId":null,"bytes":hashed,"total":size,"phase":"hashing"}));
    }
    if hashed != size {
        return CommandResult::error(api_error("源文件在校验期间发生变化，请重新选择".into()));
    }
    let initial = hex::encode(before.finalize());
    if let Err(e) = file.seek(SeekFrom::Start(0)) {
        return CommandResult::error(api_error(e.to_string()));
    }
    write_reader(
        app,
        client,
        agent,
        &path,
        ExactFileReader {
            file,
            expected_size: size,
        },
        size,
        None,
        Some(initial),
        true,
        "import",
        task,
    )
}

fn api_error(message: String) -> DesktopApiError {
    DesktopApiError {
        status: None,
        code: Some("native_write_failed".into()),
        message,
        details: None,
    }
}
fn source_content_matches(expected: &str, actual: &str) -> bool {
    expected == actual
}

fn open_authorized_staging(
    root_path: &Path,
    stage_path: &Path,
    operation_id: &str,
    expected_identity: &str,
) -> Result<fs::File, DesktopApiError> {
    let root =
        fs::canonicalize(root_path).map_err(|e| api_error(format!("暂存根目录无效: {e}")))?;
    let canonical_stage =
        fs::canonicalize(stage_path).map_err(|e| api_error(format!("暂存文件路径无效: {e}")))?;
    if canonical_stage != root.join(operation_id) {
        return Err(api_error("Node 返回的暂存文件不匹配操作 ID".into()));
    }
    let parent = fs::canonicalize(
        stage_path
            .parent()
            .ok_or_else(|| api_error("暂存路径无父目录".into()))?,
    )
    .map_err(|e| api_error(format!("暂存目录无效: {e}")))?;
    if parent != root {
        return Err(api_error("Node 返回的暂存路径越界".into()));
    }
    let before = fs::symlink_metadata(stage_path)
        .map_err(|e| api_error(format!("无法访问暂存文件: {e}")))?;
    if !before.file_type().is_file() || before.file_type().is_symlink() {
        return Err(api_error("暂存路径不是普通文件".into()));
    }
    let mut options = OpenOptions::new();
    options.read(true).write(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        options.custom_flags(libc::O_NOFOLLOW | libc::O_CLOEXEC);
    }
    #[cfg(windows)]
    {
        use std::os::windows::fs::OpenOptionsExt;
        options.custom_flags(0x00200000);
    }
    let file = options
        .open(stage_path)
        .map_err(|e| api_error(format!("无法打开暂存文件: {e}")))?;
    let opened = file.metadata().map_err(|e| api_error(e.to_string()))?;
    if !opened.is_file()
        || !same_file_identity(&before, &opened)
        || identity_string(&file)? != expected_identity
    {
        return Err(api_error("暂存文件身份与Node授权不匹配".into()));
    }
    Ok(file)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn text_and_import_limits_are_distinct() {
        assert_eq!(write_limit("text"), 16 * 1024 * 1024);
        assert_eq!(write_limit("import"), 1024 * 1024 * 1024);
    }

    #[test]
    fn inline_import_is_capped_at_16_mib_while_native_import_allows_1_gib() {
        assert!(validate_inline_import_size(INLINE_IMPORT_LIMIT).is_ok());
        assert!(validate_inline_import_size(INLINE_IMPORT_LIMIT + 1).is_err());
        assert_eq!(write_limit("import"), 1024 * 1024 * 1024);
    }

    #[test]
    fn replaced_staging_path_hardlink_is_rejected_without_truncating_target() {
        let dir = tempfile::tempdir().unwrap();
        let root = dir.path().join("stage");
        fs::create_dir(&root).unwrap();
        let stage = root.join("op1");
        let outside = dir.path().join("outside");
        fs::write(&stage, b"authorized").unwrap();
        fs::write(&outside, b"do not truncate").unwrap();
        let authorized = identity_string(&fs::File::open(&stage).unwrap()).unwrap();
        fs::remove_file(&stage).unwrap();
        fs::hard_link(&outside, &stage).unwrap();
        assert!(open_authorized_staging(&root, &stage, "op1", &authorized).is_err());
        assert_eq!(fs::read(outside).unwrap(), b"do not truncate");
    }

    #[cfg(windows)]
    #[test]
    fn ordinary_node_staging_path_matches_canonical_extended_windows_path() {
        let dir = tempfile::tempdir().unwrap();
        let canonical = fs::canonicalize(dir.path()).unwrap();
        let canonical_text = canonical.to_string_lossy();
        let ordinary = Path::new(
            canonical_text
                .strip_prefix(r"\\?\")
                .unwrap_or(&canonical_text),
        )
        .to_path_buf();
        let stage = ordinary.join("op-normal-win");
        fs::write(&stage, b"staged").unwrap();
        let identity = identity_string(&fs::File::open(&stage).unwrap()).unwrap();
        let opened =
            open_authorized_staging(&ordinary, &stage, "op-normal-win", &identity).unwrap();
        opened.set_len(0).unwrap();
        assert_eq!(fs::metadata(&stage).unwrap().len(), 0);
    }

    #[test]
    fn unknown_commit_result_is_preserved_for_the_ui() {
        let result = CommandResult::unknown("Node response was lost".into());
        assert_eq!(result.status, "unknown");
        assert_eq!(result.error_code.as_deref(), Some("commit_result_unknown"));
        assert!(result.path.is_none() && result.revision.is_none());
    }

    #[test]
    fn changed_source_hash_is_rejected() {
        let before = hex::encode(Sha256::digest(b"version one"));
        let copied = hex::encode(Sha256::digest(b"version two"));
        assert!(!source_content_matches(&before, &copied));
        assert!(source_content_matches(&before, &before));
    }

    #[test]
    fn dialog_task_can_be_cancelled_before_prepare_and_duplicate_ids_are_rejected() {
        let id = "preparing-write-test";
        let task = ActiveTask::begin("agent", id).unwrap();
        assert!(ActiveTask::begin("agent", id).is_err());
        cancel(Arc::new(Client::new("")), id).unwrap();
        assert!(task.is_cancelled());
        drop(task);
        assert!(!ACTIVE.get().unwrap().lock().unwrap().contains_key(id));
    }

    #[test]
    fn cancel_late_during_commit_is_not_reported_as_a_save_failure() {
        let busy = DesktopApiError {
            status: Some(409),
            code: Some("workspace_operation_busy".into()),
            message: "commit in progress".into(),
            details: None,
        };
        assert_eq!(handle_cancel_result(Err(busy)), Ok(()));
        let conflict = DesktopApiError {
            status: Some(409),
            code: Some("workspace_revision_mismatch".into()),
            message: "conflict".into(),
            details: None,
        };
        assert_eq!(handle_cancel_result(Err(conflict)), Err("conflict".into()));
    }
}

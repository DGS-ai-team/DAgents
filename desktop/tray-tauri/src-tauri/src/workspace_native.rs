use crate::nodeclient::Client;
use sha2::Digest;
use std::fs::{self, File, OpenOptions};
use std::io::{Read, Seek, SeekFrom, Write};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use tauri::AppHandle;
use tauri_plugin_dialog::DialogExt;

static TEMP_SEQUENCE: AtomicU64 = AtomicU64::new(0);

#[derive(Debug, Clone, serde::Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct SaveAsResult {
    pub status: String,
    pub bytes_written: u64,
}

pub async fn save_as(
    app: AppHandle,
    client: Arc<Client>,
    agent_id: String,
    relative_path: String,
    draft_content: Option<String>,
) -> Result<SaveAsResult, String> {
    tauri::async_runtime::spawn_blocking(move || {
        let location = client.workspace_location(&agent_id)?;
        let (source_path, root) = crate::workspace_read::validate_workspace_file_path(
            Path::new(&location.root),
            &relative_path,
        )?;
        let source = match crate::workspace_read::open_workspace_file(&root, &relative_path) {
            Ok((file, _)) => Some(file),
            Err(error) if draft_content.is_some() && error.starts_with("WORKSPACE_NOT_FOUND:") => {
                None
            }
            Err(error) => return Err(error),
        };
        if draft_content.is_none() && source.is_none() {
            return Err("工作区源文件已不存在，无法另存当前文件".into());
        }
        let mut source = source;
        let suggested_name = Path::new(&relative_path)
            .file_name()
            .ok_or_else(|| "工作区文件名无效".to_string())?
            .to_string_lossy()
            .into_owned();
        // The blocking native dialog and the copy both run off the WebView thread.
        let selected = app
            .dialog()
            .file()
            .set_file_name(&suggested_name)
            .blocking_save_file();
        let Some(selected) = selected else {
            return Ok(SaveAsResult {
                status: "cancelled".into(),
                bytes_written: 0,
            });
        };
        let target = selected
            .into_path()
            .map_err(|e| format!("保存位置无效: {e}"))?;
        if let Some(draft) = draft_content {
            replace_with_draft(
                &target,
                source.as_ref(),
                &root,
                &relative_path,
                &source_path,
                draft.as_bytes(),
            )
        } else {
            let Some(source) = source.as_mut() else {
                return Err("工作区源文件已不存在，无法另存当前文件".into());
            };
            replace_with_file(source, &target)
        }
    })
    .await
    .map_err(|error| format!("工作区另存任务失败: {error}"))?
}

pub async fn reveal(
    client: Arc<Client>,
    agent_id: String,
    relative_path: String,
) -> Result<(), String> {
    tauri::async_runtime::spawn_blocking(move || {
        let location = client.workspace_location(&agent_id)?;
        let (file, _root) =
            crate::workspace_read::open_workspace_file(Path::new(&location.root), &relative_path)?;
        let path = opened_file_path(&file)?;
        #[cfg(target_os = "windows")]
        {
            let mut select_argument = std::ffi::OsString::from("/select,");
            select_argument.push(&path);
            std::process::Command::new("explorer.exe")
                .arg(select_argument)
                .spawn()
                .map_err(|e| format!("无法在文件管理器中显示文件: {e}"))?;
            Ok(())
        }
        #[cfg(not(target_os = "windows"))]
        {
            let parent = path
                .parent()
                .ok_or_else(|| "工作区文件没有父目录".to_string())?;
            open::that(parent).map_err(|e| format!("无法打开文件管理器: {e}"))
        }
    })
    .await
    .map_err(|error| format!("工作区定位任务失败: {error}"))?
}

fn opened_file_path(file: &File) -> Result<PathBuf, String> {
    #[cfg(windows)]
    {
        use std::os::windows::io::AsRawHandle;
        use windows::Win32::Foundation::HANDLE;
        use windows::Win32::Storage::FileSystem::{
            GetFinalPathNameByHandleW, GETFINALPATHNAMEBYHANDLE_FLAGS,
        };
        let mut buffer = vec![0u16; 32768];
        let count = unsafe {
            GetFinalPathNameByHandleW(
                HANDLE(file.as_raw_handle() as *mut _),
                &mut buffer,
                GETFINALPATHNAMEBYHANDLE_FLAGS(0),
            )
        };
        if count == 0 || count as usize >= buffer.len() {
            return Err("无法读取已授权文件的实际路径".into());
        }
        buffer.truncate(count as usize);
        let path = String::from_utf16_lossy(&buffer);
        let path = if let Some(unc) = path.strip_prefix("\\\\?\\UNC\\") {
            format!("\\\\{unc}")
        } else {
            path.strip_prefix("\\\\?\\").unwrap_or(&path).to_string()
        };
        Ok(PathBuf::from(path))
    }
    #[cfg(unix)]
    {
        use std::os::unix::io::AsRawFd;
        fs::canonicalize(format!("/proc/self/fd/{}", file.as_raw_fd()))
            .map_err(|e| format!("无法读取已授权文件的实际路径: {e}"))
    }
    #[cfg(not(any(unix, windows)))]
    {
        let _ = file;
        Err("此平台无法安全定位工作区文件".into())
    }
}

fn replace_with_file(source: &mut File, target: &Path) -> Result<SaveAsResult, String> {
    let source_metadata = source
        .metadata()
        .map_err(|e| format!("检查源文件失败: {e}"))?;
    let (mut temporary, temporary_path) = create_temp_sibling(target)?;
    let result = (|| {
        source
            .seek(SeekFrom::Start(0))
            .map_err(|e| format!("定位源文件失败: {e}"))?;
        let mut copied_hash = sha2::Sha256::new();
        let mut buffer = [0u8; 256 * 1024];
        let mut bytes_written = 0u64;
        loop {
            let count = source
                .read(&mut buffer)
                .map_err(|e| format!("读取工作区文件失败: {e}"))?;
            if count == 0 {
                break;
            }
            temporary
                .write_all(&buffer[..count])
                .map_err(|e| format!("写入另存文件失败: {e}"))?;
            sha2::Digest::update(&mut copied_hash, &buffer[..count]);
            bytes_written += count as u64;
        }
        temporary
            .sync_all()
            .map_err(|e| format!("写入另存文件失败: {e}"))?;
        // Catch source edits that raced with the streaming copy before publishing it.
        source
            .seek(SeekFrom::Start(0))
            .map_err(|e| format!("定位源文件失败: {e}"))?;
        let mut check_hash = sha2::Sha256::new();
        loop {
            let count = source
                .read(&mut buffer)
                .map_err(|e| format!("校验源文件失败: {e}"))?;
            if count == 0 {
                break;
            }
            sha2::Digest::update(&mut check_hash, &buffer[..count]);
        }
        if sha2::Digest::finalize(copied_hash) != sha2::Digest::finalize(check_hash) {
            return Err("源文件在另存期间发生变化，请重试".into());
        }
        if bytes_written != source_metadata.len() {
            return Err("源文件在另存期间发生变化，请重试".into());
        }
        drop(temporary);
        publish_temp(&temporary_path, target, Some(source))?;
        Ok(SaveAsResult {
            status: "saved".into(),
            bytes_written,
        })
    })();
    if result.is_err() {
        let _ = fs::remove_file(&temporary_path);
    }
    result
}

fn replace_with_draft(
    target: &Path,
    original_source: Option<&File>,
    root: &Path,
    relative_path: &str,
    original_source_path: &Path,
    bytes: &[u8],
) -> Result<SaveAsResult, String> {
    let (mut temporary, temporary_path) = create_temp_sibling(target)?;
    let result = (|| {
        temporary
            .write_all(bytes)
            .map_err(|e| format!("写入草稿副本失败: {e}"))?;
        temporary
            .sync_all()
            .map_err(|e| format!("写入草稿副本失败: {e}"))?;
        let (current_source_path, _current_root) =
            crate::workspace_read::validate_workspace_file_path(root, relative_path)?;
        if same_path(target, original_source_path)? || same_path(target, &current_source_path)? {
            return Err("另存目标仍是工作区源文件路径，已取消以保护本地草稿".into());
        }
        let current_source = match crate::workspace_read::open_workspace_file(root, relative_path) {
            Ok((file, _)) => Some(file),
            Err(error) if error.starts_with("WORKSPACE_NOT_FOUND:") => None,
            Err(error) => return Err(error),
        };
        if let Some(source) = original_source {
            if existing_target_is_same_file_handle(source, target)? {
                return Err("另存目标与工作区源文件相同，已取消以保护源文件".into());
            }
        }
        if let Some(source) = current_source.as_ref() {
            if existing_target_is_same_file_handle(source, target)? {
                return Err("另存目标与工作区源文件相同，已取消以保护源文件".into());
            }
        }
        drop(temporary);
        publish_temp(&temporary_path, target, None)?;
        Ok(SaveAsResult {
            status: "saved".into(),
            bytes_written: bytes.len() as u64,
        })
    })();
    if result.is_err() {
        let _ = fs::remove_file(&temporary_path);
    }
    result
}

fn same_path(left: &Path, right: &Path) -> Result<bool, String> {
    fn canonicalize_through_missing(path: &Path) -> Result<PathBuf, String> {
        let mut current = path.to_path_buf();
        let mut missing = Vec::new();
        loop {
            match fs::symlink_metadata(&current) {
                Ok(_) => {
                    let mut resolved =
                        fs::canonicalize(&current).map_err(|e| format!("无法检查文件路径: {e}"))?;
                    for component in missing.iter().rev() {
                        resolved.push(component);
                    }
                    return Ok(resolved);
                }
                Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
                    let name = current
                        .file_name()
                        .ok_or_else(|| "文件路径没有可用的父目录".to_string())?;
                    missing.push(name.to_os_string());
                    if !current.pop() {
                        return Err("无法解析文件路径".into());
                    }
                }
                Err(error) => return Err(format!("无法检查文件路径: {error}")),
            }
        }
    }
    let left = canonicalize_through_missing(left)?;
    let right = canonicalize_through_missing(right)?;
    #[cfg(windows)]
    {
        Ok(left
            .to_string_lossy()
            .eq_ignore_ascii_case(&right.to_string_lossy()))
    }
    #[cfg(not(windows))]
    {
        Ok(left == right)
    }
}

fn create_temp_sibling(target: &Path) -> Result<(File, PathBuf), String> {
    let parent = target
        .parent()
        .ok_or_else(|| "保存位置没有父目录".to_string())?;
    for _ in 0..32 {
        let id = TEMP_SEQUENCE.fetch_add(1, Ordering::Relaxed);
        let name = format!(".dagents-save-{}-{id}.tmp", std::process::id());
        let path = parent.join(name);
        match OpenOptions::new().write(true).create_new(true).open(&path) {
            Ok(file) => return Ok((file, path)),
            Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => continue,
            Err(error) => return Err(format!("无法创建临时另存文件: {error}")),
        }
    }
    Err("无法分配临时另存文件名".into())
}

fn publish_temp(temp: &Path, target: &Path, source: Option<&File>) -> Result<(), String> {
    reject_reparse_target(target)?;
    if let Some(source) = source {
        if existing_target_is_same_file_handle(source, target)? {
            return Err("另存目标与工作区源文件相同或为其硬链接，已取消以保护源文件".into());
        }
    }
    if target.exists() {
        replace_existing(temp, target).map_err(|e| format!("替换另存目标失败，原文件已保留: {e}"))
    } else {
        publish_new(temp, target).map_err(|e| format!("发布另存文件失败: {e}"))
    }
}

#[cfg(unix)]
fn publish_new(temp: &Path, target: &Path) -> std::io::Result<()> {
    // A same-directory hard link publishes atomically and fails if another
    // process created the destination after the existence check.
    fs::hard_link(temp, target)?;
    let _ = fs::remove_file(temp);
    Ok(())
}

#[cfg(not(unix))]
fn publish_new(temp: &Path, target: &Path) -> std::io::Result<()> {
    // Windows rename does not replace an existing destination.
    fs::rename(temp, target)
}

fn reject_reparse_target(target: &Path) -> Result<(), String> {
    match fs::symlink_metadata(target) {
        Ok(meta) if meta.file_type().is_symlink() => {
            Err("另存目标是符号链接，已取消以保护现有文件".into())
        }
        Ok(meta) => {
            #[cfg(windows)]
            {
                use std::os::windows::fs::MetadataExt;
                if meta.file_attributes() & 0x400 != 0 {
                    return Err("另存目标是重解析点，已取消".into());
                }
            }
            if meta.is_file() {
                Ok(())
            } else {
                Err("另存目标不是普通文件".into())
            }
        }
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(error) => Err(format!("检查另存目标失败: {error}")),
    }
}

fn existing_target_is_same_file_handle(source: &File, target: &Path) -> Result<bool, String> {
    if !target.exists() {
        return Ok(false);
    }
    reject_reparse_target(target)?;
    let candidate = File::open(target).map_err(|e| format!("检查另存目标失败: {e}"))?;
    same_file(source, &candidate)
}

#[cfg(windows)]
fn same_file(a: &File, b: &File) -> Result<bool, String> {
    use std::os::windows::io::AsRawHandle;
    use windows::Win32::Foundation::HANDLE;
    use windows::Win32::Storage::FileSystem::{
        GetFileInformationByHandle, BY_HANDLE_FILE_INFORMATION,
    };
    let mut a_info = BY_HANDLE_FILE_INFORMATION::default();
    let mut b_info = BY_HANDLE_FILE_INFORMATION::default();
    unsafe {
        GetFileInformationByHandle(HANDLE(a.as_raw_handle() as *mut _), &mut a_info)
            .map_err(|e| format!("读取源文件身份失败: {e}"))?;
        GetFileInformationByHandle(HANDLE(b.as_raw_handle() as *mut _), &mut b_info)
            .map_err(|e| format!("读取目标文件身份失败: {e}"))?;
    }
    Ok(a_info.dwVolumeSerialNumber == b_info.dwVolumeSerialNumber
        && a_info.nFileIndexHigh == b_info.nFileIndexHigh
        && a_info.nFileIndexLow == b_info.nFileIndexLow)
}

#[cfg(unix)]
fn same_file(a: &File, b: &File) -> Result<bool, String> {
    use std::os::unix::fs::MetadataExt;
    let a = a
        .metadata()
        .map_err(|e| format!("读取源文件身份失败: {e}"))?;
    let b = b
        .metadata()
        .map_err(|e| format!("读取目标文件身份失败: {e}"))?;
    Ok(a.dev() == b.dev() && a.ino() == b.ino())
}

#[cfg(not(any(unix, windows)))]
fn same_file(a: &File, b: &File) -> Result<bool, String> {
    let a = a
        .metadata()
        .map_err(|e| format!("读取源文件身份失败: {e}"))?;
    let b = b
        .metadata()
        .map_err(|e| format!("读取目标文件身份失败: {e}"))?;
    Ok(a.len() == b.len() && a.modified().ok() == b.modified().ok())
}

#[cfg(windows)]
fn replace_existing(temp: &Path, target: &Path) -> std::io::Result<()> {
    use std::os::windows::ffi::OsStrExt;
    use windows::core::PCWSTR;
    use windows::Win32::Storage::FileSystem::{ReplaceFileW, REPLACE_FILE_FLAGS};
    let temp: Vec<u16> = temp.as_os_str().encode_wide().chain(Some(0)).collect();
    let target: Vec<u16> = target.as_os_str().encode_wide().chain(Some(0)).collect();
    unsafe {
        ReplaceFileW(
            PCWSTR(target.as_ptr()),
            PCWSTR(temp.as_ptr()),
            PCWSTR::null(),
            REPLACE_FILE_FLAGS(0),
            None,
            None,
        )
    }
    .map_err(|error| std::io::Error::other(error.to_string()))
}

#[cfg(not(windows))]
fn replace_existing(temp: &Path, target: &Path) -> std::io::Result<()> {
    fs::rename(temp, target)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn saves_stream_and_replaces_existing_target() {
        let dir = tempfile::tempdir().unwrap();
        let source_path = dir.path().join("source.bin");
        let target = dir.path().join("target.bin");
        fs::write(&source_path, vec![37u8; 2 * 1024 * 1024 + 3]).unwrap();
        fs::write(&target, b"old target").unwrap();
        let mut source = File::open(&source_path).unwrap();
        let result = replace_with_file(&mut source, &target).unwrap();
        assert_eq!(result.status, "saved");
        assert_eq!(result.bytes_written, 2 * 1024 * 1024 + 3);
        assert_eq!(fs::read(target).unwrap(), vec![37u8; 2 * 1024 * 1024 + 3]);
    }

    #[test]
    fn refuses_same_file_and_hard_link_without_changing_source() {
        let dir = tempfile::tempdir().unwrap();
        let root = fs::canonicalize(dir.path()).unwrap();
        let source_path = root.join("source.txt");
        fs::write(&source_path, b"preserve me").unwrap();
        let hard_link = dir.path().join("hard-link.txt");
        fs::hard_link(&source_path, &hard_link).unwrap();
        let mut source = File::open(&source_path).unwrap();
        assert!(replace_with_file(&mut source, &hard_link)
            .unwrap_err()
            .contains("硬链接"));
        assert_eq!(fs::read(&source_path).unwrap(), b"preserve me");
        assert!(replace_with_draft(
            &source_path,
            Some(&source),
            &root,
            "source.txt",
            &source_path,
            b"draft",
        )
        .unwrap_err()
        .contains("源文件路径"));
        assert_eq!(fs::read(&source_path).unwrap(), b"preserve me");
    }

    #[test]
    fn exports_draft_bytes_without_changing_the_authorized_source() {
        let dir = tempfile::tempdir().unwrap();
        let root = fs::canonicalize(dir.path()).unwrap();
        let source_path = root.join("source.txt");
        let target = root.join("export.txt");
        fs::write(&source_path, b"older saved version").unwrap();
        let source = File::open(&source_path).unwrap();
        let result = replace_with_draft(
            &target,
            Some(&source),
            &root,
            "source.txt",
            &source_path,
            "当前草稿".as_bytes(),
        )
        .unwrap();
        assert_eq!(result.status, "saved");
        assert_eq!(fs::read(target).unwrap(), "当前草稿".as_bytes());
        assert_eq!(fs::read(source_path).unwrap(), b"older saved version");
    }

    #[test]
    fn exports_retained_draft_when_original_file_is_missing() {
        let dir = tempfile::tempdir().unwrap();
        let workspace_dir = dir.path().join("workspace");
        fs::create_dir(&workspace_dir).unwrap();
        let root = fs::canonicalize(&workspace_dir).unwrap();
        let exports = dir.path().join("exports");
        fs::create_dir(&exports).unwrap();
        let source_path = root.join("notes.md");
        let target = exports.join("notes.md");
        let (validated_path, _) =
            crate::workspace_read::validate_workspace_file_path(&root, "notes.md").unwrap();
        assert_eq!(validated_path, source_path);
        let result = replace_with_draft(
            &target,
            None,
            &root,
            "notes.md",
            &source_path,
            b"retained draft",
        )
        .unwrap();
        assert_eq!(result.status, "saved");
        assert_eq!(fs::read(target).unwrap(), b"retained draft");
        assert!(!source_path.exists());
    }

    #[test]
    fn exports_retained_draft_when_original_parent_directory_is_missing() {
        let dir = tempfile::tempdir().unwrap();
        let workspace_dir = dir.path().join("workspace");
        let exports = dir.path().join("exports");
        fs::create_dir(&workspace_dir).unwrap();
        fs::create_dir(&exports).unwrap();
        let root = fs::canonicalize(&workspace_dir).unwrap();
        let deleted_parent = root.join("deleted-parent");
        fs::create_dir(&deleted_parent).unwrap();
        let source_path = deleted_parent.join("notes.md");
        fs::remove_dir(&deleted_parent).unwrap();
        let target = exports.join("notes.md");

        let (validated_path, _) =
            crate::workspace_read::validate_workspace_file_path(&root, "deleted-parent/notes.md")
                .unwrap();
        assert_eq!(validated_path, source_path);
        let result = replace_with_draft(
            &target,
            None,
            &root,
            "deleted-parent/notes.md",
            &source_path,
            b"draft after parent removal",
        )
        .unwrap();
        assert_eq!(result.status, "saved");
        assert_eq!(fs::read(target).unwrap(), b"draft after parent removal");
        assert!(!deleted_parent.exists());
    }

    #[test]
    fn missing_draft_path_cannot_be_recreated_by_export_or_escape_workspace() {
        let dir = tempfile::tempdir().unwrap();
        let workspace_dir = dir.path().join("workspace");
        fs::create_dir(&workspace_dir).unwrap();
        let root = fs::canonicalize(&workspace_dir).unwrap();
        let source_path = root.join("notes.md");
        assert!(replace_with_draft(
            &source_path,
            None,
            &root,
            "notes.md",
            &source_path,
            b"draft",
        )
        .unwrap_err()
        .contains("源文件路径"));
        assert!(!source_path.exists());
        assert!(
            crate::workspace_read::validate_workspace_file_path(&root, ".dagents/private.db")
                .is_err()
        );
        assert!(
            crate::workspace_read::validate_workspace_file_path(&root, "../outside.txt").is_err()
        );
    }

    #[test]
    fn rejects_reappeared_source_path_and_hard_link_for_missing_draft() {
        let dir = tempfile::tempdir().unwrap();
        let workspace_dir = dir.path().join("workspace");
        let export_dir = dir.path().join("exports");
        fs::create_dir(&workspace_dir).unwrap();
        fs::create_dir(&export_dir).unwrap();
        let root = fs::canonicalize(&workspace_dir).unwrap();
        let source_path = root.join("notes.md");
        let target = export_dir.join("notes.md");

        fs::write(&target, b"existing destination").unwrap();
        fs::hard_link(&target, &source_path).unwrap();
        let error = replace_with_draft(
            &target,
            None,
            &root,
            "notes.md",
            &source_path,
            b"retained draft",
        )
        .unwrap_err();
        assert!(error.contains("相同"));
        assert_eq!(fs::read(&target).unwrap(), b"existing destination");
        assert_eq!(fs::read(&source_path).unwrap(), b"existing destination");
    }

    #[test]
    fn failed_destination_keeps_preexisting_target_and_cleans_temp() {
        let dir = tempfile::tempdir().unwrap();
        let source_path = dir.path().join("source");
        let destination_dir = dir.path().join("destination-dir");
        fs::write(&source_path, b"source").unwrap();
        fs::create_dir(&destination_dir).unwrap();
        let mut source = File::open(&source_path).unwrap();
        assert!(replace_with_file(&mut source, &destination_dir).is_err());
        assert!(destination_dir.is_dir());
        assert_eq!(fs::read(source_path).unwrap(), b"source");
        assert_eq!(fs::read_dir(dir.path()).unwrap().count(), 2);
    }

    #[cfg(unix)]
    #[test]
    fn publishing_a_new_file_never_overwrites_a_destination_created_late() {
        let dir = tempfile::tempdir().unwrap();
        let temp = dir.path().join("temp");
        let target = dir.path().join("target");
        fs::write(&temp, b"new").unwrap();
        fs::write(&target, b"preexisting").unwrap();
        assert!(publish_new(&temp, &target).is_err());
        assert_eq!(fs::read(&target).unwrap(), b"preexisting");
        assert_eq!(fs::read(&temp).unwrap(), b"new");
    }
}

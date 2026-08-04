//! Shell 单实例：Windows 命名互斥体；其它平台文件锁。

use crate::layout::Layout;

#[cfg(not(windows))]
use std::fs::{self, File, OpenOptions};
#[cfg(not(windows))]
use std::io::Write;
#[cfg(not(windows))]
use std::path::PathBuf;

pub struct InstanceGuard {
    #[cfg(windows)]
    _mutex: windows_sys::Win32::Foundation::HANDLE,
    #[cfg(not(windows))]
    _file: File,
    #[cfg(not(windows))]
    path: PathBuf,
}

#[cfg(windows)]
impl Drop for InstanceGuard {
    fn drop(&mut self) {
        if !self._mutex.is_null() {
            unsafe {
                windows_sys::Win32::Foundation::CloseHandle(self._mutex);
            }
            self._mutex = std::ptr::null_mut();
        }
    }
}

#[cfg(not(windows))]
impl Drop for InstanceGuard {
    fn drop(&mut self) {
        let _ = fs::remove_file(&self.path);
    }
}

#[derive(Debug, thiserror::Error)]
pub enum InstanceError {
    #[error("another instance is already running")]
    AlreadyRunning,
    #[error("{0}")]
    Other(String),
}

pub fn acquire(layout: &Layout) -> Result<InstanceGuard, InstanceError> {
    #[cfg(windows)]
    {
        use std::os::windows::ffi::OsStrExt;
        use windows_sys::Win32::Foundation::{GetLastError, ERROR_ALREADY_EXISTS, HANDLE};
        use windows_sys::Win32::System::Threading::CreateMutexW;

        let name = format!(
            "Local\\DAgentsShell-{}",
            hash_path(&layout.config_path.to_string_lossy())
        );
        let wide: Vec<u16> = std::ffi::OsStr::new(&name)
            .encode_wide()
            .chain(std::iter::once(0))
            .collect();
        let handle: HANDLE = unsafe { CreateMutexW(std::ptr::null(), 0, wide.as_ptr()) };
        if handle.is_null() {
            return Err(InstanceError::Other(format!(
                "CreateMutexW failed: {}",
                unsafe { GetLastError() }
            )));
        }
        if unsafe { GetLastError() } == ERROR_ALREADY_EXISTS {
            unsafe {
                windows_sys::Win32::Foundation::CloseHandle(handle);
            }
            return Err(InstanceError::AlreadyRunning);
        }
        Ok(InstanceGuard { _mutex: handle })
    }

    #[cfg(not(windows))]
    {
        let runtime = layout.home.join(".runtime");
        fs::create_dir_all(&runtime)
            .map_err(|e| InstanceError::Other(format!("创建 .runtime: {e}")))?;
        let path = runtime.join("shell.lock");
        let mut file = OpenOptions::new()
            .create(true)
            .write(true)
            .open(&path)
            .map_err(|e| InstanceError::Other(format!("打开 shell.lock: {e}")))?;
        #[cfg(unix)]
        {
            // advisory lock via flock (LOCK_EX | LOCK_NB)
            let fd = std::os::unix::io::AsRawFd::as_raw_fd(&file);
            let rc = unsafe { libc_flock(fd, 2 | 4) };
            if rc != 0 {
                return Err(InstanceError::AlreadyRunning);
            }
        }
        let _ = writeln!(file, "{}", std::process::id());
        Ok(InstanceGuard {
            _file: file,
            path,
        })
    }
}

#[cfg(windows)]
fn hash_path(s: &str) -> u64 {
    // FNV-1a 64
    let mut h: u64 = 0xcbf29ce484222325;
    for b in s.bytes() {
        h ^= u64::from(b);
        h = h.wrapping_mul(0x100000001b3);
    }
    h
}

#[cfg(unix)]
unsafe fn libc_flock(fd: i32, op: i32) -> i32 {
    // Avoid libc crate dependency: declare flock.
    extern "C" {
        fn flock(fd: i32, operation: i32) -> i32;
    }
    flock(fd, op)
}

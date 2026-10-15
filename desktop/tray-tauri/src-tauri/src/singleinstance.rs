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
    #[cfg(windows)]
    _quit_event: windows_sys::Win32::Foundation::HANDLE,
    #[cfg(windows)]
    _show_event: windows_sys::Win32::Foundation::HANDLE,
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
        if !self._quit_event.is_null() {
            unsafe {
                windows_sys::Win32::Foundation::CloseHandle(self._quit_event);
            }
            self._quit_event = std::ptr::null_mut();
        }
        if !self._show_event.is_null() {
            unsafe {
                windows_sys::Win32::Foundation::CloseHandle(self._show_event);
            }
            self._show_event = std::ptr::null_mut();
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
        use windows_sys::Win32::System::Threading::{CreateEventW, CreateMutexW};

        let name = mutex_name(layout);
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
        let event_name = quit_event_name(layout);
        let event_wide: Vec<u16> = std::ffi::OsStr::new(&event_name)
            .encode_wide()
            .chain(std::iter::once(0))
            .collect();
        let quit_event = unsafe { CreateEventW(std::ptr::null(), 0, 0, event_wide.as_ptr()) };
        if quit_event.is_null() {
            unsafe { windows_sys::Win32::Foundation::CloseHandle(handle) };
            return Err(InstanceError::Other(format!(
                "CreateEventW failed: {}",
                unsafe { GetLastError() }
            )));
        }
        let show_name = show_event_name(layout);
        let show_wide: Vec<u16> = std::ffi::OsStr::new(&show_name)
            .encode_wide()
            .chain(std::iter::once(0))
            .collect();
        let show_event = unsafe { CreateEventW(std::ptr::null(), 0, 0, show_wide.as_ptr()) };
        if show_event.is_null() {
            unsafe {
                windows_sys::Win32::Foundation::CloseHandle(quit_event);
                windows_sys::Win32::Foundation::CloseHandle(handle);
            }
            return Err(InstanceError::Other(format!(
                "CreateEventW (show) failed: {}",
                unsafe { GetLastError() }
            )));
        }
        Ok(InstanceGuard {
            _mutex: handle,
            _quit_event: quit_event,
            _show_event: show_event,
        })
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
        Ok(InstanceGuard { _file: file, path })
    }
}

pub fn is_running(layout: &Layout) -> Result<bool, String> {
    #[cfg(windows)]
    {
        use std::os::windows::ffi::OsStrExt;
        use windows_sys::Win32::Foundation::{CloseHandle, GetLastError, ERROR_FILE_NOT_FOUND};
        use windows_sys::Win32::System::Threading::OpenMutexW;
        const SYNCHRONIZE: u32 = 0x0010_0000;
        let wide: Vec<u16> = std::ffi::OsStr::new(&mutex_name(layout))
            .encode_wide()
            .chain(std::iter::once(0))
            .collect();
        let mutex = unsafe { OpenMutexW(SYNCHRONIZE, 0, wide.as_ptr()) };
        if mutex.is_null() {
            return if unsafe { GetLastError() } == ERROR_FILE_NOT_FOUND {
                Ok(false)
            } else {
                Err(format!("检查 Desktop 实例失败: {}", unsafe {
                    GetLastError()
                }))
            };
        }
        unsafe { CloseHandle(mutex) };
        Ok(true)
    }
    #[cfg(not(windows))]
    {
        use std::fs::OpenOptions;
        use std::os::unix::io::AsRawFd;
        let path = layout.home.join(".runtime").join("shell.lock");
        let file = match OpenOptions::new().read(true).write(true).open(path) {
            Ok(file) => file,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(false),
            Err(error) => return Err(format!("检查 Desktop 锁失败: {error}")),
        };
        let fd = file.as_raw_fd();
        let rc = unsafe { libc_flock(fd, 2 | 4) };
        if rc != 0 {
            return Ok(true);
        }
        let _ = unsafe { libc_flock(fd, 8) };
        Ok(false)
    }
}

#[cfg(windows)]
pub fn request_quit_and_wait(layout: &Layout, timeout: std::time::Duration) -> Result<(), String> {
    use std::os::windows::ffi::OsStrExt;
    use windows_sys::Win32::Foundation::{CloseHandle, GetLastError, ERROR_FILE_NOT_FOUND};
    use windows_sys::Win32::System::Threading::{
        OpenEventW, OpenMutexW, SetEvent, EVENT_MODIFY_STATE,
    };
    const SYNCHRONIZE: u32 = 0x0010_0000;
    let mutex_wide: Vec<u16> = std::ffi::OsStr::new(&mutex_name(layout))
        .encode_wide()
        .chain(std::iter::once(0))
        .collect();
    let mutex = unsafe { OpenMutexW(SYNCHRONIZE, 0, mutex_wide.as_ptr()) };
    if mutex.is_null() {
        if unsafe { GetLastError() } == ERROR_FILE_NOT_FOUND {
            return Ok(());
        }
        return Err(format!("无法检查 Desktop 实例: {}", unsafe {
            GetLastError()
        }));
    }
    unsafe { CloseHandle(mutex) };

    let event_wide: Vec<u16> = std::ffi::OsStr::new(&quit_event_name(layout))
        .encode_wide()
        .chain(std::iter::once(0))
        .collect();
    let deadline = std::time::Instant::now() + timeout;
    let event = loop {
        let event = unsafe { OpenEventW(EVENT_MODIFY_STATE, 0, event_wide.as_ptr()) };
        if !event.is_null() {
            break event;
        }
        if unsafe { GetLastError() } != ERROR_FILE_NOT_FOUND {
            return Err(format!("无法打开 Desktop 退出信号: {}", unsafe {
                GetLastError()
            }));
        }
        let mutex = unsafe { OpenMutexW(SYNCHRONIZE, 0, mutex_wide.as_ptr()) };
        if mutex.is_null() && unsafe { GetLastError() } == ERROR_FILE_NOT_FOUND {
            return Ok(());
        }
        if !mutex.is_null() {
            unsafe { CloseHandle(mutex) };
        }
        if std::time::Instant::now() >= deadline {
            return Err("Desktop 正在运行，但退出信号不可用".into());
        }
        std::thread::sleep(std::time::Duration::from_millis(100));
    };
    let signaled = unsafe { SetEvent(event) } != 0;
    unsafe { CloseHandle(event) };
    if !signaled {
        return Err("无法发送 Desktop 退出信号".into());
    }

    loop {
        let mutex = unsafe { OpenMutexW(SYNCHRONIZE, 0, mutex_wide.as_ptr()) };
        if mutex.is_null() {
            if unsafe { GetLastError() } == ERROR_FILE_NOT_FOUND {
                return Ok(());
            }
            return Err(format!("等待 Desktop 退出失败: {}", unsafe {
                GetLastError()
            }));
        }
        unsafe { CloseHandle(mutex) };
        if std::time::Instant::now() >= deadline {
            return Err("等待 Desktop 正常退出超时；Node 未被停止".into());
        }
        std::thread::sleep(std::time::Duration::from_millis(100));
    }
}

#[cfg(not(windows))]
pub fn request_quit_and_wait(
    _layout: &Layout,
    _timeout: std::time::Duration,
) -> Result<(), String> {
    Err("当前平台未实现 Desktop 实例退出信号".into())
}

#[cfg(windows)]
pub fn request_show(layout: &Layout) -> Result<bool, String> {
    use std::os::windows::ffi::OsStrExt;
    use windows_sys::Win32::Foundation::{CloseHandle, GetLastError, ERROR_FILE_NOT_FOUND};
    use windows_sys::Win32::System::Threading::{
        OpenEventW, OpenMutexW, SetEvent, EVENT_MODIFY_STATE,
    };
    const SYNCHRONIZE: u32 = 0x0010_0000;
    let mutex_wide: Vec<u16> = std::ffi::OsStr::new(&mutex_name(layout))
        .encode_wide()
        .chain(std::iter::once(0))
        .collect();
    let mutex = unsafe { OpenMutexW(SYNCHRONIZE, 0, mutex_wide.as_ptr()) };
    if mutex.is_null() {
        return if unsafe { GetLastError() } == ERROR_FILE_NOT_FOUND {
            Ok(false)
        } else {
            Err(format!("检查 Desktop 实例失败: {}", unsafe {
                GetLastError()
            }))
        };
    }
    unsafe { CloseHandle(mutex) };
    let event_wide: Vec<u16> = std::ffi::OsStr::new(&show_event_name(layout))
        .encode_wide()
        .chain(std::iter::once(0))
        .collect();
    let event = unsafe { OpenEventW(EVENT_MODIFY_STATE, 0, event_wide.as_ptr()) };
    if event.is_null() {
        return Err(format!(
            "Desktop 正在运行，但唤醒信号不可用: {}",
            unsafe { GetLastError() }
        ));
    }
    let signaled = unsafe { SetEvent(event) } != 0;
    unsafe { CloseHandle(event) };
    if signaled {
        Ok(true)
    } else {
        Err("无法发送 Desktop 唤醒信号".into())
    }
}

#[cfg(not(windows))]
pub fn request_show(_layout: &Layout) -> Result<bool, String> {
    Err("当前平台未实现 Desktop 窗口唤醒信号".into())
}

#[cfg(windows)]
pub fn start_control_listener(app: tauri::AppHandle, layout: &Layout) {
    use std::os::windows::ffi::OsStrExt;
    use tauri::Manager;
    use windows_sys::Win32::Foundation::{CloseHandle, WAIT_OBJECT_0};
    use windows_sys::Win32::System::Threading::{OpenEventW, WaitForMultipleObjects};
    const SYNCHRONIZE: u32 = 0x0010_0000;
    let quit_name = quit_event_name(layout);
    let show_name = show_event_name(layout);
    std::thread::spawn(move || {
        let quit_wide: Vec<u16> = std::ffi::OsStr::new(&quit_name)
            .encode_wide()
            .chain(std::iter::once(0))
            .collect();
        let show_wide: Vec<u16> = std::ffi::OsStr::new(&show_name)
            .encode_wide()
            .chain(std::iter::once(0))
            .collect();
        let quit = unsafe { OpenEventW(SYNCHRONIZE, 0, quit_wide.as_ptr()) };
        let show = unsafe { OpenEventW(SYNCHRONIZE, 0, show_wide.as_ptr()) };
        if quit.is_null() || show.is_null() {
            if !quit.is_null() {
                unsafe { CloseHandle(quit) };
            }
            if !show.is_null() {
                unsafe { CloseHandle(show) };
            }
            return;
        }
        let handles = [show, quit];
        loop {
            let result = unsafe { WaitForMultipleObjects(2, handles.as_ptr(), 0, u32::MAX) };
            if result == WAIT_OBJECT_0 {
                if let Some(window) = app.get_webview_window("desktop_mvp") {
                    let _ = window.unminimize();
                    let _ = window.show();
                    let _ = window.set_focus();
                }
            } else if result == WAIT_OBJECT_0 + 1 {
                app.exit(0);
                break;
            } else {
                break;
            }
        }
        unsafe {
            CloseHandle(show);
            CloseHandle(quit);
        }
    });
}

#[cfg(not(windows))]
pub fn start_control_listener(_app: tauri::AppHandle, _layout: &Layout) {}

#[cfg(windows)]
fn mutex_name(layout: &Layout) -> String {
    format!(
        "Local\\DAgentsDesktop-{}",
        hash_path(&canonical_instance_home(layout))
    )
}

#[cfg(windows)]
fn quit_event_name(layout: &Layout) -> String {
    format!(
        "Local\\DAgentsDesktopQuit-{}",
        hash_path(&canonical_instance_home(layout))
    )
}

#[cfg(windows)]
fn show_event_name(layout: &Layout) -> String {
    format!(
        "Local\\DAgentsDesktopShow-{}",
        hash_path(&canonical_instance_home(layout))
    )
}

#[cfg(windows)]
fn canonical_instance_home(layout: &Layout) -> String {
    let path = layout
        .home
        .canonicalize()
        .unwrap_or_else(|_| layout.home.clone());
    path.to_string_lossy().replace('/', "\\").to_lowercase()
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

#[cfg(all(test, windows))]
mod instance_name_tests {
    use super::hash_path;

    fn normalize(path: &str) -> String {
        path.replace('/', "\\").to_lowercase()
    }

    #[test]
    fn windows_instance_key_ignores_case_and_separator_spelling() {
        let a = normalize(r"C:\Program Files\DAgents");
        let b = normalize("c:/program files/dagents");
        assert_eq!(a, b);
        assert_eq!(hash_path(&a), hash_path(&b));
    }
}

#[cfg(unix)]
unsafe fn libc_flock(fd: i32, op: i32) -> i32 {
    // Avoid libc crate dependency: declare flock.
    extern "C" {
        fn flock(fd: i32, operation: i32) -> i32;
    }
    flock(fd, op)
}

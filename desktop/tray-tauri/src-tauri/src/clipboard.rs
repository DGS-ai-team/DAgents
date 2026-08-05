//! Clipboard helpers for Web UI file attachment handoff.

#[cfg(windows)]
pub fn file_paths() -> Result<Vec<String>, String> {
    use windows_sys::Win32::System::DataExchange::{GetClipboardData, OpenClipboard};
    use windows_sys::Win32::UI::Shell::DragQueryFileW;

    const CF_HDROP: u32 = 15;
    unsafe {
        if OpenClipboard(std::ptr::null_mut()) == 0 {
            return Err("OpenClipboard failed".into());
        }
        let _guard = ClipboardGuard;
        let hdrop = GetClipboardData(CF_HDROP);
        if hdrop.is_null() {
            return Ok(Vec::new());
        }
        let count = DragQueryFileW(hdrop as _, u32::MAX, std::ptr::null_mut(), 0);
        if count == 0 {
            return Ok(Vec::new());
        }
        let mut out = Vec::with_capacity(count as usize);
        for i in 0..count {
            let n = DragQueryFileW(hdrop as _, i, std::ptr::null_mut(), 0);
            if n == 0 {
                continue;
            }
            let mut buf = vec![0_u16; n as usize + 1];
            let written = DragQueryFileW(hdrop as _, i, buf.as_mut_ptr(), n + 1);
            if written == 0 {
                continue;
            }
            out.push(String::from_utf16_lossy(&buf[..written as usize]));
        }
        Ok(out)
    }
}

#[cfg(windows)]
struct ClipboardGuard;

#[cfg(windows)]
impl Drop for ClipboardGuard {
    fn drop(&mut self) {
        unsafe {
            windows_sys::Win32::System::DataExchange::CloseClipboard();
        }
    }
}

#[cfg(not(windows))]
pub fn file_paths() -> Result<Vec<String>, String> {
    Ok(Vec::new())
}

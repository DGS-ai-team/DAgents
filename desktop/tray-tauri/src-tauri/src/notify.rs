//! Windows toast notifications plus cross-platform dedupe planning.

use crate::pending::Entry;
use std::collections::{HashMap, HashSet};
use std::sync::Mutex;

/// Must match the Tauri bundle identifier and the AppUserModelID on the Start Menu shortcut.
const APP_USER_MODEL_ID: &str = "com.dagents.shell";

#[derive(Debug, Clone)]
pub struct SyncPlan {
    pub to_push: Vec<Entry>,
    pub next_last: HashMap<String, Entry>,
}

pub fn plan_sync(
    last: &HashMap<String, Entry>,
    toast_entries: &[Entry],
    retain_ids: &HashSet<String>,
) -> SyncPlan {
    let mut next = HashMap::with_capacity(last.len() + toast_entries.len());
    let mut to_push = Vec::new();
    for e in toast_entries {
        let id = entry_key(e);
        if id.is_empty() {
            continue;
        }
        let prev = last.get(&id);
        if prev
            .map(|p| p.active() && p.hitl_items == e.hitl_items && p.has_unread == e.has_unread)
            .unwrap_or(false)
        {
            next.insert(id, e.clone());
            continue;
        }
        to_push.push(e.clone());
        next.insert(id, e.clone());
    }
    for id in retain_ids {
        let id = id.trim();
        if id.is_empty() || next.contains_key(id) {
            continue;
        }
        if let Some(prev) = last.get(id) {
            next.insert(id.to_string(), prev.clone());
        }
    }
    SyncPlan {
        to_push,
        next_last: next,
    }
}

pub struct Notifier {
    enabled: bool,
    last: Mutex<HashMap<String, Entry>>,
}

impl Notifier {
    pub fn new(enabled: bool) -> Self {
        Self {
            enabled,
            last: Mutex::new(HashMap::new()),
        }
    }

    pub fn sync(&self, entries: &[Entry], retain_ids: &HashSet<String>) {
        if !self.enabled {
            return;
        }
        let Ok(mut last) = self.last.lock() else {
            return;
        };
        let plan = plan_sync(&last, entries, retain_ids);
        for e in &plan.to_push {
            if let Err(err) = self.push_pending(e) {
                crate::diagnostics::stderr_best_effort(format_args!("shell toast: {err}"));
            }
        }
        *last = plan.next_last;
    }

    pub fn push_update_available(&self, latest_version: &str) -> Result<(), String> {
        if self.enabled {
            push_update_toast(latest_version)
        } else {
            Ok(())
        }
    }

    fn push_pending(&self, e: &Entry) -> Result<(), String> {
        push_pending_toast(&e.summary_label())
    }
}

fn entry_key(e: &Entry) -> String {
    let id = e.session_id.trim();
    if id.is_empty() {
        e.agent_id.trim().to_string()
    } else {
        id.to_string()
    }
}

/// Register the desktop app identity before Tauri creates its window or emits notifications.
/// Windows requires a Start Menu shortcut with this same AppUserModelID for desktop toasts.
#[cfg(windows)]
pub fn register_app_identity() -> Result<(), String> {
    windows_notification_identity::register()
}

#[cfg(not(windows))]
pub fn register_app_identity() -> Result<(), String> {
    Ok(())
}

#[cfg(windows)]
mod windows_notification_identity {
    use super::APP_USER_MODEL_ID;
    use std::env;
    use std::ffi::OsStr;
    use std::mem::{size_of, ManuallyDrop};
    use std::os::windows::ffi::OsStrExt;
    use std::path::{Path, PathBuf};
    use windows::core::{Interface, GUID, PCWSTR, PWSTR};
    use windows::Win32::Foundation::PROPERTYKEY;
    use windows::Win32::System::Com::StructuredStorage::{
        PropVariantClear, PROPVARIANT, PROPVARIANT_0, PROPVARIANT_0_0, PROPVARIANT_0_0_0,
    };
    use windows::Win32::System::Com::{
        CoCreateInstance, CoInitializeEx, CoTaskMemAlloc, CoUninitialize, IPersistFile,
        CLSCTX_INPROC_SERVER, COINIT_APARTMENTTHREADED, STGM_READWRITE,
    };
    use windows::Win32::System::Variant::VT_LPWSTR;
    use windows::Win32::UI::Shell::PropertiesSystem::IPropertyStore;
    use windows::Win32::UI::Shell::{IShellLinkW, SetCurrentProcessExplicitAppUserModelID};

    const CLSID_SHELL_LINK: GUID = GUID::from_u128(0x00021401_0000_0000_c000_000000000046);
    const PKEY_APP_USER_MODEL_ID: PROPERTYKEY = PROPERTYKEY {
        fmtid: GUID::from_u128(0x9f4c2855_9f79_4b39_a8d0_e1d42de1d5f3),
        pid: 5,
    };
    const RPC_E_CHANGED_MODE: i32 = 0x8001_0106u32 as i32;

    struct ComApartment(bool);

    impl Drop for ComApartment {
        fn drop(&mut self) {
            if self.0 {
                unsafe { CoUninitialize() };
            }
        }
    }

    fn initialize_com() -> Result<ComApartment, String> {
        let hr = unsafe { CoInitializeEx(None, COINIT_APARTMENTTHREADED) };
        if hr.0 < 0 && hr.0 != RPC_E_CHANGED_MODE {
            return Err(format!(
                "无法初始化 Windows 通知组件: HRESULT 0x{:08X}",
                hr.0
            ));
        }
        Ok(ComApartment(hr.0 >= 0))
    }

    struct OwnedPropVariant(PROPVARIANT);

    impl OwnedPropVariant {
        fn from_str(value: &str) -> Result<Self, String> {
            let value = wide(OsStr::new(value));
            let allocation = unsafe { CoTaskMemAlloc(value.len() * size_of::<u16>()) };
            if allocation.is_null() {
                return Err("无法分配 Windows 通知身份属性".to_string());
            }
            unsafe {
                std::ptr::copy_nonoverlapping(
                    value.as_ptr(),
                    allocation.cast::<u16>(),
                    value.len(),
                );
            }
            let mut result = PROPVARIANT::default();
            result.Anonymous = PROPVARIANT_0 {
                Anonymous: ManuallyDrop::new(PROPVARIANT_0_0 {
                    vt: VT_LPWSTR,
                    wReserved1: 0,
                    wReserved2: 0,
                    wReserved3: 0,
                    Anonymous: PROPVARIANT_0_0_0 {
                        pwszVal: PWSTR(allocation.cast()),
                    },
                }),
            };
            Ok(Self(result))
        }
    }

    impl Drop for OwnedPropVariant {
        fn drop(&mut self) {
            let _ = unsafe { PropVariantClear(&mut self.0) };
        }
    }

    pub(super) fn register() -> Result<(), String> {
        let _apartment = initialize_com()?;

        let app_id = wide(OsStr::new(APP_USER_MODEL_ID));
        unsafe {
            SetCurrentProcessExplicitAppUserModelID(PCWSTR(app_id.as_ptr()))
                .map_err(|error| format!("无法设置 DAgents 通知身份: {error}"))?;
        }

        if cfg!(debug_assertions) {
            // Development builds may run beside a packaged install. Do not rewrite the
            // user's Start Menu shortcut to point at a temporary debug executable.
            return Ok(());
        }

        // Tauri's NSIS bundle creates this per-user Start Menu shortcut. Repair it on
        // startup as well, so existing installations and portable release builds can
        // emit notifications under the DAgents identity.
        let app_data = env::var_os("APPDATA")
            .ok_or_else(|| "找不到当前用户的应用数据目录，无法注册 Windows 通知".to_string())?;
        let programs = PathBuf::from(app_data)
            .join("Microsoft")
            .join("Windows")
            .join("Start Menu")
            .join("Programs");
        let shortcut = programs.join("DAgents Desktop.lnk");
        std::fs::create_dir_all(&programs)
            .map_err(|error| format!("无法准备 DAgents 开始菜单快捷方式: {error}"))?;

        let executable = env::current_exe()
            .map_err(|error| format!("无法定位 DAgents Desktop 程序: {error}"))?;
        write_shortcut_identity(&shortcut, &executable)
    }

    fn write_shortcut_identity(shortcut: &Path, executable: &Path) -> Result<(), String> {
        let executable_wide = wide(executable.as_os_str());
        let working_dir = executable
            .parent()
            .ok_or_else(|| "无法定位 DAgents Desktop 工作目录".to_string())?;
        let working_dir_wide = wide(working_dir.as_os_str());
        let shortcut_wide = wide(shortcut.as_os_str());

        unsafe {
            let link: IShellLinkW = CoCreateInstance(&CLSID_SHELL_LINK, None, CLSCTX_INPROC_SERVER)
                .map_err(|error| format!("无法创建 DAgents 开始菜单快捷方式: {error}"))?;
            let persist: IPersistFile = link
                .cast()
                .map_err(|error| format!("无法打开 DAgents 快捷方式存储: {error}"))?;
            if shortcut.exists() {
                persist
                    .Load(PCWSTR(shortcut_wide.as_ptr()), STGM_READWRITE)
                    .map_err(|error| format!("无法加载 DAgents 开始菜单快捷方式: {error}"))?;
            }

            link.SetPath(PCWSTR(executable_wide.as_ptr()))
                .map_err(|error| format!("无法更新 DAgents 快捷方式目标: {error}"))?;
            link.SetWorkingDirectory(PCWSTR(working_dir_wide.as_ptr()))
                .map_err(|error| format!("无法更新 DAgents 快捷方式目录: {error}"))?;
            link.SetIconLocation(PCWSTR(executable_wide.as_ptr()), 0)
                .map_err(|error| format!("无法更新 DAgents 快捷方式图标: {error}"))?;

            let properties: IPropertyStore = link
                .cast()
                .map_err(|error| format!("无法设置 DAgents 通知身份: {error}"))?;
            let app_id_property = OwnedPropVariant::from_str(APP_USER_MODEL_ID)?;
            properties
                .SetValue(&PKEY_APP_USER_MODEL_ID, &app_id_property.0)
                .map_err(|error| format!("无法设置 DAgents 通知身份: {error}"))?;
            properties
                .Commit()
                .map_err(|error| format!("无法保存 DAgents 通知身份: {error}"))?;
            persist
                .Save(PCWSTR(shortcut_wide.as_ptr()), true)
                .map_err(|error| format!("无法保存 DAgents 开始菜单快捷方式: {error}"))?;
        }

        Ok(())
    }

    #[cfg(test)]
    #[test]
    fn shortcut_identity_is_written_and_read_back() {
        use std::time::{SystemTime, UNIX_EPOCH};

        let _apartment = initialize_com().unwrap();
        let suffix = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_nanos();
        let shortcut =
            env::temp_dir().join(format!("dagents-toast-{}-{suffix}.lnk", std::process::id()));
        let executable = env::current_exe().unwrap();
        write_shortcut_identity(&shortcut, &executable).unwrap();
        let app_id = read_shortcut_identity(&shortcut).unwrap();
        let _ = std::fs::remove_file(shortcut);

        assert_eq!(app_id, APP_USER_MODEL_ID);
    }

    #[cfg(test)]
    fn read_shortcut_identity(shortcut: &Path) -> Result<String, String> {
        let shortcut_wide = wide(shortcut.as_os_str());
        unsafe {
            let link: IShellLinkW = CoCreateInstance(&CLSID_SHELL_LINK, None, CLSCTX_INPROC_SERVER)
                .map_err(|error| format!("无法打开测试快捷方式: {error}"))?;
            let persist: IPersistFile = link
                .cast()
                .map_err(|error| format!("无法读取测试快捷方式: {error}"))?;
            persist
                .Load(PCWSTR(shortcut_wide.as_ptr()), STGM_READWRITE)
                .map_err(|error| format!("无法加载测试快捷方式: {error}"))?;
            let properties: IPropertyStore = link
                .cast()
                .map_err(|error| format!("无法读取测试快捷方式属性: {error}"))?;
            let value = OwnedPropVariant(
                properties
                    .GetValue(&PKEY_APP_USER_MODEL_ID)
                    .map_err(|error| format!("无法读取通知身份属性: {error}"))?,
            );
            let value = &*value.0.Anonymous.Anonymous;
            if value.vt != VT_LPWSTR {
                return Err("测试快捷方式中的通知身份类型不正确".to_string());
            }
            let pointer = value.Anonymous.pwszVal.0;
            if pointer.is_null() {
                return Err("测试快捷方式中的通知身份为空".to_string());
            }
            let mut length = 0;
            while *pointer.add(length) != 0 {
                length += 1;
            }
            String::from_utf16(std::slice::from_raw_parts(pointer, length))
                .map_err(|error| format!("测试快捷方式中的通知身份不是有效文本: {error}"))
        }
    }

    fn wide(value: &OsStr) -> Vec<u16> {
        value.encode_wide().chain(std::iter::once(0)).collect()
    }
}

#[cfg(windows)]
fn push_pending_toast(message: &str) -> Result<(), String> {
    use winrt_notification::{Duration, Toast};
    Toast::new(APP_USER_MODEL_ID)
        .title("DAgents 待处理")
        .text1(message)
        .text2("请打开 DAgents Desktop 查看")
        .duration(Duration::Short)
        .show()
        .map_err(|e| e.to_string())
}

#[cfg(not(windows))]
fn push_pending_toast(_message: &str) -> Result<(), String> {
    Ok(())
}

#[cfg(windows)]
fn push_update_toast(latest_version: &str) -> Result<(), String> {
    use winrt_notification::{Duration, Toast};
    let latest = if latest_version.trim().is_empty() {
        "新版本"
    } else {
        latest_version.trim()
    };
    Toast::new(APP_USER_MODEL_ID)
        .title("DAgents 新版本可用")
        .text1(&format!("可升级到 {latest}，点击查看详情"))
        .text2("请打开 DAgents Desktop 查看")
        .duration(Duration::Short)
        .show()
        .map_err(|e| e.to_string())
}

#[cfg(not(windows))]
fn push_update_toast(_latest_version: &str) -> Result<(), String> {
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::SystemTime;

    fn entry(id: &str, hitl: i32, unread: bool) -> Entry {
        Entry {
            agent_id: id.into(),
            session_id: id.into(),
            display_name: id.into(),
            hitl_items: hitl,
            has_unread: unread,
            event_type: String::new(),
            updated_at: SystemTime::now(),
        }
    }

    #[test]
    fn plan_sync_dedupes_unchanged_entries() {
        let mut last = HashMap::new();
        last.insert("a1".into(), entry("a1", 1, false));
        let plan = plan_sync(
            &last,
            &[entry("a1", 1, false), entry("a2", 0, true)],
            &HashSet::new(),
        );
        assert_eq!(plan.to_push.len(), 1);
        assert!(plan.next_last.contains_key("a1"));
        assert!(plan.next_last.contains_key("a2"));
    }

    #[test]
    fn plan_sync_retains_focused_ids() {
        let mut last = HashMap::new();
        last.insert("a1".into(), entry("a1", 1, false));
        let retain = HashSet::from(["a1".to_string()]);
        let plan = plan_sync(&last, &[], &retain);
        assert!(plan.next_last.contains_key("a1"));
        assert!(plan.to_push.is_empty());
    }

    #[test]
    fn notification_identity_matches_the_tauri_bundle_identifier() {
        let config: serde_json::Value =
            serde_json::from_str(include_str!("../tauri.conf.json")).unwrap();
        assert_eq!(APP_USER_MODEL_ID, config["identifier"].as_str().unwrap());
    }
}

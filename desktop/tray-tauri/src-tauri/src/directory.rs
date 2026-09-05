//! Native directory picker used by the Node Web UI.

/// Open a native folder picker and return the selected absolute path.
/// `None` means the user cancelled the dialog.
pub fn pick_directory() -> Result<Option<String>, String> {
    Ok(rfd::FileDialog::new()
        .set_title("选择 Agent 工作目录")
        .pick_folder()
        .map(|path| path.to_string_lossy().into_owned()))
}

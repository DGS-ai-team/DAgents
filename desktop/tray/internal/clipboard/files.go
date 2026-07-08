package clipboard

// FilePaths 返回剪贴板中 CF_HDROP 文件路径；无数据时返回 nil slice。
func FilePaths() ([]string, error) {
	return filePaths()
}

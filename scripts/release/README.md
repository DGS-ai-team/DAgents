# Release metadata scripts

从 `dev` 准备正式版本时，使用脚本统一更新版本常量、CHANGELOG、README、手册和路线图：

```bash
python scripts/release/prepare_release.py 0.10.5 \
  --summary $'- Improve release validation\n- Add release artifact checksums'
python scripts/release/validate_release.py --version 0.10.5
```

`validate_release.py` 不依赖项目运行时依赖，可在 release PR 的 CI 预检和 tag 发版前重复执行。脚本只更新明确的版本标记，遇到格式不符合预期会失败，不会静默覆盖文件。

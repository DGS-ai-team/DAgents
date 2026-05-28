# 操作系统与运行环境兼容清单

本文说明 **CPython 3.11** 在官方文档中的支持边界，以及 **DAgents** 以「源码运行」与「PyInstaller 单文件包」交付时的**实际**兼容差异。结论先行：

- **官方对 Linux 并未给出「最低 RHEL/Ubuntu 版本号」**；更可靠的是看 **glibc 版本** 与 **是否能在该环境从源码成功构建 CPython**。
- **Windows**：CPython 3.11 对**传统安装包**写明支持 **Windows 8.1 及更新版本**；Microsoft Store 版要求 **Windows 10+**（见下文引用）。
- **极旧 Linux（例如 RHEL 6，glibc 2.12）**：不适合作为 **3.11+ 预编译单文件包**的目标；若业务必须留在该环境，应优先考虑 **容器 / 较新宿主上的进程** 或 **更低版本 Python + 自建依赖**（本仓库当前主线以 **3.11+** 声明、CI 以 **3.13** 验证，见根目录 `README.md`）。

---

## 1. CPython 3.11：官方怎么说

### 1.1 Windows

[Using Python on Windows（3.11 文档）](https://docs.python.org/3.11/using/windows.html) 写明：

> As specified in [PEP 11](https://peps.python.org/pep-0011/), a Python release only supports a Windows platform while Microsoft considers the platform under extended support. **This means that Python 3.11 supports Windows 8.1 and newer.** If you require Windows 7 support, please install Python 3.8.

同页对 **Microsoft Store** 包补充：需要 **Windows 10 及以上**。

因此：

| 安装来源 | 最低 Windows（官方表述） |
|----------|---------------------------|
| python.org 传统安装包 / 嵌入包等 | **Windows 8.1+** |
| Microsoft Store 的 Python 3.11 | **Windows 10+** |

### 1.2 Linux / 其它 Unix

[PEP 11 – CPython platform support](https://peps.python.org/pep-0011/) 将 **`x86_64-unknown-linux-gnu`（glibc, gcc）** 等列为 **Tier 1**，但 **PEP 并未规定「最低 glibc 小版本」或「最低 RHEL 大版本」**。

[Using Python on Unix platforms（3.11）](https://docs.python.org/3.11/using/unix.html) 主要描述获取源码与编译流程，同样**不**给出「最低 Ubuntu 版本」一类硬指标。

**POSIX 与区域设置**（与能否正常跑 Unicode 相关）：自 3.7 起，官方期望系统至少提供 **`C.UTF-8` / `C.utf8` / `UTF-8`** 之一作为非传统 `C` locale 的替代；详见 PEP 11 的 *Legacy C Locale* 小节。

### 1.3 macOS

PEP 11 中 **Tier 2** 含 **`x86_64-apple-darwin`**，**Tier 1** 含 **`aarch64-apple-darwin`**。具体 **macOS 大版本下限**以 **python.org 对应安装包发布说明**为准（会随 Apple 支持策略与构建链调整）。

---

## 2. 「最低支持什么操作系统」该怎么理解

对 **Linux 服务器**：

1. **从源码安装 CPython 3.11**（或发行版自带/第三方提供的 3.11）：能否成功取决于 **内核 / libc / OpenSSL / 编译器** 等，官方不打包成「支持到 RHEL x」的一句话。
2. **运行别人编好的二进制**（含 **PyInstaller onefile**、wheel 里的 `.so`）：限制主要来自 **构建该二进制时的 glibc（及动态链接库）版本**，常见错误是 **`GLIBC_x.xx not found`**——这与「Python 3.11 官方 Tier 1」是两套问题。

因此下表按 **交付形态** 区分 **Windows** 与 **Linux** 的「最低环境」描述方式。

---

## 3. DAgents：交付形态 × 兼容关注点

| 交付形态 | Windows | Linux | macOS |
|----------|---------|-------|--------|
| **源码 + pip**（`requirements.txt`） | 受限于你在该机上安装的 **Python 小版本**；若使用 **3.11**，可参考 §1.1 的 **Win 8.1+**（传统安装包） | 以 **发行版是否提供 / 能否编译 3.11+** 为准；DAgents 声明 **3.11+**，CI 验证 **3.13** | 以本机 Python 与依赖 wheel 为准 |
| **PyInstaller 单文件包**（本仓库 CI，见 `.github/workflows/build-and-release.yml`） | 当前矩阵为 **Windows 2022** 上构建的 **x64/x86**；实际最低 **Windows** 版本还受 **VC 运行库** 等约束，需以 **在目标机实测** 为准 | **由 CI 选用的 Linux 镜像 glibc 决定**。当前 Linux x64 默认在 **Ubuntu 20.04（focal）** 容器内用 pyenv 编 **3.13** 再打 PyInstaller，产物往往依赖 **较新的 glibc 符号**（例如与 **GLIBC_2.30** 相关的问题），**无法在 glibc 更旧的主机（如部分 RHEL 7/8 或更老环境）上保证可运行** | 若 CI 未产出 darwin 单文件包，则本节不适用 |

**不要求必须用 Python 3.13**：若目标环境 glibc 偏旧，可采取：

- **在同一类旧 glibc 环境**（例如 **glibc 2.28** 的 **RHEL 8 / Rocky Linux 8**）用本仓库 `scripts/ci/build_linux_rocky8_pyenv.sh` 自行打 PyInstaller（或自建 Docker 镜像与 workflow）；或  
- **固定使用 Python 3.11** 并在该环境 **源码安装 + venv**，使解释器与依赖均与宿主 glibc 一致（DAgents 代码需在 3.11 下回归验证）。

---

## 4. 常见 Linux 发行版与 glibc 对照（便于估算）

以下为**典型**主版本自带的 glibc 主版本（实际以 `ldd --version` 为准；企业版可能有 backport）：

| 环境（示例） | 典型 glibc（量级） | 与「现代 PyInstaller（focal/较新链）」 |
|--------------|-------------------|----------------------------------------|
| RHEL 6 / CentOS 6 | ~**2.12** | **极难**承载官方 **3.11+** 预编译解释器/onefile；不建议作为目标 |
| RHEL 7 / CentOS 7 | ~**2.17** | 与 **manylinux2014** 量级接近；**是否**能跑 **3.11 官方二进制**仍取决于具体构建；**focal 打的 onefile** 通常 **不兼容** |
| RHEL 8 / Rocky 8 / Alma 8 | ~**2.28** | 若 onefile 在 **glibc 2.28** 环境构建，通常 **可覆盖** 该类宿主；低于 2.28 仍可能失败 |
| Ubuntu 20.04（focal） | ~**2.31** | 与本仓库默认 Linux x64 **构建环境**同量级 |

---

## 5. 建议排障顺序（Linux 上出现 `GLIBC_x.xx not found`）

1. 在目标机执行 **`ldd --version`**，确认 **glibc** 版本。  
2. 若运行 **PyInstaller 包**：换用 **在 ≤ 目标机 glibc 的环境** 上重打的包，或改用 **源码 + venv**。  
3. 若必须 **RHEL 6 类宿主**：视为 **不支持 CPython 3.11+ 单文件包** 的场景，改 **架构**（容器 / 跳板机）或 **降级 Python 大版本**（需整条依赖链支持，且非本仓库默认承诺）。

---

## 6. 参考链接

- [PEP 11 – CPython platform support](https://peps.python.org/pep-0011/)  
- [Using Python on Windows（3.11）](https://docs.python.org/3.11/using/windows.html)  
- [Using Python on Unix platforms（3.11）](https://docs.python.org/3.11/using/unix.html)  
- 本仓库：**[README.md](../README.md)**（Python 版本声明）、**[`.github/workflows/build-and-release.yml`](../.github/workflows/build-and-release.yml)**（PyInstaller 矩阵）、**[`scripts/ci/README.md`](../scripts/ci/README.md)**（Linux 容器构建脚本说明）

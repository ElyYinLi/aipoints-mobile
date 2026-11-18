# 沙箱里用 GitHub：只走 REST API

> 本文档记录在**受限沙箱环境**里操作 GitHub 的实测结论。普通本机开发请直接用 `git`，不用看这篇。
>
> 背景：沙箱的网络策略会拦掉 `github.com` 的 git 协议端点，但放行 REST API。这不是「没登录」的问题，带 token 也一样。

## 一、通道可用性实测

| 通道 | 状态 | 说明 |
|---|---|---|
| `https://api.github.com`（REST） | ✅ 通 | 建仓库、读写文件、提交 commit、建 Release、传资产，全走它 |
| `https://uploads.github.com` | ✅ 通 | Release 资产专用域名 |
| `https://raw.githubusercontent.com` | ✅ 通 | 取单个文件快，适合读几个源文件参考 |
| `https://codeload.github.com` | ⚠️ 极慢 | 30 MB 仓库超过 170 秒仍收不完，超时 |
| `git clone` / `git push` / `ls-remote` | ❌ 不通 | 带 token 也静默超时；`info/refs` 端点本身可达（返回 401），但完整传输会中断 |
| `gh auth login` | ❌ 不可用 | 交互式授权流程在沙箱里走不通 |

**结论：沙箱里不要用 git 协议，全部走 REST API。** 别在 `git push` 上浪费时间，那不是配置问题。

## 二、凭据

沙箱里通常没有 `GH_TOKEN`，`~/.config/gh/hosts.yml` 是空对象。

```bash
# 只放环境变量或 600 权限临时文件，用完立即删
printf '%s' "github_pat_xxx" > /tmp/tok.txt && chmod 600 /tmp/tok.txt
export GH_TOKEN=$(tr -d '\n' < /tmp/tok.txt) && rm -f /tmp/tok.txt
```

三条纪律：

1. **令牌不进任何文件、不进 git**。发版前跑 `python3 tools/check_secrets.py .`，它是代码级硬闸，不靠记性。
2. 会话被压缩后上下文里的令牌可能被截断成 `github_pat_xxx…`，直接请求会 401。要**从原始来源复制完整串**，别手动输入。
3. 权限最小化：`Contents` + `Workflows` + `Actions` 三项读写足够，仓库范围选 `Only select repositories`。

## 三、判断本地与远端差多少：比 blob sha，不要比 git status

这是最容易踩的坑：`git status` 显示一堆 modified，**不代表远端没有这些内容**，只代表本地工作区和本地 HEAD 不一致。

正确做法是比 **git 对象哈希**：

```python
# 远端每个文件的 blob sha
tree = call("GET", f"/repos/{repo}/git/trees/{branch}?recursive=1")
remote = {i["path"]: i["sha"] for i in tree["tree"] if i["type"] == "blob"}

# 本地文件算同一个哈希（git 内部算法）
def blob_sha(data: bytes) -> str:
    h = hashlib.sha1()
    h.update(b"blob %d\0" % len(data))
    h.update(data)
    return h.hexdigest()
```

一次调用比对全部文件，几秒钟出结果，且不受本地提交历史干扰。本仓库已封装为 `tools/gh_sync.py --dry-run`。

## 四、提交文件

### 反例：`PUT /contents/{path}` 逐个文件提交

能跑，但坑多：

| 坑 | 后果 | 规避 |
|---|---|---|
| 不带 `sha` | 409 Conflict | 每次提交前先 GET 拿最新 sha |
| 连续改同一个文件 | 409 | 每提交一个就重新 GET |
| 一次改多个文件 | 也会 409 | 串行 + `time.sleep(1)`，或用上个返回的 commit sha 当 base |
| `content` 带换行 | 422 | `b64encode` 后不要 wrap |

### 正解：Git Data API 一次提交整个 tree

`tools/gh_sync.py` 用的就是这套，**只产生一个 commit，不会 409**：

```python
# 1. 每个变化的文件建 blob
blob = POST /repos/{repo}/git/blobs   {"content": b64, "encoding": "base64"}
# 2. 带上 base_tree 建新 tree（未列出的文件自动继承）
tree = POST /repos/{repo}/git/trees  {"base_tree": base, "tree": [{path, mode, type, sha}]}
# 3. 建 commit
commit = POST /repos/{repo}/git/commits  {"message": ..., "tree": tree.sha, "parents": [base]}
# 4. 移动分支指针
PATCH /repos/{repo}/git/refs/heads/main  {"sha": commit.sha}
```

用法：

```bash
export GH_TOKEN=github_pat_xxx
python3 tools/gh_sync.py --repo OWNER/NAME --dry-run   # 先看差异
python3 tools/gh_sync.py --repo OWNER/NAME             # 真推
```

## 五、Release 与资产

```bash
# 查最新
curl -sS -H "Authorization: Bearer $T" \
  "https://api.github.com/repos/OWNER/REPO/releases?per_page=5"

# 传资产：注意域名是 uploads.github.com，name 要 URL 编码
curl -sS -X POST -H "Authorization: Bearer $T" \
     -H "Content-Type: application/octet-stream" \
     --data-binary @"dist/xxx.zip" \
     "https://uploads.github.com/repos/OWNER/REPO/releases/$RID/assets?name=xxx.zip"
```

两个坑：

- **下载 CI artifact 时要去掉 `Authorization` 头**。GitHub 会 302 到对象存储，把鉴权头带过去会 401。
- Release 附件名**不能含中文**，会被规范化成 `default.bat` 之类。中文补丁必须先打成 zip。

## 六、密钥防泄漏：靠代码拦，不靠记性

任何「会上传」的脚本，放行前必须先扫内容。本仓库的闸门是 `tools/check_secrets.py`：

- 扫真实形态的令牌（`github_pat_*`、`ghp_*`、私钥头、AWS key），**不误报文档里的占位符**；
- 扫密钥类文件后缀（`.jks`/`.keystore`/`.p12`/`.mobileprovision`/`.pem`/`.key`）；
- 支持直接扫 `.zip`/`.apk`/`.ipa`/`.aar` 内部文件名与文本内容。

把 `python3 tools/check_secrets.py .` 挂进 CI 的 lint 步骤，含密钥的提交就进不了主干。

## 七、沙箱环境本身的其他坑

| 现象 | 真相 |
|---|---|
| 工作区被重置 | 上一轮的 `/workspace` 产物可能全没，长期产物必须**外部归档**（Release / 项目网盘） |
| `git clone` 一直跑 | 会被自动转后台，5 分钟还在跑。别等，换 raw / codeload |
| 大文件下载超时 | `--max-time 170` + 30 MB 不一定够；用 `curl -w "%{http_code} %{size_download}"` 核对实际字节数 |
| Android SDK 位置 | 不一定在 `~/Android/Sdk`，可能在 `/opt/android-sdk`。**先 `ls` 再写脚本** |
| JDK 版本 | 可能只有 20；`javac -source 11 -target 11` 可用但会提示 deprecated，加 `-nowarn` 或 `-Xlint:-options` 静音 |
| `javac -bootclasspath` | JDK 9+ 下与 `-target 11` 冲突（`option --boot-class-path not allowed`）。把 `android.jar` 放 `-classpath` 最前面即可 |
| `gomobile bind` | module 模式下强制依赖 `golang.org/x/mobile`（要求 go≥1.26），会抬高引擎 go 指令。改走 GOPATH 模式隔离，详见 `docs/gomobile-notes.md` |

## 八、一句话

**沙箱里把 REST API 当 git 用：blob sha 比内容，Git Data API 提交，`uploads.github.com` 传包。git 那套在这里不通，不要试图修它。**

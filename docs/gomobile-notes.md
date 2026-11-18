# gomobile bind 的依赖升级说明

## 问题

`gomobile bind` 在 Go 1.24+ 的 module 模式下要求 `golang.org/x/mobile` 出现在
**当前 module 的依赖图**里，否则报：

```
gomobile bind requires golang.org/x/mobile in the current module,
but it is not in the module dependency graph.
```

而 `golang.org/x/mobile@latest` 现在要求 `go >= 1.26.0`。把这条依赖写进
`core/go.mod` 会把整个引擎的 `go` 指令抬到 1.26，导致：

- 只装了较早 Go 的机器（含某些 Linux 发行版仓库版）无法编译引擎；
- CI 必须先下 1.26 toolchain 才能 `go build`，多一层失败点。

## 结论：**不要把 x/mobile 写进 core/go.mod**

做法是让 `gomobile bind` 在 **GOPATH 模式**下工作：把 `core` 软链到
`$GOPATH/src/github.com/ElyYinLi/aipoints-mobile/core`，用 `GO111MODULE=off`
调用 gomobile。这样 x/mobile 不会污染引擎的依赖图，引擎保持 `go 1.21`。

构建脚本 `android/build-apk.sh` 与 `.github/workflows/build-ios.yml` 已按此处理。

## 如果要模块模式（不推荐）

```bash
cd core
go get golang.org/x/mobile/bind@latest   # 会把 go 指令抬到 1.26
go mod tidy
gomobile bind -target=android ./mobile
```

代价：`go.mod` 里多出 4 个 indirect 依赖，且 `go` 版本被锁死在 1.26+。
只有在确实需要 vendoring / 私有 proxy 时才值得。

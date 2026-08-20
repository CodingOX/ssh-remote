# 安装 ssh-remote

仅在 `command -v ssh-remote` 无输出时读取本文件。先确认系统 OpenSSH 客户端存在：

```bash
command -v ssh
command -v scp
```

缺失时请用户按其操作系统安装 OpenSSH。不要猜测 Release URL、资产名或校验和，也不要使用 `curl | sh`。

## 优先：正式 Release 二进制

1. 在 `https://github.com/CodingOX/ssh-remote/releases` 确认用户指定的 Release 存在。
2. 从该 Release 页面选择与平台匹配的实际资产：
   - macOS Apple Silicon：`ssh-remote_<version>_darwin_arm64.tar.gz`
   - macOS Intel：`ssh-remote_<version>_darwin_amd64.tar.gz`
   - Linux x86_64：`ssh-remote_<version>_linux_amd64.tar.gz`
   - Linux ARM64：`ssh-remote_<version>_linux_arm64.tar.gz`
3. 同时下载该 Release 附带的 `SHA256SUMS`，仅校验实际选择的归档：macOS 使用 `shasum -a 256 -c -`，Linux 使用 `sha256sum -c -`。
4. 解压后将 `ssh-remote` 放入用户 PATH，执行 `ssh-remote version` 验证。

若没有可验证的 Release，使用 Go 安装。

## 备选：Go 安装

需要 Go 1.22 或更高版本：

```bash
go install github.com/CodingOX/ssh-remote/cmd/ssh-remote@latest
```

安装后仍找不到命令时，将 `$(go env GOBIN)`（非空时）或 `$(go env GOPATH)/bin` 加入 shell 的 `PATH`，重开终端后验证：

```bash
ssh-remote version
```

## 固定版本构建

需要固定版本时，只从用户已核验的 tag 或 commit 构建：

```bash
git clone https://github.com/CodingOX/ssh-remote.git
cd ssh-remote
git checkout <verified-tag-or-commit>
go build -o bin/ssh-remote ./cmd/ssh-remote
./bin/ssh-remote version
```

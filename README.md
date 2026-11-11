# Codex Snapshot

把 `/home/agent/.codex`、`/home/agent/workspace` 和（如果存在）`/home/agent/.bash_history` 归档、压缩并加密，生成 `/home/agent/codex-snapshot-<UTC时间>-<随机值>.tar.gz.enc`。默认排除 `.codex/tmp` 和所有名为 `node_modules` 的目录。程序是独立的 Linux 可执行文件，创建快照时不需要调用 `tar` 或 `openssl`。

## 编译与运行

本目录下运行：

```sh
mkdir -p bin
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/codex-snapshot-linux-amd64 .
```

把 `bin/codex-snapshot-linux-amd64` 上传到 Debian 13 x86_64 的 `/home/agent`，再以能读取 `.codex`、`workspace` 和历史文件的账户运行：

```sh
chmod 700 /home/agent/codex-snapshot-linux-amd64
/home/agent/codex-snapshot-linux-amd64
```

程序不接受参数。成功后输出 `FILE=`、`BYTES=`、与 `sha256sum` 兼容的校验行，以及 `PASSWORD=`。**请立即把密码和 SHA-256 值分别保存在快照文件以外的安全位置**；密码只在这次运行的标准输出中出现。如果标准输出被日志系统记录，密码也会进入日志。

输出文件权限为 `0600`。如果 `.codex` 或 `workspace` 不存在、读取文件失败，程序退出并删除未完成的临时文件。缺失的 `.bash_history` 会跳过；socket、FIFO 等特殊文件会跳过并在标准错误输出说明。符号链接会作为链接归档，不会跟随链接去读取外部路径。

## 解密和查看

把输出中的 `PASSWORD=` 后面的 64 位十六进制文本单独写入权限为 `0600` 的密码文件，例如 `snapshot-password.txt`，然后运行：

```sh
sha256sum /path/to/codex-snapshot-....tar.gz.enc
set -o pipefail
openssl enc -d -aes-256-cbc -salt -pbkdf2 -iter 600000 -md sha256 \
  -pass file:snapshot-password.txt \
  -in /path/to/codex-snapshot-....tar.gz.enc | tar -tzf -
```

确认校验值和归档内容后，要恢复时把最后的 `tar -tzf -` 换成 `tar -xzf - -C /your/restore/directory`。请只在你信任的机器和空目录中恢复。

此格式与原脚本的 OpenSSL AES-256-CBC、PBKDF2-HMAC-SHA256、600000 次迭代和 8 字节随机盐兼容。[OpenSSL `enc` 参数说明](https://docs.openssl.org/3.6/man1/openssl-enc/)。AES-CBC 本身不提供防篡改认证；保存并核对独立的 SHA-256 值可用于发现文件变化。

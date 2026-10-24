# SingBox Panel

集中管理多台服务器上官方 sing-box 的 Web 面板。可在其中管理节点、入站、出站、路由、用户与订阅，并远程执行 sing-box 的安装、更新、启停、配置下发和卸载。

## 安装

需要 Linux x86_64 或 arm64、systemd、已解析到面板服务器的域名，以及支持 WebSocket 的 HTTPS 反向代理。

```bash
curl -fsSL https://github.com/hann0w0/SingBox-Panel/releases/latest/download/install.sh | sudo bash
```

安装脚本支持非交互安装（`--base-url`、`--admin`、`--password`、`--non-interactive`），用 `--version vX.Y.Z` 可指定版本。安装完成后在面板中创建服务器，并在目标 VPS 上执行该节点详情页生成的 Agent 安装命令。

## 卸载

```bash
# 保留配置与数据库
curl -fsSL https://github.com/hann0w0/SingBox-Panel/releases/latest/download/install.sh | sudo bash -s -- --uninstall --yes

# 连同配置与数据库一并删除
curl -fsSL https://github.com/hann0w0/SingBox-Panel/releases/latest/download/install.sh | sudo bash -s -- --uninstall --purge --yes
```

节点 Agent 与 sing-box 在对应服务器详情页单独卸载，互不影响。

## 工作模式

面板通过 HTTPS 提供管理界面，节点上的 Agent 主动以 WSS 连接面板，节点无需开放额外管理端口，也不提供任意 Shell 执行能力。Agent 以官方方式安装 sing-box，下发配置前执行 `sing-box check`，程序与配置更新采用校验、原子替换与失败回滚。

面板支持两种配置模式：管理模式根据面板中的节点、协议与路由设置生成配置；原始配置模式保存并下发完整 JSON，适合面板暂未结构化支持的 sing-box 配置项。

## 导入出站

在「服务器 → 出站 → 新增出站」中，可手动填写，或解析一条节点 URI。解析后确认协议、地址和端口，填写出站标签并保存；出站会按现有配置下发流程应用到服务器。

URI 入口接收单条节点分享链接，解析结果保存为独立的出站配置。订阅地址由外部节点页面管理。

## 流量周期

在服务器新增或编辑中设置每月流量重置日（1–31 日，默认 1 日）。每个周期从该日 **UTC 00:00** 开始，到下次重置时刻结束；不足该日期的月份使用月末，例如 31 日在二月按 28 日或闰年 29 日起算。

流量页保留最近 1 小时、12 小时、24 小时、7 天和 30 天的切换，默认 24 小时，上传、下载、合计及入站明细随所选范围变化。时间统一使用 UTC；各范围只计入最近一次重置之后的流量，重置之前的图表时段显示为零。修改重置日会立即重新划分计量起点，不删除历史记录，也不重启 sing-box；统计范围为面板已采集的 sing-box 流量。

## 迁移选项

SQLite 数据库升级默认在迁移前生成一致性备份。明确无需备份的部署可临时设置环境变量 `SINGBOX_PANEL_SKIP_MIGRATION_BACKUP=true`，或在配置的 `database` 下设置 `skip_migration_backup: true`。

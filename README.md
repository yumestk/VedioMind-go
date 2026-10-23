# VedioMind Go

Go + React 的视频内容理解工作台。V1 目标：粘贴 Bilibili / YouTube 公共单视频链接，读取平台字幕、内嵌播放、生成总结和章节，并进行带字幕引用的多轮问答。

当前完成 **P3：完整字幕总结与持久化任务**。支持账号、链接导入、字幕/播放器、总结、任务恢复和账号隔离；章节和问答尚未实现。

## 本地运行

需要 Go 1.27.1、Node 24.22.1、PostgreSQL 18，以及 PATH 中可用的 yt-dlp（YouTube 元数据解析）。前后端依赖分别由 `go.sum` 和 `web/package-lock.json` 固定。

```sh
# 启动数据库；也可使用已有 PostgreSQL 并调整连接地址
POSTGRES_PASSWORD=vediomind_dev docker compose up -d postgres
cp .env.example .env
set -a
. ./.env
set +a
go run ./cmd/server
```

服务启动时执行嵌入的 Goose 迁移；也可只运行 `go run ./cmd/server migrate`。迁移失败则不启动 HTTP。

```sh
# 新终端，使用 .nvmrc 对应的 Node；不修改全局默认版本
nvm use
cd web
npm ci
npm run dev
```

访问 `http://localhost:5173`。Vite 将 `/api` 代理到 `127.0.0.1:8080`，浏览器始终同源。数据库端口为本机 `55437`；示例密码只用于本地开发。

## 检查

测试数据库账号需要创建/删除 schema 权限；每个集成测试使用独立 schema，不清空业务表。

```sh
export TEST_DATABASE_URL='postgres://vediomind:vediomind_dev@127.0.0.1:55437/vediomind?sslmode=disable'
go test -race ./...
go vet ./...
cd web
npm run build
npm test
```

没有 `TEST_DATABASE_URL` 时 PostgreSQL 集成测试会明确跳过，不能视为完成账号验收。CI 提供 PostgreSQL 18 并运行这些检查。

账号密码使用 bcrypt cost 12；Session 是 HttpOnly Cookie，数据库仅保存随机 token 的 SHA-256。修改请求要求精确匹配 `PUBLIC_ORIGIN` 和 CSRF Header。生产环境设置 `APP_ENV=production` 与 HTTPS `PUBLIC_ORIGIN`，启用 Secure Cookie。未配置信任代理，登录限流按直连来源 IP 和规范化账号共享固定分钟窗口；反向代理部署前应明确客户端 IP 信任边界。

`/api/health/live` 表示进程可响应，`/api/health/ready` 检查数据库。无需模型密钥即可启动。

本版本账号隔离基础不等于完整的多企业 SaaS。组织、计费、Redis、消息队列、向量库、上传和语音识别不属于 V1。

计划、ADR 和验收记录按约定只保留在本地 `docs/`，不提交 GitHub。

平台解析有网络超时与大小限制，仅请求受限平台/字幕域名，不读取浏览器 Cookie。YouTube 依赖 yt-dlp，仅检查元数据并读取 JSON3 字幕，不下载媒体。无字幕则拒绝导入且不保存半份记录。重复导入复用原有快照。平台可能限制字幕、区域和嵌入播放；受控测试通过不代表所有公开视频均可导入。

## 模型与任务

在后端环境中配置 `AI_API_KEY` 和 `AI_MODEL`（选用供应商当前支持的模型），可调整 `AI_BASE_URL`。密钥不可放入 `VITE_*` 或浏览器代码。未配置时服务仍能启动、导入和播放，分析任务会显示 `AI_NOT_CONFIGURED`。修改环境后重启服务，在视频页点击重试。

新导入原子保存字幕并创建分析任务。最多 2 个 worker（可配置 1–8）从 PostgreSQL 原子领取任务；租约 60 秒、每 15 秒续租，持有者及版本限制产物提交。单次调用 120 秒，首次领取起总计 10 分钟，最多 6 次领取、每阶段 3 次模型请求、总计 6 次。暂时失败写入重试时间并释放执行槽，永久错误立即结束。供应商可能在断连后仍处理请求，不能保证模型调用恰好一次。

`AI_CONTEXT_BUDGET` 默认 48000，按完整请求 JSON 字节数、输出预留和 2048 安全量做保守估计；这不是供应商精确 tokenizer 或 Token 计费。超限不截断字幕且不调用模型。每次实际模型尝试均消耗数据库共享的分钟额度（全局/模型各 60，账号 10）；固定窗口允许边界突发。

`GET /api/media/:id/analysis` 返回已保存总结与最近任务；另提供活动任务和任务详情接口。前端串行轮询，终态/离开页面后停止；总结 Markdown 经过 DOMPurify 清理。

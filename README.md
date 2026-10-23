# VedioMind Go

Go + React 的视频内容理解工作台。V1 目标：粘贴 Bilibili / YouTube 公共单视频链接，读取平台字幕、内嵌播放、生成总结和章节，并进行带字幕引用的多轮问答。

当前完成 **P2：视频链接、字幕与内嵌播放**。已支持账号、视频导入与历史、字幕搜索及时间定位、owner 隔离和删除；AI 总结、章节和问答尚未实现。

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

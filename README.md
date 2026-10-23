# VedioMind Go

Go + React 的视频内容理解工作台。V1 目标：粘贴 Bilibili / YouTube 公共单视频链接，读取平台字幕、内嵌播放、生成总结和章节，并进行带字幕引用的多轮问答。

已实现 **V1 核心闭环及部署配置**：账号 → 链接导入 → 字幕 / 内嵌播放 → 总结 → 章节 → 带引用的多轮问答。受控浏览器全链路已通过；真实模型调用按计划留待后续验收。

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

账号密码使用 bcrypt cost 12；Session 是 HttpOnly Cookie，数据库仅保存随机 token 的 SHA-256。修改请求要求精确匹配 `PUBLIC_ORIGIN` 和 CSRF Header。生产环境设置 `APP_ENV=production` 与 HTTPS `PUBLIC_ORIGIN`，启用 Secure Cookie。默认不信任转发来源，登录限流按直连来源 IP 和规范化账号共享固定分钟窗口；反向代理部署时，用 `TRUSTED_PROXIES` 明确可信代理地址/CIDR，禁止把任意客户端传入的转发头当作真实来源。

`/api/health/live` 表示进程可响应，`/api/health/ready` 检查数据库。无需模型密钥即可启动。

本版本账号隔离基础不等于完整的多企业 SaaS。组织、计费、Redis、消息队列、向量库、上传和语音识别不属于 V1。

计划、ADR 和验收记录按约定只保留在本地 `docs/`，不提交 GitHub。

平台解析有网络超时与大小限制，仅请求受限平台/字幕域名，不读取浏览器 Cookie。YouTube 依赖 yt-dlp，仅检查元数据并读取 JSON3 字幕，不下载媒体。无字幕则拒绝导入且不保存半份记录。重复导入复用原有快照。平台可能限制字幕、区域和嵌入播放；受控测试通过不代表所有公开视频均可导入。

## 模型与任务

在后端环境中配置 `AI_API_KEY` 和 `AI_MODEL`（选用供应商当前支持的模型），可调整 `AI_BASE_URL`。密钥不可放入 `VITE_*` 或浏览器代码。未配置时服务仍能启动、导入和播放，分析任务会显示 `AI_NOT_CONFIGURED`。修改环境后重启服务，在视频页点击重试。

新导入原子保存字幕并创建分析任务。默认 2 个 worker（可配置 1–8）从 PostgreSQL 原子领取任务；租约 60 秒、每 15 秒续租，持有者及版本限制产物提交。单次调用 120 秒，首次领取起总计 10 分钟，最多 6 次领取、每阶段 3 次模型请求、总计 6 次。暂时失败写入重试时间并释放执行槽，永久错误立即结束。供应商可能在断连后仍处理请求，不能保证模型调用恰好一次。

`AI_CONTEXT_BUDGET` 默认 48000，按完整请求 JSON 字节数、输出预留和 2048 安全量做保守估计；这不是供应商精确 tokenizer 或 Token 计费。超限不截断字幕且不调用模型。每次实际模型尝试均消耗数据库共享的分钟额度（全局/模型各 60，账号 10）；固定窗口允许边界突发。

`GET /api/media/:id/analysis` 返回已保存总结与最近任务；另提供活动任务和任务详情接口。前端串行轮询，终态/离开页面后停止；总结 Markdown 经过 DOMPurify 清理。

章节输出限定为 1–20 个真实字幕锚点；首章覆盖第一条字幕，顺序严格递增，标题最多 100 个 Unicode 字符。服务端回填起止时间，拒绝虚构 ID 和零时长章节。当前分析管线 v2 为总结→章节；旧 v1 成功总结保持有效，补全分析只生成缺少的阶段。阶段处理器版本与管线版本分别记录。

问答保留完整成功历史，每轮使用完整字幕和最近 6 轮成功对话。问题限 1–500 个 Unicode 字符。引用 ID 由后端验证，最多回填 5 条原文和毫秒时间；无证据时统一拒答。ID 存在不代表答案语义一定正确，仍需核对引用。

会话使用 PostgreSQL 180 秒占用与 revision，单次同步问答总时限 150 秒；调用期间不保持数据库长事务。同会话并发返回 409。请求标识以账号为作用域，绑定原始媒体/会话参数和问题；相同请求成功后重放同一回合，不重复写消息。失效请求的提交和释放均验证占用 token。整轮问题、答案、引用和请求成功状态原子提交。浏览器不把模型调用放入 Effect；失败保留输入与请求标识以供重试。

## 同源运行与容器

本地构建后，Go 可直接提供 React 静态页面及 API，支持深链接；未知 `/api` 路径始终返回 JSON 404。

```sh
# 在项目根目录；先按上文导出 DATABASE_URL 等配置
(cd web && npm ci && npm run build)
PUBLIC_ORIGIN=http://localhost:8080 go run ./cmd/server
```

浏览器访问 `http://localhost:8080`。`WEB_DIST` 默认为 `web/dist`。

```sh
# Compose 中数据库与应用一起启动；使用 URL 安全字符组成的本地密码
POSTGRES_PASSWORD=vediomind_dev PUBLIC_ORIGIN=http://localhost:8080 docker compose up --build -d
```

镜像包括编译后的 Go、React 静态资源、Python、Node 和固定版本 yt-dlp。应用以非 root 用户运行，数据库有持久卷和健康检查，应用在数据库就绪后启动。启动时迁移数据库；升级前应备份。停止容器使用 `docker compose down`，保留数据卷；不要把 `down -v` 当作日常停止命令。

生产需 HTTPS 反向代理、`APP_ENV=production`、真实 `PUBLIC_ORIGIN`、数据库备份与恢复演练、可信代理 CIDR 和外部网络连通性。模型服务地址在生产要求 HTTPS。本项目不包含企业组织、计费、负载压测或高可用数据库部署，不能把 V1 验收当作付费 SaaS 上线验收。

## 浏览器与真实平台验证

```sh
# 根目录已有 TEST_DATABASE_URL；测试使用独立 schema
cd web
npx playwright install chromium
npm run test:e2e
npm run test:e2e:production
```

两组均连接真实 Gin/PostgreSQL，外部平台、模型和 iframe 为受控响应。覆盖注册、导入、总结、章节、两轮问答/引用、刷新、删除、退出、恶意 Markdown、窄屏，以及离开页面后取消旧读取并停止轮询。测试替身只在 Go 测试进程启用，生产没有失败后伪造成功的开关。CI 另构建并启动完整 Compose，检查 Node/yt-dlp、数据库迁移、应用就绪和静态深链接。

手动真实平台 smoke（普通测试不会联网）：

```sh
LIVE_VIDEO_URL='https://www.bilibili.com/video/BV1jKat6REbW' go test ./internal/platform -run '^TestLivePlatform$' -count=1 -v
LIVE_VIDEO_URL='https://www.youtube.com/watch?v=dQw4w9WgXcQ' go test ./internal/platform -run '^TestLivePlatform$' -count=1 -v
```

2026-10-23 本地网络实测：两个链接分别取得 1680 条 Bilibili 自动中文字幕、61 条 YouTube 人工英文字幕，yt-dlp 为 2026.06.09。Bilibili 官方 iframe 在浏览器加载到可播放数据状态（readyState 4）；YouTube 同一样例的 iframe 显示 “This video is unavailable”，因此不声称 YouTube 真实播放已通过。页面提供原站链接；嵌入受限时不会自动下载或绕过平台限制。

真实 DeepSeek 总结/章节/问答尚未调用，输出质量与实际模型预算待单独验收。本机 Docker daemon 未运行；Linux CI 已通过完整 Compose 镜像构建、PostgreSQL 迁移、Node/yt-dlp 检查、应用就绪和静态深链接验证。

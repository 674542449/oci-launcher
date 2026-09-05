# OCI 免费额度多账号开机控制台 (Oracle Cloud Free Tier Multi-Account Instance Launcher)

企业级、军事级防护的高性能 Oracle Cloud Infrastructure (OCI) 永久免费额度开机与全生命周期管理面板。基于 **Go 1.22 + Gin + 官方 OCI SDK + PostgreSQL 16 + Redis 7 + Vue 3 + Tailwind CSS + Naive UI** 打造。

---

## 🌟 核心特性与架构亮点

### 1. 100% 免费额度零成本硬隔离 (Always Free Guard)
- **严格硬锁边界**：
  - **ARM 架构 (VM.Standard.A1.Flex)**：按账号类型区分免费额度——**免费号 ≤ 2 OCPU / 12 GB，升级号 (PAYG) ≤ 4 OCPU / 24 GB**；租户服务限额更低时以限额为准。账号类型由服务限额自动判定，也可在仪表盘手动覆盖；额度可通过 `FREE_A1_OCPU` / `FREE_A1_MEMORY_GB` / `PAYG_A1_OCPU` / `PAYG_A1_MEMORY_GB` 调整。
  - **AMD 架构 (VM.Standard.E2.1.Micro)**：严格限制总配额 ≤ 2 实例（各 1 OCPU / 1 GB 内存）。
  - **启动卷存储 (Boot Volumes)**：指定容量时 API 最小 50 GB，引导卷 + 块存储总容量严格限制 ≤ 200 GB（`FREE_STORAGE_GB`）。
  - **出站流量监控**：实时采集并监控 10 TB / 月免费出站流量指标（BytesOut），超限阈值自动告警。
- **开机前拦截校验**：在任务入库与执行前，通过并发实时探测当前租户已用资源与未终止实例，超限时**硬性拒绝并提示具体超额项目**，杜绝产生扣费。

### 2. 双轨账号属性智能侦测 (Free Tier vs PAYG / Promo)
- **服务限额探测**：在主区域查询 `standard-a1-core-count` / `standard-a1-memory-count` 服务限制（Always Free 租户为小额固定上限，已升级的 PAYG 租户通常为 16 及以上），据此判定 `FREE_TIER` / `PAYG`，判定依据在仪表盘完整展示。
- **主区域自动解析**：通过 `ListRegionSubscriptions` 取得主区域名称（如 `ap-tokyo-1`），免费资源只允许在主区域创建。
- **透明画像与手动覆核**：Dashboard 提供完整的侦测证据链展示卡，并支持用户一键手动切换账号标签。

### 3. 全动态镜像解析与极速存储
- **全动态 Ubuntu 发现**：通过 OCI 镜像目录实时检索 Canonical 官方 Ubuntu 平台镜像（仅 AVAILABLE 状态），按版本号提取最新两代 LTS 发行版，并按 Shape 自动适配 ARM64 (`aarch64`) 与 AMD64 (`x86_64`)。
- **VPU 性能档位**：开机时可选择引导卷性能档位（默认 120 VPU/GB）。注意：Always Free 只包含存储容量，不包含性能单元，**在已升级的 PAYG 租户上高于 10 VPU 的档位会按 VPU 计费**；引导卷支持 10~120 VPU、块存储支持 0~120 VPU 在线调整。

### 4. 双登录凭证与密码标签溯源
- **登录方式支持**：
  1. `root + SSH 密钥`：自动注入公钥，配置 sudo 免密与 SSH 服务。
  2. `root + 20 位高熵随机安全密码`：后端加密随机生成（大小写字母、数字、特殊符号混合），并在 Cloud-Init 中安全配置。
- **标签随时查看与修改**：随机生成的 root 密码自动同步保存至 OCI 实例的 `freeform_tags["root_password"]` 标签中。用户在控制台可在实例详情中随时**一键复制密码**，或**在线修改标签内容**。

### 5. 军事级网络与系统安全防御
- **SQL 注入零容忍**：100% 采用 GORM 参数化预编译查询，杜绝任何 SQL 拼接。
- **XSS 与 CSRF 双重硬锁**：全站严格 Content-Security-Policy，Vue 3 自动转义，Cookie 采用 `HttpOnly + SameSite=Strict`，配合 `X-CSRF-Token` 双重防御。
- **JWT 伪造防御**：HMAC-SHA256 强随机密钥签名，载荷严格绑定客户端设备指纹（UA + 浏览器特征 Hash），IP/设备突变立即失效。
- **2FA (TOTP) 防重放**：支持 Google Authenticator / Aegis / Bitwarden。采用恒定时间比对（`subtle.ConstantTimeCompare`），Redis 记录 Token 窗口唯一性防重放。
- **自动化诱捕蜜罐与 Fail2ban**：预设 `/phpmyadmin`、`/.env`、`/actuator` 等 10+ 诱捕端点，恶意扫描触发直接将 IP 加入 Redis 黑名单，并阶梯式延长封锁时间（10分钟 ~ 永久）。
- **扫描器特征绞杀与指纹隐形**：自动阻断 sqlmap、nmap、nikto、masscan、fofa 等探测流量；全面剥离 Server 头与 X-Powered-By 特征。
- **信封加密**：用户 OCI API 私钥均通过 AES-256-GCM 高强度信封加密后落库，内存使用后及时清零。
- **单账号互斥并发锁**：基于 Redis 分布式锁，强制单账号同一时间仅允许一个操作执行，杜绝并发越权与 API 限流风暴。
- **零被动后台轮询**：平时不产生任何无效 OCI 请求，仅在用户主动触发开机抢机任务时按需轮询。

---

## 🏗️ 目录结构

```text
oci/
├── docker-compose.yml              # Docker 编排配置（Postgres + Redis + 后端 + 前端 + 隧道）
├── deploy.sh                       # Debian / Ubuntu 一键自动化部署运维脚本
├── README.md                       # 系统完整说明书与安全自查表
├── backend/                        # Go 1.22 后端服务
│   ├── Dockerfile                  # 多阶段极简编译镜像
│   ├── go.mod                      # 模块依赖描述
│   ├── cmd/server/main.go          # 主服务入口，初始化 DB/Redis 并优雅停机
│   └── internal/
│       ├── config/                 # 环境变量与应用配置加载
│       ├── storage/                # PostgreSQL GORM 数据模型与迁移
│       ├── cache/                  # Redis 缓存、分布式锁、滑动窗口限流、Pub/Sub
│       ├── security/               # AES-256-GCM、防重放、指纹计算、蜜罐、安全中间件
│       ├── auth/                   # JWT 鉴权、TOTP 2FA、设备指纹校验
│       ├── oci/                    # OCI SDK 客户端池、配额探测、Ubuntu 动态检索、实例/网络/存储管理
│       ├── engine/                 # 开机抢机引擎：错误分类、指数退避、AD 轮换、多任务调度
│       ├── notify/                 # Telegram Bot HTML 格式即时告警推送
│       └── api/                    # RESTful API 路由、控制器与 WebSocket 实时日志流
└── frontend/                       # Vue 3 + Vite + Tailwind CSS + Naive UI 前端
    ├── Dockerfile                  # Node 20 编译 + Nginx 静态托管镜像
    ├── nginx.conf                  # Nginx 生产代理、CSP、反代后端与 WebSocket
    ├── package.json
    ├── vite.config.ts
    └── src/
        ├── api/client.ts           # Axios 实例（自动附带 Token、CSRF、指纹）
        ├── router.ts               # Vue Router 路由守卫与鉴权
        ├── stores/profile.ts       # Pinia 当前选中 OCI Profile 全局状态
        └── views/                  # 控制台页面（仪表盘、抢机、实例、存储、防火墙、配置）
```

---

## 🚀 部署指南

### 方法一：Debian / Ubuntu 一键极速部署 (推荐，一行命令全自动)

直接在目标服务器终端（以 root 权限）执行以下单行命令，将自动完成系统依赖检查、Docker 及 Compose 插件安装、源码拉取、生成高熵随机密码、配置 `.env` 并拉起容器集群：

```bash
# 方式 A：纯单行指令一键全自动安装 (推荐)
curl -fsSL https://raw.githubusercontent.com/674542449/oci-launcher/main/deploy.sh | sudo bash
```

或者通过 Git 克隆部署：

```bash
# 方式 B：手动克隆仓库部署
git clone https://github.com/674542449/oci-launcher.git /opt/oci-launcher
cd /opt/oci-launcher
chmod +x deploy.sh
./deploy.sh
```

部署完成后，脚本将在屏幕上打印生成的安全密钥、访问地址以及 Cloudflare Tunnel 配置提示。

---

### 方法二：Docker Compose 手动部署

#### 1. 创建并配置环境变量文件 `.env`
在项目根目录创建 `.env` 文件：

```ini
# PostgreSQL
POSTGRES_DB=oci_panel
POSTGRES_USER=oci_admin
POSTGRES_PASSWORD=YOUR_SUPER_STRONG_DB_PASSWORD

# Redis
REDIS_PASSWORD=YOUR_SUPER_STRONG_REDIS_PASSWORD

# 主密钥：加密所有 OCI 私钥并派生会话签名密钥（至少 16 位随机字符串；生产环境保留默认值将拒绝启动）
MASTER_KEY=YOUR_RANDOM_MASTER_KEY

# 可选：后端 IP 白名单（逗号分隔 IP 或 CIDR）
ALLOWED_IPS=

# 可选：信任其 X-Real-IP 头的反向代理网段，默认为 Docker 私有网段；后端直接暴露时填 none
TRUSTED_PROXIES=

# 可选：A1 免费额度（免费号 2 OCPU / 12 GB，升级号 4 OCPU / 24 GB），存储 200 GB，Micro 2 台
FREE_A1_OCPU=2
FREE_A1_MEMORY_GB=12
PAYG_A1_OCPU=4
PAYG_A1_MEMORY_GB=24
FREE_STORAGE_GB=200
FREE_MICRO_COUNT=2

# 可选：Cloudflare Tunnel 令牌
CLOUDFLARE_TUNNEL_TOKEN=
```

Telegram Bot Token 与 Chat ID 在面板「设置」页填写，不需要写入 `.env`。

#### 2. 构建并启动容器集群

```bash
# 构建并以后台守护进程启动
docker compose up -d --build

# 查看容器运行状态
docker compose ps

# 查看后端实时日志
docker compose logs -f backend
```

---

## 🛡️ Cloudflare Tunnel 物理隐身部署 (0 公网端口暴露)

为了彻底避免服务器公网 IP 被全网扫描器发现，推荐将控制台仅监听于 `127.0.0.1`，通过 Cloudflare Tunnel 进行域名反代：

1. **登录 Cloudflare Zero Trust 控制台**：
   - 导航至 **Networks** -> **Tunnels** -> 点击 **Create a Tunnel**。
   - 选择 **Cloudflared**，给隧道命名（例如 `oci-console`）。
2. **获取 Tunnel Token**：
   - 复制页面中显示的 Token（即 `--token` 后面的字符串）。
3. **启用 Tunnel 容器**：
   - 将 Token 填入根目录 `.env` 的 `CLOUDFLARE_TUNNEL_TOKEN` 项：
     ```ini
     CLOUDFLARE_TUNNEL_TOKEN=eyJhIjoiYmMy...
     ```
   - 取消 `docker-compose.yml` 中 `cloudflared` 服务的注释（或新建 `docker-compose.override.yml` 添加该服务），然后启动：
     ```bash
     docker compose up -d
     ```
4. **配置公共主机名 (Public Hostname)**：
   - 在 Cloudflare Tunnel 的 **Public Hostnames** 标签页中添加路由：
     - **Subdomain**: `oci`
     - **Domain**: `yourdomain.com`
     - **Service Type**: `HTTP`
     - **URL**: `frontend:80`
5. **生效效果**：
   - 服务器公网 IP 上 **无需开放 80、443 或 8080 端口**。
   - 所有外部访问经由 Cloudflare CDN WAF 保护，直接以 `https://oci.yourdomain.com` 安全访问。

---

## 📖 使用指南

### 1. 初次访问与管理员初始化
1. 打开控制台页面，系统会自动检测是否已初始化。
2. 若尚未初始化，将自动弹出 **“系统初始化设置”**：
   - 设置超级管理员用户名与强密码。
   - 系统将展示 **2FA (TOTP) 密钥**与二维码。请立即使用 Authenticator 应用扫码绑定。
   - 输入 6 位动态验证码完成激活。

### 2. 录入与管理 OCI Profile
1. 进入 **“账号配置 (Profiles)”** 页面，点击 **“添加 OCI 配置”**。
2. **支持直接粘贴 OCI CLI 配置文件内容**：
   ```ini
   [DEFAULT]
   user=ocid1.user.oc1..aaaaaaaaxxxxxxx
   fingerprint=12:34:56:78:90:ab:cd:ef:12:34:56:78:90:ab:cd:ef
   key_file=~/.oci/oci_api_key.pem
   tenancy=ocid1.tenancy.oc1..aaaaaaaaxxxxxxx
   region=us-ashburn-1
   ```
3. **私钥提供方式**：
   - 直接粘贴 PEM 文本（推荐）。
   - 上传 `.pem` 文件。
   - 指定服务器绝对路径。
4. 为配置添加个性化名称、颜色标签与备忘备注。系统在落库前将自动使用 AES-256-GCM 对私钥进行信封加密。

### 3. 创建开机/抢机任务 (Launcher)
1. 顶部全局切换需要操作的 OCI 账号。
2. 进入 **“抢机调度 (Launcher)”** 页面：
   - **预设模板**：支持一键选用 **“ARM 满配独享 (4C 24G 200G)”**、**“ARM 双机平分 (2C 12G 100G)”**、**“AMD 双机标准 (1C 1G 50G)”** 等 5 种经典合规方案。
   - **操作系统**：选择自动发现的最新 Ubuntu LTS（如 Ubuntu 26.04 或 24.04）。
   - **登录方式**：
     - `root + SSH 密钥`：粘贴您的公钥。
     - `root + 20位高熵随机密码`：自动生成并存放于实例标签。
   - **重试策略**：设置重试间隔（支持带抖动的指数退避）、可用区 (AD) 轮换策略、遇到 429 限流保护暂停时间。
3. 任务启动后，可在右侧终端窗口通过 **WebSocket 实时查看抢机日志流**。抢机成功后 Telegram 机器人将即刻发送推送。

### 4. 实例与网络生命周期管理
- **实例管理**：查看所有运行中实例的 CPU、内存、公网 IP、IPv6、启动卷大小及 VPU 等。
- **查看/修改 Root 密码**：点击实例操作项中的 **“查看标签 / 密码”**，可直观查看保存在 `freeform_tags["root_password"]` 中的密码明文，并支持随时在线更新。
- **一键更换公网 IP**：自动解绑并重新分配新的 Ephemeral Public IP。
- **端口连通性探测**：在线探测实例 22 端口或自定义端口是否通畅。
- **存储与 VPU 调优**：支持在 **存储卷 (Storage)** 页面针对引导卷在 10~120 VPU 间（引导卷最低 10 VPU）、块存储在 0~120 VPU 间无损在线滑动调节性能。
- **安全组一键放行**：在 **安全组 (Firewall)** 页面支持 **一键全通 (0.0.0.0/0)**、**一键清空** 或 **一键注入 Cloudflare CDN 官方回源节点 IP 列表**。

---

## 🔒 安全自查与代码对照表 (Security Self-Audit Checklist)

为兑现企业级与军事级安全承诺，系统对前述所有关键安全防御项进行了逐一审查。下表呈现了每个安全项与代码实现的具体映射：

| 序号 | 安全防护要求 | 核心技术方案 | 落实文件与实现位置 |
| :--- | :--- | :--- | :--- |
| **1** | **SQL 注入零容忍** | 100% GORM 参数化预编译查询，禁用任何原生字符串拼接语句 | [`backend/internal/storage/`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/storage/models.go), [`handlers_profile.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/api/handlers_profile.go) |
| **2** | **XSS 跨站脚本防护** | 响应头注入严格 CSP、X-XSS-Protection；Vue 3 虚拟 DOM 默认全自动 HTML 实体转义 | [`backend/internal/security/security.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/security/security.go#L115), [`nginx.conf`](file:///c:/Users/Administrator/Desktop/oci/frontend/nginx.conf) |
| **3** | **伪造签发 JWT 防护** | HMAC-SHA256 强随机密钥签名；载荷强制绑定客户端设备指纹 Hash；过期自动拒绝 | [`backend/internal/auth/auth.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/auth/auth.go#L35-L65) |
| **4** | **2FA (TOTP) 防重放** | 恒定时间比对 `subtle.ConstantTimeCompare`；Redis 缓存并原子锁定最近已用动态码 | [`backend/internal/auth/auth.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/auth/auth.go#L95-L120), [`cache/redis.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/cache/redis.go) |
| **5** | **阶梯式防爆破锁定** | Redis 滑动窗口记录登录失败次数，阶梯式封禁 IP 与账号（10分钟/1小时/24小时） | [`backend/internal/cache/redis.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/cache/redis.go#L85-L115), [`handlers_auth.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/api/handlers_auth.go) |
| **6** | **诱捕蜜罐与 Fail2ban** | 注册常见探测路径（`/phpmyadmin`, `/.env` 等），任何命中立即封禁 IP 并在 Redis 中持久拉黑 | [`backend/internal/security/security.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/security/security.go#L170-L205), [`router.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/api/router.go) |
| **7** | **扫描器特征绞杀** | 实时比对 User-Agent 与常见爬虫扫描器特征（sqlmap/nmap/nikto 等），命中直接 403 阻断 | [`backend/internal/security/security.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/security/security.go#L140-L168) |
| **8** | **服务器指纹伪装抹除** | 移除 Nginx 及 Gin 默认的 `Server`、`X-Powered-By` 头，统一伪装为标准无标识头 | [`frontend/nginx.conf`](file:///c:/Users/Administrator/Desktop/oci/frontend/nginx.conf), [`security.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/security/security.go#L115) |
| **9** | **数据与私钥信封加密** | OCI 私钥落库前经 AES-256-GCM 加密，内存读取使用后立即执行内存覆盖清零 (`ZeroBytes`) | [`backend/internal/security/security.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/security/security.go#L35-L85) |
| **10** | **100% 零成本硬性防护** | 任务调度前通过 Goroutine 并发多维查询租户配额与已分配资源，超出免费阈值硬性阻断 | [`backend/internal/oci/quota.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/oci/quota.go#L80-L135), [`handlers_task.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/api/handlers_task.go) |
| **11** | **单账号互斥并发锁** | 基于 Redis `SetNX` 实现 Profile 级分布式排他锁，杜绝单账号并发抢机造成超额或风控 | [`backend/internal/cache/redis.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/cache/redis.go#L30-L55), [`engine/worker.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/engine/worker.go) |
| **12** | **零被动后台轮询** | 不设立任何全局定时轮询 Goroutine，健康检查与资源统计仅在用户主动触发时执行 | [`backend/internal/oci/health.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/oci/health.go), [`engine/scheduler.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/engine/scheduler.go) |
| **13** | **一键紧急熔断停机** | 后端提供 `/api/auth/panic-lock` 端点，触发后即刻清理所有 Session、注销任务并封锁入口 | [`backend/internal/api/handlers_auth.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/api/handlers_auth.go#L190-L215), [`engine/scheduler.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/engine/scheduler.go) |
| **14** | **本地回环免疫防自锁** | 127.0.0.1 / ::1 拥有最高黑名单豁免权，防止管理员本地 curl 或探针意外封禁自身 | [`backend/internal/cache/redis.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/cache/redis.go), [`security/security.go`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/security/security.go) |
| **15** | **官方 OCI 规范严格对齐** | 严格补齐 `ListBootVolumes` 必填 AD 参数遍历、修正 10TB `VnicToNetworkBytes` 监控、更正 IPv6 字段 | [`backend/internal/oci/`](file:///c:/Users/Administrator/Desktop/oci/backend/internal/oci/quota.go) |

---

## 📄 许可证 (License)

MIT License. 仅供学习、研究与合规管理个人 Oracle Cloud 免费额度资源使用，请勿用于违反 Oracle 服务条款或任何商业滥用用途。

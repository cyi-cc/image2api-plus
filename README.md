<div align="center">

<img src="frontend/public/favicon.svg" width="84" alt="image2api" />

<h1>image2api</h1>

**vivid 协议 AI 生图 / 生视频网关 —— 渠道化接入同构上游,开箱即用的运营系统**

<sub>线上实例(品牌):[Vivid AI · vividai.run](https://vividai.run)</sub>

**简体中文** | [English](README.en.md)

[![Online Demo](https://img.shields.io/badge/在线体验-vividai.run-7c3aed?style=for-the-badge)](https://vividai.run)

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Vue 3](https://img.shields.io/badge/Vue-3-42b883?logo=vuedotjs&logoColor=white)](https://vuejs.org)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?logo=docker&logoColor=white)](#-部署)
[![Self-hosted](https://img.shields.io/badge/self--hosted-yes-success)](#-部署)
[![License](https://img.shields.io/badge/license-MIT-blue)](#-license)

[在线体验](https://vividai.run) · [功能](#-核心功能) · [部署](#-部署) · [API 文档](#-vivid-协议-api) · [交流群](#-交流--联系)

<br/>

<img src="docs/screenshots/home.png" alt="image2api — 首页" width="860" />

</div>

---

## 📖 目录

- [简介](#-简介)
- [界面预览](#-界面预览)
- [核心功能](#-核心功能)
- [渠道架构](#-渠道架构)
- [vivid 协议 API](#-vivid-协议-api)
- [部署](#-部署)
- [技术栈](#-技术栈)
- [仓库结构](#-仓库结构)
- [交流 / 联系](#-交流--联系)
- [License](#-license)

## ✨ 简介

**image2api** 是一个同构网关:对上,把任意多个 **vivid 协议(vividai)实例**接成「渠道」统一调度;对下,对外暴露同一套 **vivid 协议 API**(`POST /v1/generate` NDJSON 流 · `POST /v1/uploads` 预签名直传 · `GET /v1/models` · `GET /v1/balance`)。没有内置供应商代码、没有账号池 —— 上游就是另一个 vividai,套娃式组网任意层叠。

它不只是 API 网关:自带**积分计费、CDK 充值、在线充值(易支付)、邀请奖励、代理价体系、用户体系、管理后台、现代化画图前端**,一条命令即可跑成一个对外运营的 AI 生成站点 —— 作者的线上实例 **[Vivid AI · vividai.run](https://vividai.run)** 即基于本项目搭建。

> 💡 前后端**完全开源**(MIT),Go + Vue 3,可自由二开 / 自部署。

**一句话亮点** 🔁 vivid 协议渠道组网 · 📦 保存渠道即全量同步模型 · 💳 积分 + 充值 + 代理价 · 🧩 并发分组 · 🎨 画图前端 + 管理后台 · 🐳 一键部署(TLS 反代自理)

## 🖼️ 界面预览

<div align="center">
<sub>✨ Vivid 风格浅色界面 · 渠道化调度 · 数据驱动的运营看板</sub>
</div>

<table>
  <tr>
    <td width="33%"><img src="docs/screenshots/playground.png" alt="画图台" /></td>
    <td width="33%"><img src="docs/screenshots/dashboard.png" alt="概览看板" /></td>
    <td width="33%"><img src="docs/screenshots/models.png" alt="模型管理" /></td>
  </tr>
  <tr>
    <td align="center"><b>🎨 画图台</b><br/><sub>生图 / 生视频一站式 · 参考图上传 · 实时计价</sub></td>
    <td align="center"><b>📊 概览看板</b><br/><sub>用户 / 生成量 / 渠道健康 / 24h 趋势</sub></td>
    <td align="center"><b>🧩 模型管理</b><br/><sub>渠道绑定与权重 · 普通价 + 代理价 · 配价即上架</sub></td>
  </tr>
  <tr>
    <td width="33%"><img src="docs/screenshots/channels.png" alt="渠道管理" /></td>
    <td width="33%"><img src="docs/screenshots/users.png" alt="用户管理" /></td>
    <td width="33%"><img src="docs/screenshots/logs.png" alt="调用日志" /></td>
  </tr>
  <tr>
    <td align="center"><b>🔌 渠道管理</b><br/><sub>保存即全量同步上游模型 · 余额实时探测 · 故障转移</sub></td>
    <td align="center"><b>👥 用户管理</b><br/><sub>角色 / 并发组 / 积分 · 批量选中操作</sub></td>
    <td align="center"><b>📜 调用日志</b><br/><sub>成功 / 失败 / 进行中 · 提示词与耗时全留痕</sub></td>
  </tr>
</table>

## 🚀 核心功能

#### 🎨 生成能力
- 生图 + 生视频一站式,支持**参考图 / 首末帧 / 视频 / 音频**等多模态参考(按模型能力声明下发)
- 多分辨率(图像 1K / 2K / 4K · 视频 720p / 1080p / 768p)、多宽高比、视频多时长(离散档或区间档按秒计价),按模型独立配置与定价
- **配价即上架**:模型无启停开关,全部档位有价 + 绑定了启用渠道即对外可见;不定价 = 隐藏
- **模型别名**:同一上游模型可配多个对外 id,API 调用任意别名均可命中
- **去AI特征**(可选):画图台一键开启,生成图片自动做去AI痕迹处理,按画质档位加收积分(后台可改价、可整体关闭)

#### 🔁 渠道调度
- **保存渠道即全量同步**:填上游 `base_url` + `vk_` key 保存,自动拉取上游 `/v1/models` 全部入库绑定;上游消失的模型自动解绑,无其他渠道的孤儿模型自动清理
- **权重 + 并发感知调度**:渠道可配权重(加权随机)与并发上限,逐渠道尝试、失败自动故障转移
- **错误分类**:上游 4xx(参数/额度)直接透传中文拒绝理由、不重试;网络/5xx 换渠道重试
- 渠道 API Key 以 AES-GCM 加密落库(`CHANNEL_SECRET`),上游余额实时探测

#### 🔌 vivid 协议 API
- `POST /v1/generate` —— NDJSON 流式生成:首行回执 `jobId`,约 25s 空行心跳,终帧 `succeeded`/`failed`
- `POST /v1/uploads` —— 预签名 PUT 直传参考文件到对象存储
- `GET /v1/models` —— 裸数组,含能力规格(清晰度档位 / 比例 / 时长 / 参考文件上限)
- `GET /v1/balance` —— 余额查询
- **jobId 断流重连**:断流后仅带 `jobId` 重发同一请求即可续等,不重复下单、不重复扣费;进程重启后带 `jobId` 的任务也会自动续等
- 统一错误包 `{"error":{"code","message"}}`(4001 参数拒绝 / 4002 积分不足 / 4011 未授权 / 4293 并发上限 / 5001 服务异常);API Key 为 `vk_` + 48 hex

#### 💳 计费与运营
- 积分制(**预扣 + 失败退款**),按模型 / 分辨率 / 时长精细定价;去AI特征按画质另计附加积分
- **代理价体系**:用户可设为「代理」角色,模型可设代理价;代理用户(含其 API Key 调用)自动按代理价计费,未设代理价则回退普通价
- **在线充值(易支付)**:微信 / 支付宝扫码,金额档位 + 自定义,订单 30 分钟未付自动取消,支付回调 MD5 验签 + 幂等自动到账
- **CDK 兑换码**充值 · **邀请奖励** · 邮箱注册 / 验证码 / 找回密码
- **并发分组**:按分组限制用户「同时生成数」(画图台 + API Key 合计,`0` = 不限),Redis 自愈计数,新用户自动入默认组
- 三级角色:普通用户 / 代理 / 管理员

#### 🖥️ 用户前台(Vue 3)
- 画图台 · 创作记录画廊 · 生成日志(含失败原因 / 来源标签)
- 作品画廊**多选批量操作**:全选本页 / 批量删除 / 批量下载(自动打包 zip)
- **充值 · 订单** · API 文档 · API Key 管理 · 邀请 · 无限画布
- **站内公告**:登录后自动弹出 Markdown 公告

#### 🛠️ 管理后台
- 概览看板(趋势 / DAU / 失败 Top / 消费榜)
- **渠道管理**(保存即同步模型 / 上游余额 / 启用开关 / 并发 / 权重)· 模型管理(普通价 + 代理价 + 别名,上架状态只读)· **并发分组** · **订单管理** · 全站日志 · 用户管理(设为代理 / 分配并发组)· CDK · 图片管理 · 展示位 · **站点公告** · 站点配置(易支付、去AI特征等)
- **违禁词管理**:提示词命中即拦截,按词 / 按用户统计触发次数

**🧰 工程亮点**:媒体存 S3/RustFS 经鉴权代理分发 + 保留期清理 · 预签名直传参考文件 · 自愈式维护轮询(僵死任务续等 / 退款、过期文件清理)· 一条命令 Docker 部署(TLS 交给你的反代)。

## 🔌 渠道架构

```
调用方 ──vivid 协议──▶ image2api ──vivid 协议──▶ 上游 vividai 实例 A (渠道, 权重 w, 并发 c)
                     (本服务)                 ├─ 上游 vividai 实例 B
                                              └─ …任意多个
```

- 渠道 = 一个同构上游:`base_url` + `vk_` API Key + 启用开关 + 并发上限 + 调度权重
- 模型来自渠道同步(上游 `/v1/models`),本地只做别名映射与定价;**模型可在多个渠道间故障转移**
- 本服务对外同样说 vivid 协议 —— 所以它自己也能被接成别人的渠道,无限层叠组网
- **不支持接 OpenAI 协议上游**:上游必须是 vivid 协议实例

## 📡 vivid 协议 API

```bash
# 文生图 / 生视频 —— NDJSON 流,首行回执 jobId
curl -N https://你的域名/v1/generate \
  -H "Authorization: Bearer vk_xxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{"model": "seedance-2.0", "prompt": "a cute cat on a desk", "ratio": "16:9", "quality": "720p"}'
# → {"jobId":"…","status":"running"}   …空行心跳…   {"status":"succeeded","outputs":[{"url":"…"}]}

# 断流重连:只带 jobId 重发,不重复下单
curl -N https://你的域名/v1/generate -H "Authorization: Bearer vk_xxxx" -d '{"jobId":"<上文 jobId>"}'

# 参考文件:先拿预签名上传会话,PUT 直传后拿 ref key
curl https://你的域名/v1/uploads -H "Authorization: Bearer vk_xxxx" -d '{"kind":"image","mime":"image/png"}'

# 模型清单(裸数组)与余额
curl https://你的域名/v1/models  -H "Authorization: Bearer vk_xxxx"
curl https://你的域名/v1/balance -H "Authorization: Bearer vk_xxxx"
```

完整参数、错误码表与 Python 示例见站内 **/docs** 文档页。

## 🚀 部署

> 域名 + HTTPS 由你自己的反向代理处理(本项目不内置证书签发)。

**Docker(推荐)**:`docker compose up -d --build` 一条命令拉起 PostgreSQL + Redis + RustFS + 后端 + 前端(nginx **HTTP 监听容器 2000 端口**),把你的反向代理指到 `http://<本机>:2000`。

> ⚠️ 部署前把 `docker-compose.yml` 里的密码 / 密钥改掉 —— 特别是 **`CHANNEL_SECRET`**(渠道上游 Key 的加密密钥,变更后已存渠道密钥无法解密)。

也可**从源码手动构建**,自备 PostgreSQL · Redis · RustFS(或任意 S3)· 反向代理:

```bash
# 1. 创建空库(后端启动自动建表)
createdb vivid_ai

# 2. 配置并从源码构建后端 —— 完整环境变量见 backend/.env.example
cd backend && go build -o bin/api ./cmd/api && ./bin/api   # 监听 127.0.0.1:6666

# 3. 构建前端(产物 frontend/dist)
cd frontend && npm install && npm run build
```

Nginx 反代(证书自行用 certbot / acme.sh):

```nginx
server {
    listen 443 ssl;
    server_name 你的域名;
    ssl_certificate     /path/fullchain.pem;
    ssl_certificate_key /path/privkey.pem;
    root /path/to/frontend/dist;
    index index.html;
    client_max_body_size 50m;
    proxy_read_timeout 600s;            # 视频生成耗时长
    location /assets/ { expires 1y; add_header Cache-Control "public, max-age=31536000, immutable"; }
    location / { try_files $uri $uri/ /index.html; add_header Cache-Control "no-cache"; }
    location ^~ /admin/api/ { proxy_pass http://127.0.0.1:6666; }
    location ^~ /images/    { proxy_pass http://127.0.0.1:6666; }
    location = /health      { proxy_pass http://127.0.0.1:6666; }
    location ^~ /v1/        { proxy_pass http://127.0.0.1:6666; add_header Cache-Control "no-store" always; }
}
```

部署后:后台「渠道」页新建渠道(上游 URL + `vk_` key)→ 保存即自动同步全部上游模型 → 模型管理里配价 → 画图台 / 对外 API 立即可用。

## 🧱 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go · gin · gorm(PostgreSQL)· go-redis |
| 前端 | Vue 3 · Vue Router · Vite · Tailwind CSS v4 |
| 基础设施 | PostgreSQL · Redis · RustFS(S3 兼容)· Nginx |

## 📦 仓库结构

```
backend/                       后端源码(Go)
├── cmd/api/                   服务入口(main)
├── internal/
│   ├── bootstrap/             应用装配、定时维护任务启动
│   ├── config/                环境变量配置加载
│   ├── http/
│   │   ├── handler/           HTTP 处理器(/v1 vivid 协议、渠道管理、鉴权…)
│   │   ├── middleware/        鉴权 / 请求 ID 等中间件
│   │   └── router/            路由注册
│   ├── model/                 GORM 数据模型(渠道 / 模型 / 用户 / 事件…)
│   ├── upstream/              vivid 协议上游客户端(NDJSON / 上传 / 模型 / 余额)
│   ├── repo/                  数据访问层
│   ├── service/               业务逻辑(渠道调度、计费、维护、v1 job 生命周期)
│   └── storage/               RustFS / S3 媒体存储(预签名 PUT/GET)
├── Dockerfile                 多阶段构建
└── .env.example               后端环境变量模板

frontend/                      前端源码(Vue 3 + Vite)
├── src/
│   ├── views/                 页面(画图台 / 渠道 / 模型 / 用户 / 并发组 / 订单 / 日志 / 概览 / 充值 / 设置…)
│   ├── components/            复用组件(弹窗 / 选择器 / 灯箱…)
│   ├── layouts/               公共 / 后台布局
│   └── api.js · auth.js …     接口封装、鉴权、主题、积分等
├── Dockerfile                 Nginx 静态托管(HTTP :2000)+ API 反代
└── default.conf.template      Nginx 站点模板(反代 + 缓存策略)

docker-compose.yml             Docker 编排(Postgres / Redis / RustFS / 后端 / 前端)
```

## 💬 交流 / 联系

| | |
|---|---|
| 🌐 官网 | **[vividai.run](https://vividai.run)** |
| 👥 QQ 交流群 | **1106849765** · [点击加群](https://qm.qq.com/q/976LeMFoHu) |
| 🐧 QQ | **1114639355** · [加好友](https://qm.qq.com/q/ItgCcNA7ac) |
| 🛒 小店 | **[pay.ldxp.cn/shop/chiyi](https://pay.ldxp.cn/shop/chiyi)** |
| ✉️ 邮箱 | vividairun@gmail.com |

## 📄 License

本项目(前端 + 后端)基于 [MIT](LICENSE) 协议开源,可自由使用、修改、商用与二次分发。

<div align="center">

如果这个项目对你有帮助,欢迎 ⭐ Star 支持!

</div>

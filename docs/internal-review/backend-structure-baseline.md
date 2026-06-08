# Backend Structure Baseline

更新时间：2026-06-08 11:07 CST

本基线对应 2026-06-08 这轮后端架构重放在批次 6 完成后的状态。目标从“冻结旧债务”切换为“固化已收敛结果”：继续阻止新增大文件、`sdk -> internal` 反向依赖和 management handler 直连持久化路径，同时把 allowlist 收紧到当前真实剩余债务。

## 扫描命令

```bash
python3 scripts/check-backend-structure.py
```

CI 中也会运行同一脚本。扫描器只依赖 Python 标准库，allowlist 位于：

```text
docs/internal-review/backend-structure-allowlist.json
```

## 当前结构指标

基于 2026-06-08 批次 6 完成后的本地基线：

| 指标 | 数量 |
| --- | ---: |
| Go 文件总数 | 885 |
| 生产 Go 文件 | 645 |
| 测试 Go 文件 | 240 |
| `internal/` Go 文件 | 698 |
| `internal/` 生产 Go 文件 | 518 |
| `internal/` 测试 Go 文件 | 180 |
| 生产 Go 文件中 `>800` 行 | 2 |
| 生产 Go 文件中 `>1200` 行 | 1 |
| `internal/` 生产 Go 文件中 `>800` 行 | 1 |
| `internal/` 生产 Go 文件中 `>1200` 行 | 1 |
| 生产 `sdk/**` 中直接导入 `internal/**` 的文件 | 20 |
| 管理端 `Handler` receiver 方法 | 174 |
| `server.go` 内管理路由注册 | 0 |
| `internal/` 生产目录 | 114 |
| `internal/` 有同级测试目录 | 54 |
| `internal/` 无同级测试目录 | 60 |

## 当前 `>1200` 行生产文件

这些文件是当前仅剩的 `>1200` 行生产文件，只允许通过 allowlist 保持现状；继续增长会触发结构扫描失败。

| 文件 | 行数 | 治理阶段 |
| --- | ---: | --- |
| `internal/registry/model_definitions_static_data.go` | 1233 | 静态数据例外 |

## 当前 `>800` 行生产文件

| 文件 | 行数 | 说明 |
| --- | ---: | --- |
| `internal/registry/model_definitions_static_data.go` | 1233 | 静态模型定义例外 |
| `sdk/api/handlers/handlers.go` | 1160 | SDK façade 仍偏厚，但未新增 `sdk -> internal` 债务 |

## 门禁规则

- 生产 Go 文件 `>800` 行：扫描输出 warning，作为治理提示。
- 生产 Go 文件 `>1200` 行：默认失败；只有 `backend-structure-allowlist.json` 中登记的历史债务可通过。
- allowlist 中的大文件带有 `max_lines`，文件继续增长会失败；收敛后应同步收紧 allowlist。
- 生产 `sdk/**` 文件禁止新增对 `github.com/router-for-me/CLIProxyAPI/v6/internal/**` 的导入。
- 现存 `sdk -> internal` 导入按文件和 import path 精确登记；同一文件新增 internal import 也会失败。
- 管理端 handler 禁止新增对 YAML/SQLite 持久化函数的直接调用。
- 当前生产 management handler 直连持久化调用已清零；若再次出现将直接触发扫描失败。

## 架构例外登记

- `internal/registry/model_definitions_static_data.go` 是静态模型定义数据，暂按静态数据例外处理；如果后续引入生成器或数据文件，应将其移出业务大文件债务。

## 剩余热点 Owner 映射

| 对象 | 当前 owner | 当前状态 | 退出条件 |
| --- | --- | --- | --- |
| `internal/registry/model_definitions_static_data.go` | registry / model definitions | 静态数据例外，仍在 `>1200` allowlist | 引入生成器或外部数据文件后移出业务大文件 |
| `sdk/api/handlers/handlers.go` | SDK transport façade | 仍是唯一 `>800` 的 SDK 生产热点 | 继续通过 `sdkbridge/*` 下沉横切能力，使 `sdk -> internal` 文件数继续降到 `<=15` |
| `internal/management/apitools/*` | management API tools service | `api_tools.go` 已降到 130 行，外部 I/O owner 已迁出 handler | 后续继续把 provider refresh 细分为更窄 bridge/adapter 时同步补测试 |
| `internal/management/imagegeneration/service.go` | management image generation service | 任务 registry、timeout、phase hook、TTL cleanup 已归 service | 如新增任务类型，必须复用同一 owner / context / cleanup contract |
| `internal/management/updateflow/*` | management update service | GitHub 查询、updater health/progress/apply 已归 service，`update.go` 降到 166 行 | 后续若新增更新源或后台轮询，必须继续挂到该 service owner 下 |

## 重构前契约测试清单

后续阶段开始迁移前，应按影响面补齐或复核以下测试：

- 管理 API route smoke。
- auth files list/upload/delete/patch 响应字段和状态码。
- config YAML 与 DB-backed runtime settings overlay 顺序。
- management image generation task lifecycle / error body / multipart edits。
- management update check / current state / progress / updater trigger contract。
- request logs query/filter/content/cleanup。
- quota snapshot 写入、查询与保留策略。
- provider executor non-stream/stream/error body 基础路径。
- SSE event translation 和 usage reporting。
- auth manager selection/retry/cooldown/refresh 并发行为。
- service/server route registration、middleware 顺序和 shutdown path。
- SDK public package compatibility compile tests。

## 后续维护要求

- 新业务数据默认进入 SQLite/数据库和管理 API，不得为了前端实现方便新增到 `config.yaml`。
- 新管理 API 必须通过 transport + use case/service 边界进入，不得把业务规则继续堆到超级 `Handler`；runtime settings 持久化也必须走 service/bridge，不得回退到 handler 直写。
- 新 provider 执行逻辑必须优先复用 runtime pipeline 或先补 pipeline 抽象，不得复制完整横切 executor 模板。
- 每个阶段结束时都要更新本基线或 allowlist，确保债务数量只减少、不扩大。

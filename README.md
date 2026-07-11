# Pipeline MaxHit

> ⚠️ **纯 Vibe 项目** — 全部代码由 Claude (Anthropic) 生成，无手写。

计算 MAA Framework Pipeline 中每个节点在一个任务中的**理论最大执行次数**。

## 原理

基于 MAA Framework 任务流水线协议，将 Pipeline 建模为带容量约束的有向图，通过 Dinic 最大流求解。对修改图结构的任务选项进行枚举，取各组合下每个节点的最大执行次数。

### 处理的约束

| 约束 | 说明 |
| ------ | ------ |
| `max_hit` | 节点容量上限 |
| `next` / `on_error` | 有向边（成功 / 失败跳转） |
| `jump_back` | 回跳边，子链结束后返回父节点 |
| `DirectHit`（无 inverse） | 一定命中，在 next 列表中阻塞后续节点 |
| `StopTask` | 终止任务链，出边全部移除 |
| `anchor` | 锚点引用，流不敏感上近似展开 |
| `option` / `pipeline_override` | 任务选项覆盖，修改图结构的选项按组合枚举，其余乐观并集 |

## 安装

```bash
go install github.com/ocsin1/pipeline-maxhit@latest
```

或本地构建：

```bash
git clone https://github.com/ocsin1/pipeline-maxhit
cd pipeline-maxhit
mkdir -p install
go build -o install/pipeline-maxhit.exe .
```

Go 1.22+，无外部依赖。

## 用法

### 指定入口节点

```bash
pipeline-maxhit -pipeline assets/resource/pipeline -entry OpenGame
```

可选 `-defaults assets/resource/default_pipeline.json` 加载节点默认值。

### 从任务定义读取

```bash
# 单个任务（默认取首个）
pipeline-maxhit -pipeline assets/resource/pipeline -task assets/tasks/ItemTransfer.json

# 指定任务名
pipeline-maxhit -pipeline assets/resource/pipeline -task assets/tasks/AutoCollect.json -task-name AutoCollect

# 逗号分隔多个任务
pipeline-maxhit -pipeline assets/resource/pipeline -task assets/tasks/AutoCollect.json -task-name AutoCollect,SellProduct

# 运行文件中全部任务
pipeline-maxhit -pipeline assets/resource/pipeline -task assets/tasks/ItemTransfer.json -task-name all
```

选项覆盖自动分析——修改图结构（`next` / `recognition` / `action`）的选项按组合枚举，其余乐观并集。进度输出到 stderr，结果输出到 stdout。

### 列出任务

```bash
pipeline-maxhit -task assets/tasks/AutoCollect.json -list-tasks
```

### 参数

| 参数 | 必填 | 说明 |
| ------ | ------ | ------ |
| `-pipeline` | 是 | Pipeline JSON 目录路径 |
| `-entry` | 否* | 入口节点名（覆盖 `-task` 的 entry） |
| `-task` | 否* | 任务接口 JSON 文件路径 |
| `-task-name` | 否 | 任务名（默认取文件首个任务） |
| `-defaults` | 否 | `default_pipeline.json` 路径 |
| `-list-tasks` | 否 | 列出任务后退出 |

\* `-entry` 或 `-task` 至少提供一个。

## 输出

``` text
Pipeline Max-Exec Analysis
==========================

Total nodes: 2005
Reachable:   277
Zero-exec:   0

⚠  SCC with unbounded max_hit: NodeA, NodeB, NodeC

Node                          MaxHit      MaxExec   Source
──────────────────────────────────────────────────────────
WaitBlackScreen               UINT_MAX   1000000000 JumpBack
ClickContinue                 UINT_MAX   1000000000 JumpBack
EnterGame                     UINT_MAX            1 Normal(from OpenGame)
OpenGame                      UINT_MAX            1 Entry
```

### 列说明

| 列 | 说明 |
| ---- | ------ |
| `Node` | 节点名 |
| `MaxHit` | max_hit 配置（`UINT_MAX` = 无限制） |
| `MaxExec` | 理论最大执行次数，0 见 Source 列原因 |
| `Source` | `Entry` / `Normal(from X)` / `JumpBack` / `Zero` / `Unreachable` / `Blocked(by X)` |

### SCC 警告

多个节点通过普通边形成 SCC 且全部 max_hit = UINT_MAX 时输出警告，理论执行次数无上界（算法用 1e9 代理）。

## 注意事项

- **理论最大值**是对所有合法执行路径的安全上界，实际运行可能达不到
- 修改图结构的选项（`next` / `recognition` / `action`）**按组合枚举**取 max，不修改图结构的选项（`enabled` / `max_hit` 等）用**乐观并集**，互斥选项可能导致高估
- 锚点引用为流不敏感近似（不跟踪执行顺序）
- `SubTask` custom action 不展开为边，无法到达纯 SubTask 触发的节点
- `max_hit = UINT32_MAX` 在流网络中用 1e9 代理

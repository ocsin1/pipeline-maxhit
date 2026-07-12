# Pipeline MaxHit

> ⚠️ **纯 Vibe 项目** — 全部代码由 Claude (Anthropic) 生成，无手写。

检测 MAA Framework Pipeline 是否存在**可能的死循环**（无限执行），同时给出每个节点的理论最大执行次数。

MAA Pipeline 是识别-动作的任务流水线。如果节点间形成回环且缺少 `max_hit` 限制，就可能在运行时无限循环——这是 Pipeline 编写中最危险的 bug。本工具通过静态分析在上线前发现这类问题。

## 死循环检测

工具从两个维度检测潜在死循环：

### SCC 警告（普通边回环）

多个节点通过 `next` / `on_error` 普通边形成**强连通分量（SCC）**，且所有节点 `max_hit = UINT_MAX`（无限制）时，输出警告：

```text
⚠  SCC with unbounded max_hit: NodeA, NodeB, NodeC
```

这些节点可以互相可达且没有执行次数上限——**几乎确定是死循环**。

### JumpBack 高执行值（回跳链）

`jump_back` 边创建"子程序"语义：子链结束后返回父节点继续执行。如果回跳链上的节点没有 `max_hit` 限制，执行次数会飙升到 1e9（UINT_MAX 的代理值）：

```text
Node                   MaxHit      MaxExec   Source
WaitBlackScreen        UINT_MAX   1000000000 JumpBack
```

Source 列为 `JumpBack` 且 MaxExec 极大的节点，说明存在**无上限的回跳路径**——可能无限循环。

### 零执行节点

可达但 MaxExec = 0 的节点被流网络判定为无法到达，可能是被 `DirectHit` 阻塞或路径断裂，辅助定位逻辑错误。

## 原理

基于 MAA Framework 任务流水线协议，将 Pipeline 建模为带容量约束的有向图，通过 Dinic 最大流求解。对修改图结构的任务选项进行枚举，取各组合下每个节点的最大执行次数。

### 处理的约束

| 约束 | 说明 |
| ------ | ------ |
| `max_hit` | 节点容量上限，有限制时阻断 SCC 警告 |
| `next` / `on_error` | 有向边（成功 / 失败跳转） |
| `jump_back` | 回跳边，子链结束后返回父节点 |
| `DirectHit`（无 inverse） | 一定命中，在 next 列表中阻塞后续节点 |
| `StopTask` | 终止任务链，出边全部移除 |
| `anchor` | 锚点引用，流不敏感上近似展开 |
| `option` / `pipeline_override` | 任务选项覆盖，修改图结构的选项按组合枚举，其余乐观并集 |
| Controllable 分支 | 多子节点可控分支隔离重算，避免流饿死导致漏报 |

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

默认加载 `default_pipeline.json`（位于 pipeline 目录下）。也可单独指定：`-defaults assets/resource/default_pipeline.json`。

### 从任务定义读取

任务接口文件来自 [MaaEnd](https://github.com/MaaEnd/MaaEnd) 仓库 `assets/tasks/`。

```bash
# 单个任务（默认取首个）
pipeline-maxhit -pipeline assets/resource/pipeline -task assets/tasks/ItemTransfer.json

# 指定任务名
pipeline-maxhit -pipeline assets/resource/pipeline -task assets/tasks/AutoCollect.json -task-name AutoCollect

# 逗号分隔多个任务
pipeline-maxhit -pipeline assets/resource/pipeline -task assets/tasks/ItemTransfer.json -task-name AutoCollect,SellProduct

# 运行文件中全部任务
pipeline-maxhit -pipeline assets/resource/pipeline -task assets/tasks/ItemTransfer.json -task-name all

# 运行目录下全部任务文件
pipeline-maxhit -pipeline assets/resource/pipeline -task assets/tasks -all-tasks
```

进度输出到 stderr，结果输出到 stdout。

### 列出任务

```bash
pipeline-maxhit -task assets/tasks/AutoCollect.json -list-tasks
```

### 参数

| 参数 | 必填 | 说明 |
| ------ | ------ | ------ |
| `-pipeline` | 是 | Pipeline JSON 目录路径 |
| `-entry` | 否* | 入口节点名（覆盖 `-task` 的 entry） |
| `-task` | 否* | 任务接口 JSON 文件或目录路径 |
| `-task-name` | 否 | 任务名，逗号分隔多个，`all` 运行全部 |
| `-all-tasks` | 否 | 运行目录中所有任务文件中的所有任务 |
| `-defaults` | 否 | `default_pipeline.json` 路径 |
| `-list-tasks` | 否 | 列出任务后退出 |

\* `-entry` 或 `-task` 至少提供一个。

## 输出

```text
Pipeline Max-Exec Analysis
==========================

节点总数: 2005  可达: 277  零执行: 0

⚠  SCC with unbounded max_hit: NodeA, NodeB, NodeC

节点                           MaxHit      MaxExec   来源
──────────────────────────────────────────────────────────
WaitBlackScreen               UINT_MAX   1000000000  JumpBack
ClickContinue                 UINT_MAX   1000000000  JumpBack
EnterGame                     UINT_MAX            1  Normal(from OpenGame)
OpenGame                      UINT_MAX            1  Entry
```

### 结果解读

**先看 SCC 警告**（如果有）：这些是确定性的死循环，需要立即修复——给回环中的至少一个节点加上 `max_hit` 限制。

**再看高 MaxExec 节点**：`JumpBack` 来源且 MaxExec = 1e9 的节点，回跳路径没有上限，考虑加 `max_hit`。

**最后看零执行节点**：可达但 MaxExec = 0，可能是被 `DirectHit` 阻塞或路径配置有误。

### 列说明

| 列 | 说明 |
| ---- | ------ |
| `节点` | 节点名 |
| `MaxHit` | max_hit 配置（`UINT_MAX` = 无限制） |
| `MaxExec` | 理论最大执行次数，1e9 表示无上限（可能死循环），0 见来源列原因 |
| `来源` | `Entry` / `Normal(from X)` / `JumpBack` / `Mixed(JumpBack+Normal)` / `Zero` / `Unreachable` / `Blocked(by X)` |

## 局限

- **静态分析**：只检测结构上的潜在死循环，不模拟运行时识别结果。实际运行可能因识别条件变化而终止
- **SCC 仅看普通边**：纯 jump_back 形成的回环不会触发 SCC 警告（但会在 MaxExec 中体现为高值）
- 修改图结构的选项（`next` / `recognition` / `action`）**按组合枚举**取 max，不修改图结构的选项（`enabled` / `max_hit` 等）用**乐观并集**，互斥选项可能导致高估
- 锚点引用为流不敏感近似（不跟踪执行顺序）
- 节点的 `Custom` action（如 SubTask）不展开为子图边，仅靠 Custom action 引用的子流水线节点不可达

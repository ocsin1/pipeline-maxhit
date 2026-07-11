# pipeline-maxhit

计算 MAA Framework Pipeline 中每个节点在一个任务中的**理论最大执行次数**。

## 原理

基于 MAA Framework 的任务流水线协议，将 Pipeline 建模为带容量约束的有向图，通过 Dinic 最大流算法计算每个节点在任意合法执行路径中的最大识别成功+动作完成次数。

算法考虑的约束：

- `max_hit`：节点容量上限
- `next` / `on_error`：有向边
- `jump_back`：回跳边（创建子程序语义，执行完后返回父节点）
- `DirectHit` 识别类型：一定命中，在 next 列表中阻塞后续节点
- `StopTask` 动作类型：终止任务链
- 锚点（`anchor`）：流不敏感上近似展开
- 任务选项覆盖（`option` / `pipeline_override`）：乐观并集

## 构建

```bash
cd tools/pipeline-maxhit
go build -o pipeline-maxhit.exe .
```

依赖：Go 1.22+，无外部依赖。

## 用法

### 指定入口节点

```bash
pipeline-maxhit \
  -pipeline assets/resource/pipeline \
  -entry OpenGame \
  -defaults assets/resource/default_pipeline.json
```

### 从任务定义读取入口

```bash
pipeline-maxhit \
  -pipeline assets/resource/pipeline \
  -task assets/tasks/AutoCollect.json \
  -defaults assets/resource/default_pipeline.json
```

默认使用任务文件中的第一个任务。指定任务名：

```bash
pipeline-maxhit \
  -pipeline assets/resource/pipeline \
  -task assets/tasks/AutoCollect.json \
  -task-name AutoCollect \
  -defaults assets/resource/default_pipeline.json
```

`-entry` 会覆盖 `-task` 中的入口节点。

### 列出任务文件中的所有任务

```bash
pipeline-maxhit -task assets/tasks/AutoCollect.json -list-tasks
```

### 完整参数

| 参数 | 必填 | 说明 |
|------|------|------|
| `-pipeline` | 是 | Pipeline JSON 目录路径 |
| `-entry` | 否* | 入口节点名（覆盖 `-task` 的入口） |
| `-task` | 否* | 任务接口 JSON 文件路径 |
| `-task-name` | 否 | 任务名（默认使用文件中第一个任务） |
| `-defaults` | 否 | `default_pipeline.json` 路径 |
| `-list-tasks` | 否 | 列出任务后退出 |

\* `-entry` 或 `-task` 至少提供一个。

## 输出示例

```
Pipeline Max-Exec Analysis
==========================

Total nodes: 2005
Reachable:   16
Zero-exec:   1

⚠  SCC with unbounded max_hit: NodeA, NodeB, NodeC

Node                          MaxHit      MaxExec   Source
──────────────────────────────────────────────────────────
WaitBlackScreen               UINT_MAX   1000000000 JumpBack
ClickContinue                 UINT_MAX   1000000000 JumpBack
EnterGame                     UINT_MAX            1 Normal(from OpenGame)
OpenGame                      UINT_MAX            1 Entry
CheckIn                       UINT_MAX   1000000000 JumpBack
...
```

### 输出列说明

| 列 | 说明 |
|----|------|
| `Node` | 节点名 |
| `MaxHit` | 节点的 max_hit 配置（UINT_MAX 表示无限制） |
| `MaxExec` | 理论最大执行次数（0 表示不可达或被阻塞） |
| `Source` | 执行来源：`Entry`、`Normal(from X)`、`JumpBack`、`Zero`、`Unreachable`、`Blocked(by X)` |

### SCC 警告

当多个节点通过普通边形成强连通分量且全部 max_hit 无限制时，会输出警告。这意味着理论执行次数可能无界（受限于 UINT32_MAX 代理值 1e9）。

## 注意事项

- **理论最大值**表示在所有合法执行路径中的上界，实际运行中可能达不到
- 任务选项取**乐观并集**——任何选项组合中可达的节点/边都会纳入计算，可能导致略微高估
- 锚点引用采用**流不敏感**近似（不考虑执行顺序）
- `max_hit = UINT32_MAX` 在流网络中用 `1,000,000,000` 代理，避免整数溢出

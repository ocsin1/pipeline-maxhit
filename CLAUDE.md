# CLAUDE.md

纯 vibe 项目 — 全部代码由 Claude 生成，无手写。

## 构建与测试

```bash
go build -o install/pipeline-maxhit.exe .
go test -v ./...
```

## 项目规则

- **提交信息**：中文
- **提交粒度**：原子化，每个逻辑变更单独提交
- **代码风格**：卫语句（guard clauses）优先，减少嵌套
- **模块路径**：`github.com/ocsin1/pipeline-maxhit`
- **Go 版本**：1.25+
- **测试数据**：`d:/maa/MaaEnd/assets/` 下的 MaaEnd 仓库
- **构建产物**：放 `install/`，已 gitignore
- **文档**：改代码时同步更新 `README.md`
- **变更后**：用 MaaEnd 测试数据跑一遍验证（`-pipeline d:/maa/MaaEnd/assets/resource/pipeline`）

## MaaEnd 测试数据路径

```
pipeline 目录:   d:/maa/MaaEnd/assets/resource/pipeline
defaults:        d:/maa/MaaEnd/assets/resource/default_pipeline.json
任务接口:        d:/maa/MaaEnd/assets/tasks/
MaaFW 源码:      d:/maa/maaFW/
```

## 常用命令

```bash
# 单个入口
./install/pipeline-maxhit.exe -pipeline d:/maa/MaaEnd/assets/resource/pipeline -entry OpenGame

# 单个任务
./install/pipeline-maxhit.exe -pipeline d:/maa/MaaEnd/assets/resource/pipeline -task d:/maa/MaaEnd/assets/tasks/ItemTransfer.json

# 全部任务
./install/pipeline-maxhit.exe -pipeline d:/maa/MaaEnd/assets/resource/pipeline -task d:/maa/MaaEnd/assets/tasks/ItemTransfer.json -task-name all

# 列出任务
./install/pipeline-maxhit.exe -task d:/maa/MaaEnd/assets/tasks/AutoCollect.json -list-tasks
```

## 算法关键点

- 执行模型参考 `d:/maa/maaFW/source/MaaFramework/Task/PipelineTask.cpp`
- `run_next` 命中即返回，每次调用最多命中一个节点
- DirectHit（无 inverse）一定命中，在 next 列表中阻塞后续节点
- jump_back 创建"子程序"语义，通过 jumpback_stack 返回
- 锚点流不敏感展开（安全上近似）
- 选项覆盖：修改图结构的枚举组合，其余的乐观并集
- 流网络：Dinic 最大流，节点拆分做容量约束，jb 供应边 + T 溢流边

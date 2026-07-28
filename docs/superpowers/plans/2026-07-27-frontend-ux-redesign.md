# AgentScope 前端 UX/UI 重构方案与实施计划

**状态：** 首轮核心实现完成，待持续迭代  
**日期：** 2026-07-27  
**范围：** `web/` React 控制台；不改变 Go、Python SDK、PostgreSQL、ClickHouse 或 Redis 的业务语义。  
**目标读者：** 实现者、评审者，以及需要用 AgentScope 完成运行排障、质量评测和版本决策的 Agent 工程师。

---

## 1. 背景与问题定义

AgentScope 已从 MVP 演进为覆盖 Agent 注册、运行时状态、OTLP Trace、实时 SSE、在线评测、人工标注、数据集、实验对比、Judge 校准、采集健康与治理的 AgentOps 平台。功能面已经足够丰富，但前端仍停留在“按后端模块堆叠 Card 与 Table”的阶段，导致：

1. 用户看得到很多数据，却无法快速判断“当前最需要处理什么”；
2. 项目概览承担了数据集、实验、Token、统计等过多职责；
3. Trace、实验和评测详情页纵向堆叠，长页面中缺少稳定的主线；
4. 列表页在窄屏下容易压缩、截断或失去关键列；
5. 在线运行、人工回流和离线回归在产品上是闭环，在导航和页面上却是分散的管理项；
6. 登录页、主应用、局部组件使用了不同的视觉语言，难以形成可识别的产品气质。

本次重构不是换一套皮肤，而是将平台重组为一个面向日常决策的 **AgentOps 控制台**：

> 在最短路径内回答：哪个 Agent/Run 有问题、问题发生在何处、是否需要人工处理、候选版本能否发布。

---

## 2. 现状审计依据

### 2.1 结构性证据

截至 2026-07-27 的代码审计结果：

| 观察项 | 现状 | 影响 |
|---|---:|---|
| 非测试 TSX 文件 | 38 | 页面和组件增长较快，缺少统一框架约束 |
| AntD `Table` | 32 | 数据密集型页面占比高 |
| 明确配置横向滚动的 Table | 约 9 | 约 23 张表在窄屏或多列数据下缺少统一兜底 |
| 行内 `style={{...}}` | 327 | 设计调整需跨页面修改，视觉一致性不可控 |
| 颜色字面量 | 104 次 / 29 种 | 状态色、边框、文字色未被 token 化 |
| 自定义 `Badge`/`Button`/`Input` | 基本未被业务页面使用 | AntD 与自定义组件层并存，未形成设计系统 |

### 2.2 当前页面与导航问题

现有左侧导航已按“在线观测 / 在线评测 / 离线评测”做了初步分组，但核心用户对象没有一等入口：

- Dataset 仅嵌在项目概览；
- Experiment 列表与创建器仅嵌在项目概览；
- Active Runs 有路由但不是主导航项；
- Comparison、Run Detail、Experiment Detail 主要依赖深链进入；
- 详情页缺乏可见的项目上下文、面包屑与“返回上一级”的稳定路径。

### 2.3 现有测试的覆盖边界

现有 Playwright 验收覆盖 Agent、Run Detail、Experiment、Calibration、Comparison、Ingest 等核心页面，并在 1440×900 与 390×844 下校验文档整体没有横向溢出。

该验收仍不足以保证：

- 每个 Table 容器内部可以横向滚动而非裁切；
- Project Home、Dataset、Sampling Rule、Annotation Queue、Metric Config、Rubric 等页面在移动端可用；
- 长 Prompt、错误详情、Tool 参数、评测理由有可读的展开路径；
- 空数据、权限不足、部分接口失败时用户知道下一步怎么做。

---

## 3. 重构目标与非目标

### 3.1 目标

1. 将首页从“功能堆叠页”改为“需要关注的运行与质量工作台”；
2. 建立项目级信息架构，让用户可从任何详情页理解自己所处项目和返回路径；
3. 统一视觉 token、组件、间距、状态色和表格策略；
4. 让 Run Detail 与 Experiment Detail 先表达结论，再提供证据；
5. 将“线上抽样 → 自动评测 → 人工标注 → Gold → Dataset → 离线实验”的闭环显式呈现；
6. 桌面与移动端均能完成核心任务，长数据不丢失；
7. 保持现有 API、鉴权、SSE 幂等、轮询降级和多租户边界不变。

### 3.2 非目标

- 不重写后端领域模型或存储边界；
- 不新增营销 Landing Page；
- 不将完整 Prompt、Token、API Key 或敏感 Snapshot 内容暴露给前端；
- 不以图表装饰替代真实诊断信息；
- 不在首轮重构中引入新的大型图表库；
- 不为了视觉效果删除现有可审计的 Trace、Judge、Gold、Snapshot 证据。

---

## 4. 产品与视觉方向

### 4.1 产品定位

**产品：** AgentScope AgentOps 控制台。  
**核心用户：** 开发、调试、评测和发布 Agent 的工程师。  
**单一核心任务：** 从异常运行或版本变化出发，迅速定位原因并做出处理/发布决策。

### 4.2 设计原则

1. **Signal first：** 首屏优先展示失败、低分、待处理、回归与实时状态，而非平铺所有数字。
2. **结论先于证据：** 实验页先给出是否通过发布门禁，Run 页先给出发生了什么，再展示原始 Trace。
3. **高密度但不拥挤：** 允许专业信息密度，但必须通过分组、层级、固定 Inspector 与渐进披露降低负担。
4. **实体优先于接口：** 用户管理 Agent、Run、Dataset、Experiment、Review，而不是“某个 API 配置”。
5. **数据不丢失：** 任何被截断的文本都必须能查看完整内容；移动端不能以裁切代替响应式设计。
6. **状态色有语义：** 颜色只用于状态和风险，不作为随意装饰。
7. **保持可追溯：** 所有摘要均可回到 Run、Trace、Snapshot、评测理由或 Judge Vote。

### 4.3 视觉语言：Signal-first Console

主应用采用克制的“运行控制台”风格，而不是登录页的渐变营销视觉：

| Token 类别 | 建议 | 用途 |
|---|---|---|
| `canvas` | `#F6F8FC` | 应用背景 |
| `surface` | `#FFFFFF` | 内容承载面 |
| `ink` | `#172033` | 主要文字、深色导航 |
| `muted` | `#667085` | 次级文本、说明 |
| `action` | `#3E5CF6` | 主操作、可点击对象 |
| `live` | `#099268` | 在线、成功、健康 |
| `attention` | `#D68A00` | 风险、待确认 |
| `danger` | `#D64545` | 失败、阻断、Veto |
| `trace` | `#6554C0` | Trace/时间线辅助识别色 |

**签名组件：Run Signal Strip**

每个 Run/Experiment 详情页顶部提供横向状态带，统一承载：

```text
状态 · Agent/版本 · 实时连接 · 耗时 · Token · 成本 · 评测结果 · 风险/待标注
```

这是一条真实的工作流摘要，不是装饰性渐变。

### 4.4 字体与密度

- 中文正文：系统 UI 字体栈（PingFang SC / Microsoft YaHei / Noto Sans SC 回退）；
- 数字、ID、Trace、模型名：等宽字体栈；
- 普通文本 14px，辅助文本 12px，数据标题 12px，页面标题 24px；
- 提供 `comfortable` 与 `compact` 两种表格/列表密度，默认使用 comfortable；
- ID 默认显示短 ID 与复制按钮，完整 ID 放入 Tooltip 或 Inspector，不与业务名称争夺视觉层级。

### 4.5 视觉体验升级规范

本次重构必须同时改善“看起来是否专业、是否有秩序、是否能一眼区分风险”的感受。视觉优化不以增加渐变、阴影或动画数量为目标，而以降低视觉噪声、突出运行信号和建立一致品牌识别为目标。

#### 4.5.1 统一应用骨架

```text
深色项目导航（稳定定位）
+ 浅色画布（长时间阅读）
+ 低对比背景层次（区分区域，不制造卡片墙）
+ 单一主操作色（明确可执行动作）
+ 严格的风险色（只表达运行/评测状态）
```

- 侧边栏改为低饱和深墨蓝背景，品牌字标、当前项目和在线状态成为导航锚点；
- 顶栏不再只是白色工具条，应承载 Breadcrumb、项目环境、全局搜索/快捷创建和实时连接状态；
- 主画布采用浅灰蓝背景，Section 使用“无边框大区块 + 少量重点 Surface”的层级，减少每个数据点都包一层 Card 的碎片感；
- Card 默认取消明显阴影，采用细边框、微弱层次和一致圆角；仅对告警、当前选中对象、关键决策区使用提升层级；
- 登录页保留品牌氛围，但渐变、圆角、文字色和主按钮需与主应用 token 复用，避免“两个产品”的感觉。

#### 4.5.2 页面视觉层级

每个页面统一采用以下从上到下的视觉节奏：

```text
1. Context：Breadcrumb / 项目 / 实体来源
2. Decision：状态、结论、风险、主操作
3. Evidence：时间线、指标矩阵、表格、评测原因
4. Raw data：JSON、完整内容、审计记录
```

- 页面标题不低于 `Title level=3`，禁止在主页面使用 `level=4/5` 作为第一标题；
- PageHeader 使用固定的标题、描述、状态和操作布局，避免各页各自排列；
- 数字统计卡必须有明确分组标题，例如“运行健康”“质量风险”“成本与延迟”，不允许只按 API 字段堆成网格；
- 主操作一页最多一个主色按钮，次级操作使用文字按钮或溢出菜单；
- Danger 操作不与普通操作并列抢占视觉焦点。

#### 4.5.3 数据视觉化原则

- 运行状态采用“状态 + 文本 + 时间”组合，不只依赖 Tag 颜色；
- 指标变化统一使用 `MetricDelta`：当前值、相对变化、方向箭头、比较基准、风险解释；
- Experiment 对比优先使用矩阵/对照条，而不是两排独立 Statistic Card；
- Run Timeline 使用语义类型图标、耗时轨道和异常标记，突出关键路径、慢 Span、失败 Tool；
- Ingest 和配额页的趋势只使用细小 sparkline、阈值线和异常标记，避免引入装饰性大图；
- 缺失数据以灰色“未采集/不适用”呈现，禁止显示为零值或危险红色。

#### 4.5.4 组件视觉规格

| 组件 | 视觉要求 |
|---|---|
| `PageHeader` | 标题 24px、辅助说明、状态 Chip、右侧单一主操作；桌面与移动端保持相同信息优先级 |
| `RunSignalStrip` | 一条横向信息带，区分状态、延迟、成本、评分和风险；允许在移动端折叠为两行 |
| `Card/Section` | 默认 12px 圆角、细边框、24px 内边距；不使用重阴影；告警区使用浅色状态底纹 |
| `DataTable` | 表头弱底色、hover 行高亮、固定操作列、长内容抽屉查看、密度可切换 |
| `StatusBadge` | 圆点 + 文本 + 必要时图标；颜色必须来自状态 token |
| `InspectorDrawer` | 深层证据使用单独 Drawer/侧栏，代码与 JSON 使用等宽字体和复制操作 |
| `EmptyState` | 轻插画或线性图标、明确原因、单一下一步 CTA；不使用泛化“暂无数据” |
| `Modal` | 表单 Modal 只用于短任务；复杂创建流程使用专页或 Step Drawer |

#### 4.5.5 动效与实时感

动效只服务于状态变化和空间理解：

- SSE 事件到达时，仅对受影响 Run 行或 Signal Strip 做一次 150–200ms 的低幅度高亮，不刷新整块页面；
- Agent 在线状态使用静态圆点/低频呼吸，不使用持续闪烁；
- Drawer、Tab、展开行使用短位移动画，帮助用户理解内容来自何处；
- 页面加载采用 Skeleton 的渐隐过渡，避免长时间 Spin；
- 为 `prefers-reduced-motion` 提供无动画模式；
- 禁止无业务含义的循环渐变、粒子背景和大面积动态光效。

#### 4.5.6 深色模式策略

首轮以高质量浅色主界面为目标；深色模式不应与重构同时仓促上线。所有 token 必须语义化，确保未来可通过 token 映射支持 dark theme，而不是在业务页面硬编码浅色值。

#### 4.5.7 视觉验收

每个重构页面需同时满足：

- 截图中页面主标题、当前实体、最关键状态和下一步动作在首屏可辨；
- 页面不出现无语义的 Card 墙、连续同权重数字或颜色滥用；
- 同一状态在 Agent、Run、Experiment、Annotation 中使用一致名称、图标与颜色；
- 登录页与主应用在品牌色、圆角、表单和按钮上具有连续性；
- 1440×900 和 390×844 截图都能作为项目演示材料，无需解释“移动端先不看”。

---

## 5. 目标信息架构

### 5.1 一级导航

```text
项目概览

运行中心
├─ 实时运行
├─ Agent
├─ Runs / Trace
└─ 采集健康

质量改进
├─ 数据集
├─ 实验
├─ 实验对比
└─ 标注队列

评测配置
├─ 指标与 Evaluator
├─ Rubric 模板
├─ Sampling Rule
└─ Judge 校准

项目设置
├─ Project Token
└─ 配额与接入信息
```

### 5.2 路由调整原则

- 所有项目资产优先采用 `/projects/:projectId/...` 路由；
- Run / Experiment / Comparison 详情页保留旧深链兼容，但页面必须恢复项目上下文；
- 新增独立 Dataset、Experiment 列表页，Project Home 不再长期承载完整管理器；
- 详情页必须提供：项目名、实体名、返回列表、上一层面包屑；
- 顶部项目切换器保持，但不能替代导航结构。

### 5.3 建议路由地图

```text
/projects/:projectId                         项目概览
/projects/:projectId/live                    实时运行
/projects/:projectId/agents                  Agent 列表
/projects/:projectId/agents/:agentId         Agent 详情
/projects/:projectId/runs                    Run 列表
/projects/:projectId/runs/:runId             Run 详情（替代全局详情主入口）
/projects/:projectId/datasets                Dataset 列表与详情
/projects/:projectId/experiments             Experiment 列表
/projects/:projectId/experiments/:id         Experiment 详情
/projects/:projectId/comparisons/:id         Comparison 详情
/projects/:projectId/reviews                 Annotation Queue
/projects/:projectId/evaluation/*            指标、Rubric、Sampling、Calibration
/projects/:projectId/ingest                  采集健康
/projects/:projectId/settings                Token、配额、接入信息
```

旧路由在第一期只做 `Navigate` 或上下文补全，不要求一次性删除。

---

## 6. 页面重构方案

### 6.1 项目概览：从功能聚合改为行动工作台

#### 当前问题

Project Home 同时混入 Token 创建、Dataset、Case、Experiment 创建和 Experiment 表格；页面第一眼无法回答“当前项目是否健康、用户接下来该做什么”。

#### 目标布局

```text
[项目名 / 环境 / 实时连接 / 创建 Run 接入 Token / 创建实验]

[需要关注]
失败 Run | 低分 Run | 待标注 | 回归风险 | 采集异常

[实时运行]                         [最近需要处理]
Active Runs / 最新事件             失败、低分、待审核列表

[质量进展]
最近实验 / 版本结论 / 数据集覆盖 / Judge 校准状态
```

#### 调整

- Dataset、Experiment 迁移到独立页面；首页只展示摘要和跳转；
- Token 创建迁移到项目设置或全局操作菜单；
- 首屏至少有一个“失败 Run / 待标注”可点击行动入口；
- 不使用 6 张同权重 Statistic Card 作为第一屏唯一内容。

### 6.2 Run Detail：以执行叙事替代 Card 瀑布

#### 当前问题

当前页面同时有大量统计 Card、多个表格、时间线、Span Tree、评分、Snapshot 和标注操作，内容完整但主线不清晰。

#### 目标布局

```text
Breadcrumb
Run Signal Strip

左：执行主线（Timeline / Span Tree 可切换）
中：选中 Span 的上下文与输入输出
右：Inspector（错误、Token、耗时、评测、动作）

Tabs：评测 | 运行配置 | Snapshot | 原始事件
```

#### 调整

- 首屏只保留关键运行摘要，不重复展示每个数据点；
- 语义事件采用统一时间线与 Inspector，LLM/Tool/Retrieval 不再先铺 5 张表；
- 任意长 Prompt、参数、输出、评分原因通过 Drawer/Inspector 查看；
- “加入标注队列”“查看快照”“复制 Trace ID”等动作收拢到 Action Bar；
- 失败 Run 自动突出错误根因、失败 Tool 和相应评测维度。

### 6.3 Agent 与实时运行：面向当前运行状态

#### 目标

- Agent List 首屏展示在线实例、活跃 Run、最近错误、平均延迟和最近心跳；
- Active Runs 成为正式导航页，不隐藏在项目概览；
- Agent Detail 以“实例健康 / 当前 Run / 最近失败 / 版本与能力”分区；
- 实时连接状态不应只是小图标，要能解释 `实时 / 重连中 / 轮询降级 / 不可用` 对用户意味着什么；
- 点击失败实例或 Run 直接进入对应 Trace，不要求用户在多个表格中寻找 ID。

### 6.4 在线评测与人工标注：显式闭环

#### 目标布局

```text
Sampling Rule
  → 被抽样的线上 Run
  → 自动评测分数
  → 待人工审核
  → Gold
  → 提升为 Dataset Case
```

#### 调整

- 在 Sampling Rule 页面显示最近命中量、低分率、入队量；
- Annotation Queue 默认按“需要处理的风险”排序，而不是单纯表格时间顺序；
- 标注项侧边预览 Run 摘要、Trace、评测分数、失败类别和历史操作；
- Gold 编辑区显示 Judge 分数与人工分数的差值；
- “提升为 Dataset”前明确说明隐私审核、修正输出和预期工具路径是否完整；
- Calibration 页面以“Judge 是否可信、哪些维度偏差最大、建议下一步”作为首屏结论。

### 6.5 Dataset、Experiment 与 Comparison：决策优先

#### Dataset

- 独立列表与详情页；
- 显示 Case 数、来源（手工/线上标注）、最近使用实验、覆盖的失败类别；
- Case 表支持筛选、全文查看、来源 Run 回链和批量选择。

#### Experiment Detail

首屏必须先回答：

```text
Candidate 是否可发布？
质量、成本、P95 延迟、错误率变化如何？
结论是否具有统计显著性？
是否触发安全 Veto？
```

其后按证据层级展开：指标矩阵 → 分维度详情 → Case 级回归 → Judge 审计 → Snapshot Diff。

#### Comparison

- 将 Baseline / Candidate 作为左右对照的固定语义，而不是两组独立 Description Card；
- 使用 delta、箭头与风险色表达变化；
- Case 级表优先列出回归、Veto、显著差异，正常样本可折叠；
- Snapshot Diff 直接解释模型、Prompt、工具、代码或环境差异，而非只展示 JSON 树。

### 6.6 采集健康与治理：从计数器墙变成诊断工作台

#### 目标布局

```text
[健康状态 / 最近 24h 趋势 / 受影响 Agent]

[异常与影响]
错误率、拒绝、丢弃、背压、接近配额、异常检测

[诊断证据]
最近异常事件、错误类型、入口 Run / Agent / 配额配置
```

#### 调整

- 合并同类 Statistic，避免十余张同权重卡片；
- 引入趋势 sparkline 或简洁时序表，不新增大型图表依赖；
- 将“未知”与“0”视觉上严格区分；
- 配额编辑从大段 Description + Form 混合结构，改为明确的只读摘要与独立编辑 Drawer；
- 将异常、背压、限流与可追踪 Agent/Run 关联。

### 6.7 登录与项目列表

- 登录页保留简洁性，但与主应用共享品牌 token；
- 减少大面积无语义渐变，增加“安全、项目隔离、OTLP 接入”简短说明；
- Project List 不应只是一张管理表，应支持最近访问、环境/状态、待处理数量和进入工作台。

---

## 7. 设计系统与组件方案

### 7.1 Ant Design Theme

在 `main.tsx` 统一扩展 AntD token，禁止业务页面重复设置：

- `colorPrimary`、`colorInfo`、`colorSuccess`、`colorWarning`、`colorError`；
- `borderRadius`、`fontSize`、`controlHeight`；
- Card、Table、Menu、Tag、Button、Drawer、Modal 的组件级 token；
- 浅色主应用与深色导航的明确边界。

### 7.2 必须新增/重构的基础组件

| 组件 | 职责 |
|---|---|
| `PageHeader` | Breadcrumb、标题、说明、主操作、上下文状态 |
| `PageSection` | 标题、说明、操作区、可选密度和边界 |
| `RunSignalStrip` | Run/Experiment 的状态、核心指标、风险、动作 |
| `StatusBadge` | 状态、风险、实时连接、评测通过语义 |
| `DataTable` | 横向滚动、列优先级、长文本展开、密度、空状态 |
| `EntityLink` | Agent / Run / Dataset / Experiment 的统一跳转与短 ID |
| `InspectorDrawer` | 查看完整 JSON、Prompt 摘要、参数、评分理由、错误堆栈 |
| `EmptyState` | 明确无数据原因与下一步 CTA |
| `ErrorState` | 可重试的请求失败、部分失败、权限不足 |
| `MetricDelta` | Baseline/Candidate 差异、方向、风险解释 |

### 7.3 样式治理规则

- 新业务代码不得新增裸露十六进制颜色，必须使用 token；
- 页面布局不得新增 `style={{...}}`，优先使用 class、组件 props 或 token；
- 所有 Table 统一经由 `DataTable` 或遵守等价规范；
- 统一页面间距、Section 间距、Card 内边距、标题等级；
- 旧 `Badge`、`Button`、`Input` 组件要么纳入统一系统，要么删除，避免双轨维护。

---

## 8. 响应式、可访问性与内容完整性

### 8.1 响应式策略

| 宽度 | 行为 |
|---|---|
| `>= 1280px` | 分栏、Inspector、宽表全列展示 |
| `768–1279px` | 收缩次级指标、表格可横滚、双栏降为单/双栏混合 |
| `< 768px` | Drawer 导航、单列布局、摘要优先、次级列隐藏、表格横滚或改为卡片列表 |

### 8.2 表格规范

每张数据表必须定义：

1. `scroll.x` 或移动端卡片替代方案；
2. 列优先级（主列、次列、可隐藏列）；
3. 长文本查看入口；
4. 空状态 CTA；
5. 行点击和操作列行为；
6. 关键 ID 的复制/回链；
7. `aria-label`、键盘焦点与 Tooltip 语义。

### 8.3 内容完整性规范

- 长 Prompt、Tool 参数、Final Answer、Judge Reason 不允许只靠 `ellipsis`；
- 被截断字段必须能用 Tooltip、Drawer、展开行或详情页查看；
- `0`、`未知`、`未采集`、`不适用` 使用不同文案与样式；
- 评测分数必须同时表达分数、是否通过、原因、数据来源；
- 原始 JSON 仅作为证据层，不作为默认阅读界面。

### 8.4 可访问性最低要求

- 所有图标按钮提供可读 label；
- 颜色不是唯一状态表达；
- 焦点样式可见；
- Modal/Drawer 首焦点和关闭行为可预测；
- SSE 状态变化通过 `aria-live` 或可见文字说明；
- 支持 `prefers-reduced-motion`，不以频繁动画表现实时性。

---

## 9. 前端架构与数据状态调整

### 9.1 页面职责拆分

当前重页面应从“大型 feature 文件”拆成领域 section：

```text
features/project-overview/
  ProjectAttentionPanel
  ActiveRunsPanel
  QualityProgressPanel
  ProjectQuickActions

features/run-detail/
  RunSignalStrip
  RunTimelinePanel
  TraceInspector
  RunEvaluationPanel
  RunEvidenceTabs

features/experiment-detail/
  ReleaseVerdict
  MetricMatrix
  CaseRegressionTable
  JudgeAuditPanel
  SnapshotDiffPanel
```

页面文件负责路由、数据编排和状态；Section 负责展示；基础组件负责交互一致性。

### 9.2 数据加载规则

- 首屏只请求回答核心问题所需的数据；
- 原始 Trace、长 JSON、Judge 投票和 Case 展开详情按需加载；
- 首屏加载使用 Skeleton，而不是孤立 `Spin`；
- 分区请求失败不得隐藏其他已成功数据；
- SSE 只更新必要的缓存实体，页面仍以 React Query 的权威请求为主；
- 在 SSE 降级为轮询时，页面必须明确显示“数据可能延迟”。

### 9.3 新接口原则

优先用现有接口完成重构。只有出现以下情况才新增后端 API：

- 首页需要聚合多个大列表才能得到简单的“待处理数量”；
- 单页首屏出现不可接受的 N+1 请求；
- 现有详情 API 不能按需加载大字段；
- 需要查询跨实体的“需要关注”工作队列。

新增接口必须保持项目级鉴权、脱敏和已有存储边界。

---

## 10. 实施阶段

### Phase 0：基线与视觉基础

- [ ] 固化现有核心页面桌面/移动端截图；
- [ ] 为所有当前路由补充页面清单；
- [ ] 在 `ConfigProvider` 中建立主题 token；
- [ ] 建立 `PageHeader`、`PageSection`、`StatusBadge`、`EmptyState`、`ErrorState`；
- [ ] 建立 `DataTable`，先解决横向滚动和长文本查看；
- [ ] 清理或整合未被使用的旧 UI 原子组件。

**验收：** 不改变业务功能；已迁移页面不再新增裸颜色与行内布局样式；所有迁移表格在 390px 视口下可读取完整数据。

### Phase 1：信息架构和应用壳

- [ ] 重构侧边导航与顶部项目上下文；
- [ ] 新增 Dataset、Experiment、Active Runs 的一级入口；
- [ ] 为 Run/Experiment/Comparison 详情补全 Breadcrumb 与项目返回路径；
- [ ] 把 Token 创建、Experiment 创建从 Project Home 中解耦为明确入口；
- [ ] 兼容旧路由。

**验收：** 从任一项目页可在不依赖浏览器后退的情况下进入 Run、Dataset、Experiment、Review 和设置；移动端导航可用。

### Phase 2：在线观测核心体验

- [ ] 重做 Project Overview 为行动工作台；
- [ ] 将 Active Runs 作为正式运行中心页；
- [ ] 重做 Agent List/Detail 的状态、实例和错误信息层级；
- [ ] 重做 Run Detail：Signal Strip、Timeline、Inspector、Evidence Tabs；
- [ ] 统一实时状态、SSE 重连与轮询降级提示。

**验收：** 对一个失败 Run，用户在三个点击以内能看到错误 Span、评测结果和“加入/查看标注队列”动作。

### Phase 3：评测与质量闭环

- [ ] 拆分 Dataset 页面和 Case 管理；
- [ ] 重做 Sampling Rule 与 Annotation Queue 的闭环引导；
- [ ] 增加 Gold 与 Judge 结果差异展示；
- [ ] 重做 Experiment Detail 的发布结论区；
- [ ] 重做 Comparison 的 delta、显著性、Veto 和 Case 回归视图；
- [ ] 重做 Calibration 的可信度与改进建议视图。

**验收：** 用户能从线上失败 Run 一路理解其抽样规则、自动评分、人工 Gold、Dataset 来源和后续实验结果。

### Phase 4：采集健康与项目设置

- [ ] 重做 Ingest Dashboard 的健康总览、趋势、异常和行动入口；
- [ ] 重做 Quota 编辑交互；
- [ ] 统一 Token、项目设置、接入说明；
- [ ] 优化 Login 和 Project List 的首次使用引导。

**验收：** 采集异常、背压、配额接近阈值都可跳转至受影响 Agent、Run 或对应配置。

### Phase 5：全量质量收口

- [ ] 清除遗留页面中的重复 token 和无效行内样式；
- [ ] 为所有路由补充加载、错误、空状态；
- [ ] 更新单元测试、交互测试和 E2E 截图；
- [ ] 完成桌面、平板、移动端视觉回归；
- [ ] 记录迁移风险、未完成项和后续优化。

---

## 11. 测试与验收计划

### 11.1 单元与组件测试

覆盖以下关键行为：

- `DataTable` 的横向滚动、列隐藏、长文本展开；
- `PageHeader` Breadcrumb、项目上下文和操作；
- SSE `live / reconnecting / fallback / closed` 状态展示；
- 评分、Veto、未知值、未采集值的显示语义；
- Annotation Item 从待处理到 Gold、提升 Dataset 的动作提示；
- Experiment 发布结论与显著性状态。

### 11.2 Playwright 页面矩阵

每个场景至少覆盖 1440×900 与 390×844：

| 场景 | 关键断言 |
|---|---|
| Project Overview | 待处理风险、实时运行、质量进展可见 |
| Agent / Active Runs | 实时状态与运行跳转可用 |
| Run Detail | Timeline、Inspector、评测、Snapshot 无裁切 |
| Dataset / Experiment | 创建、列表、详情、返回路径完整 |
| Comparison | 发布结论、指标 delta、Case 回归、Snapshot Diff 可读 |
| Sampling / Annotation | 评测回流、Gold、提升 Dataset 指引明确 |
| Ingest | 健康、背压、异常、配额均可查看和跳转 |
| 空数据 / 接口失败 | 明确说明和下一步 CTA |

### 11.3 视觉与响应式验收

- 页面和表格容器均不得出现未处理横向裁切；
- 不只检查 `documentElement.scrollWidth`，还要检查 `[data-testid="data-table"]` 等容器的可滚动性；
- 长文本有明确查看入口；
- 关键任务可仅用键盘完成；
- 所有状态色具备文本或图标补充；
- 不在 SSE 高频事件中造成页面整块闪烁或滚动位置跳动。

---

## 12. 风险与迁移策略

| 风险 | 缓解方式 |
|---|---|
| 视觉重构影响业务行为 | 先引入基础组件和主题，再逐页迁移；每页保持 API 不变 |
| 路由调整破坏深链 | 保留旧路由重定向与详情上下文补全 |
| 大型详情页拆分引入状态回归 | 先以现有 hooks 为输入，逐步抽离展示 Section |
| 移动端为适配而隐藏关键信息 | 明确定义主列、次列、展开入口，不以删除证据换取简洁 |
| Dashboard 新增聚合数据导致 N+1 | 优先前端缓存和并发请求；确有需要再设计项目级聚合 API |
| 过度视觉化掩盖工程信息 | 以状态、时间线、指标和证据为主；图表只用于趋势和比较 |

---

## 13. 完成定义

前端 UX/UI 重构完成不以“页面变好看”为判断，而必须同时满足：

1. 用户能明确在 3 次点击内从异常摘要进入对应 Run/Trace；
2. 用户能从失败 Run 看到在线评测、人工标注和 Dataset 回灌路径；
3. 用户能在 Experiment 页面先获得发布结论，再查看统计与 Case 级证据；
4. 所有关键列表在 390px 宽度下信息不丢失；
5. 页面有一致的标题、操作、空状态、错误状态与状态颜色语义；
6. 旧 E2E 继续通过，并新增覆盖所有主导航页的桌面/移动端验收；
7. API、鉴权、项目隔离、SSE 去重与轮询降级行为保持不变；
8. 不新增敏感信息泄露面，不在 UI 中展示 API Key、Project Token 或未脱敏的敏感 Prompt。

---

## 14. 首轮实施推荐顺序

若只能优先投入一轮改造，顺序应为：

```text
DataTable + Theme + AppShell
→ Project Overview
→ Runs / Run Detail
→ Dataset / Experiment / Comparison
→ Sampling / Annotation / Calibration
→ Ingest / Settings / Login
```

原因：前四项直接覆盖 AgentScope 最有价值、最适合演示与日常使用的链路：

```text
在线运行 → Trace 诊断 → 在线评测 → 人工回流 → 离线实验 → 发布判断
```



---

## 15. 2026-07-27 实施记录与验收证据

本轮已落地首轮可复用设计基础与核心用户路径：

- 建立语义化设计 token、统一 Ant Design Theme、响应式应用壳与项目级导航；
- 落地 `PageHeader`、`PageSection`、`DataTable`、`StatusBadge`、`RunSignalStrip`、`EmptyState`、`ErrorState`、`MetricDelta` 等共享组件；
- 重构登录页、项目工作台、Run 列表、实时运行、Agent 控制中心与 Run 诊断首屏；
- 将 Dataset、Experiment、接入与配额拆为一级路由，保留既有详情深链；
- 所有共享/遗留表格在窄屏下采用容器内滚动，避免文档级横向溢出；
- 保持现有 API、鉴权、项目隔离与 SSE/轮询 hook 契约不变。

本轮验证：

```text
npm run lint                                  通过
npm test -- --no-file-parallelism             22 files / 146 tests 通过
npm run build                                 通过
Playwright mock API UI smoke（1440×900）       通过，无 document overflow
Playwright mock API UI smoke（390×844）        通过，无 document overflow
```

移动端和桌面端截图分别保存于本地验收过程的 `/tmp/agentscope-project-home-mobile.png` 与
`/tmp/agentscope-project-home-desktop.png`，不纳入仓库。现有 Phase 4 E2E 所需的
`.external/phase4-agentops-result.json` 在本 worktree 不存在，因此未将该缺失的演示夹具视为产品回归；待真实 demo 产物生成后，按第 11.2 节运行全链路浏览器验收。

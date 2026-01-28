# tipg / pg-tikv Global Map (v0)

> 目标：产出一份“可执行的导航地图”（先方向、后细节）。先给出全局分层与主要路径的定位方式，后续任务再深入探索补证据与细节。  
> 范围：**全仓库**，但**重心在 Rust 核心服务**；`cloud-admin-portal/` 单独成块（横向/纵向都分开写）。

---

## 0.0 方法论（费曼学习法 + 科学实验）

> 规则：只写“能被代码证明的事实”。凡是解释里出现“应该/大概/可能”，立刻降级为 `UNKNOWN`，并记录“需要读哪里才能证伪/证实”。

### 0.0.1 费曼学习法：把系统讲清楚

- 每个模块/路径都回答：**输入是什么**、**输出是什么**、**状态在哪**、**失败如何收敛**、**不变量是什么**
- 解释过程中暴露的“概念空白”全部变成问题清单（而不是用猜测补洞）

### 0.0.2 科学实验：假设 → 证据 → 结论

- **假设来源**：README/设计文档/WORK 记录/issue
- **证据来源**：逐文件/逐函数读到的关键入口、关键分支条件、持久化 key、返回格式、错误路径（本地图目标是 module/function 粒度）
- **结论写入**：只有当证据能指向具体代码位置；否则保留 `UNKNOWN`

### 0.0.3 产物拆分：横向地图 + 纵向主路径

- **横向（Architecture Map）**：按层列入口与边界（main/pool/tls/protocol/sql/txn/storage/auth/extensions/obs/portal）
- **纵向（Primary Flows）**：按外部行为列端到端主路径（入口链路 + 状态归属 + 失败边界）
- **Priority Lenses**：把高优先级（SQL 正确性 / 多租户隔离与安全 / 性能与可观测性）映射到“节点”和“路径”

### 0.0.4 地图定位：高维导航地图/阅读索引（不是规格）

这份文档的定位是“高维导航地图/阅读索引”，不是“实现事实的代表性规格/兼容性结论”。

- 适用：
  - 帮 reviewer 快速定位入口与主路径（module/function 级别）
  - 建立共同词汇（模块边界、关键名词、主链路分流点）
  - 决定接下来读哪里（按路径与风险选择文件/函数）
- 不适用：
  - 用来证明功能支持范围、语义细节、兼容性结论已经成立
  - 用来证明安全边界/隔离已经成立（这些必须用代码与测试闭环收敛）

### 0.0.5 覆盖状态标签（module/function 粒度）

为了让地图在 module/function 粒度上更能代表实现，每个 Flow/模块都标注覆盖状态，并明确区分“事实”和“风险/假设”：

- `Verified`：已读到该 Flow/模块的关键入口函数与核心分支；可作为“导航事实”（但仍不是语义规格）
- `Partial`：已读到入口链路/关键分流点，但内部语义/边界条件未收敛；只能用来“找路”
- `Unknown`：尚未读到关键实现；本节仅为目录/命名线索，不应据此下结论

写作约定：
- Primary Flows：每条 Flow 先写 `Status: ...`，并分 `Facts`（能被代码证明） / `Risks/Assumptions`（待验证/高风险点/仍可能被推翻的假设）
- Architecture Map：模块条目用 `(Verified/Partial/Unknown)` 内联标注；`Facts/Risks` 视需要逐步补充

## 0. 怎么用这份地图（建议读法）

1. 先读 **Primary Flows**：按你关心的优先级走通端到端链路（入口→状态→存储→返回/失败边界）。
2. 每走一条 Flow，就回到 **Architecture Map** 对照：确认“边界是否清晰、状态归属是否合理、不变量是否被守住”。
3. 用 **Priority Lenses**（SQL 正确性 / 多租户隔离与安全 / 性能与可观测性）给每个节点打标签并跑 checklist。

### 标签约定（后续会持续补全）

- `correctness`: SQL 语义正确性（尽量贴近 PostgreSQL）
- `tenancy`: 多租户隔离（keyspace routing + 任何持久化必须 tenant-isolated）
- `security`: 鉴权/授权/越权面（RBAC、extensions、portal、输入处理）
- `perf`: 性能热点（扫描/排序/Join/窗口/网络往返/序列化）
- `obs`: 可观测性（采样、统计、诊断表函数、portal 依赖）

---

## 0.4 路径索引（Key Paths，用于“快速找功能脉络”）

> 只给“路径骨架 + 关键文件入口”。每条路径都能从这里定位到对应模块；细节留到后续任务补。

- [Verified] Flow 1 连接/租户/认证：`src/main.rs` → `src/protocol/handler.rs` (Startup/auth + tenant routing) → `src/pool.rs` (keyspace client) → `src/storage/tikv_store.rs` + `src/auth/*`
- [Partial] Flow 2 Simple Query：`src/protocol/handler.rs` `SimpleQueryHandler::do_query()` → `src/sql/executor.rs` `Executor::execute()` → `src/sql/session.rs`（txn glue）→ `execute_statement_on_txn()`（分发到 DDL/DML/SELECT）
- [Partial] Flow 3 Extended Query：`src/protocol/handler.rs` `ExtendedQueryHandler::do_query()` → `substitute_parameters()` → `Executor::execute()`（本质仍走 Simple 执行链路）
- [Partial] Flow 4 事务/Savepoints：`src/sql/executor.rs`（语句级 autocommit glue）+ `src/sql/session.rs`（BEGIN/COMMIT/ROLLBACK）+ `src/txn/*`（savepoints task-local）
- [Partial] Flow 5 DDL：`src/sql/executor.rs` `execute_statement_on_txn()` → `src/sql/ddl.rs`/`src/sql/sequences.rs`/`src/sql/udt.rs`/`src/sql/executor_extensions.rs` → `src/storage/*`（schema/catalog keys）
- [Partial] Flow 6 DML：`src/sql/executor.rs` `execute_{insert,update,delete}` → `src/sql/executor_dml_ops.rs` + `src/sql/dml.rs` → `src/storage/*`（row/index keys）→ triggers（见 Flow 10）
- [Partial] Flow 7 SELECT：`src/sql/executor.rs` `execute_query()` → `src/sql/executor_select.rs` `execute_query_with_ctes()` → (no FROM→tableless / has JOIN→`src/sql/executor_join.rs` / single table→planner+scan) → `src/sql/expr.rs`/`src/sql/aggregate.rs`/`src/sql/window.rs`
- [Verified] Flow 8 COPY：`src/protocol/handler.rs` (COPY parse + CopyHandler) → `src/sql/executor.rs` `execute_copy_insert()` → `src/storage/*`
- [Partial] Flow 9 Extensions/http：SQL table function → `src/sql/executor_extensions.rs` → `src/extensions/http.rs`（出站请求 + 限制）→ rowset
- [Partial] Flow 10 Async AFTER triggers：`src/main.rs` `spawn_trigger_worker()` → `src/sql/trigger_worker.rs`（消费队列）← `src/sql/executor_dml_ops.rs`/`src/sql/triggers.rs`（生产事件）+ 诊断表：`src/sql/executor_join.rs`（`_pgtikv_sys_trigger_queue_stats/_pgtikv_sys_trigger_dlq`）
- [Verified] Flow 11 Observability：采集 `src/sql/executor.rs`（record_statement）+ `src/sql/session.rs`（record_commit）→ `src/observability.rs`（rolling window + samples）→ sys tables `src/sql/executor_join.rs`（`_pgtikv_sys_observability/_pgtikv_sys_query_samples`）→ portal（dashboard 轮询）
- [Partial] Portal（管理面）：`cloud-admin-portal/backend/app/api/*.py`（tenant CRUD + connect/session + obs/query）↔ `cloud-admin-portal/backend/app/services/{pd_client,pg_client}.py` ↔ `cloud-admin-portal/frontend/src/*`（X-Tenant-Session + polling）

---

## 1. Architecture Map（横向：层与子系统）

> 这一节回答：“模块边界是什么、入口在哪、状态在哪、依赖谁、必须守住哪些不变量”。

### 1.1 Rust 核心服务（`src/`）

**Entry / Bootstrap**
- `src/main.rs` (Verified): server 启动、TLS、监听 socket、每连接创建 `DynamicHandlerFactory`、触发后台 worker（`sql::trigger_worker`）
  - keyspace 自动创建（仅 startup keyspace）：若 `TikvClientPool::get_client()` 报错字符串包含 `"does not exist"`，则调用 PD HTTP API `POST /pd/api/v2/keyspaces` 创建并重试一次
- `src/tls.rs` (Verified): TLS acceptor 构建
- `src/pool.rs` (Verified): `TikvClientPool`（按 keyspace 获取/缓存 TiKV client）
  - `keyspace == "default"` 时会转成 TiKV 的 `"DEFAULT"`（注意大小写与缓存 key 的一致性风险）

**Protocol (pgwire)**
- `src/protocol/handler.rs` (Verified) (`~3211`): pgwire handler、连接级 session、鉴权、租户路由（`tenant.user` / `tenant:user`）、Simple/Extended Query/COPY
  - 租户解析：`parse_tenant_username()` 仅按 `.`/`:` 分割，不做大小写/合法性归一化
  - 认证：`StartupHandler::on_startup()` 始终走 `CleartextPassword`
  - executor/session 初始化：`DynamicPgHandler::init_executor()`（keyspace=用户名优先，其次 `PG_KEYSPACE`，最后 `"default"`）
  - **安全关键点（需特别验证）**：`authenticate_user()` 在 auth bootstrap 出现 `"gRPC"`/`"transport"` 错误时，直接 `Ok((true, true))` 放行并赋予 superuser（需要确认设计意图与威胁模型）
  - Extended Query（已读实现骨架）：
    - `impl ExtendedQueryHandler for DynamicPgHandler`：
      - `do_query()`：从 `Portal` 取 `statement` 字符串，做参数替换 `substitute_parameters()`，再直接 `executor.execute(session, final_query)`（即 extended protocol 最终复用 simple 执行路径）
      - `do_describe_statement()/do_describe_portal()`：通过 `infer_result_fields_from_query(...)` 推断返回字段；参数类型不足时补齐 `Type::UNKNOWN`
    - 参数替换：`substitute_placeholders_outside_strings_and_dollar()` 只在“非字符串/非 dollar-quoted”区域替换 `$1/$2/...`；值按 `param_type` 选择解码并在需要时加 `'...'` 转义（属于 correctness/security hotspot，但地图阶段只记录入口与机制）
- `src/protocol/copy_format.rs` (Verified): COPY 格式处理

**SQL Engine**
- `src/sql/parser.rs` (Unknown): sqlparser-rs Postgres dialect → AST
- `src/sql/executor.rs` (Verified): statement dispatch + autocommit/transaction glue（核心调度点）
  - 语句级 task-local：`statement_time::with_statement_timestamp_millis(...)` + `txn::with_savepoints(session.savepoints(), ...)`（影响 `now()`/时间函数与 SAVEPOINT 回滚日志记录）。
    - 事实：`statement_ts = now_timestamp_millis()` 在 `Executor::execute()` 开头只计算一次，并包住整个多语句循环；`UNKNOWN`：这是否符合 PostgreSQL `statement_timestamp()`/`now()` 的语义（需读 `src/sql/statement_time.rs` + 时间函数实现处）。
  - 预解析拦截（不走 sqlparser AST）：
    - `CREATE/DROP EXTENSION`、`CREATE/DROP FUNCTION`、`CREATE/DROP TRIGGER`
    - `REFRESH/DROP MATERIALIZED VIEW`、`CREATE/DROP PROCEDURE`、`CREATE TYPE ... AS ENUM`、`DROP TYPE`
  - 多语句与错误/skip 策略：
    - `parse_sql(sql)` 返回 `Vec<Statement>`，逐条执行并把结果聚合成 `ExecuteResults(Vec<ExecuteResult>)`（用于 Simple Query Protocol 的“每条语句一个结果”）。
    - 预解析阶段：`get_skip_reason(sql_upper)` 命中则直接返回 `ExecuteResult::Skipped`；解析失败时若 `get_unsupported_reason(sql_upper)` 命中也返回 `Skipped`，否则记录一次失败采样并返回错误。
  - 每条语句执行都会包一层：`extensions::context::with_context(is_superuser, async { ... })`（影响 extension 权限模型；需结合 `src/extensions/context.rs` 验证实际作用域）。
  - Autocommit 事务策略：非显式事务下每条语句 `BEGIN` → 执行 → 成功 `COMMIT` / 失败 `ROLLBACK`；但 observability sys queries 在 autocommit 成功时会走 `ROLLBACK`（避免计入 commit/TPS）。
  - `SET search_path`：只在 `Statement::SetVariable` 分支中特判；其它 `SET` 变量基本 no-op。
    - 允许的 value 形式：`Identifier` / `CompoundIdentifier(len==1)` / `'a,b'` 单引号字符串（按逗号切分）；其它表达式直接报错。
    - 归一化规则：去掉 `$user`；`["default"]` 变成 `["public"]`；禁止 schema 名包含 `.`；空列表默认 `["public"]`。
  - `DROP TABLE IF EXISTS ...`：对每个不存在的表生成 `ExecuteResult::Notice`（`collect_notices_before_statement()`，消息类似 `table "<name>" does not exist, skipping`）。
  - observability 账号限制：`_pgtikv_sys_observer` 非 superuser 只能执行两类查询：
    - 单表无 JOIN 的 `FROM _pgtikv_sys_observability/_pgtikv_sys_query_samples`
    - tableless query（`SELECT 1` 这类无 FROM；仍有限制：无 WITH/锁/子查询）
    - 事实：对该账号的 `BEGIN/COMMIT/ROLLBACK/SAVEPOINT/SET ...` 等事务控制语句，在 `Executor::execute()` 中会直接返回 `Empty` 并 `continue`（不进入 `session.*` 分支）；因此这些语句对 observability 账号是“表面允许但无效果”。`UNKNOWN`：是否刻意避免它们影响 session 状态（需结合 portal 预期与 handler 行为验证）。
  - tableless query（`execute_tableless_query()`）：允许少量 SRF/函数（`UNNEST/regexp_split_to_table/regexp_matches/jsonb_*`）与 `pg_sleep`；SRF 多列时按最大长度“拉齐”生成多行（短数组补 `NULL`）。
  - set operation（`UNION/INTERSECT/EXCEPT`）：通过 `query::apply_union/apply_intersect/apply_except` 在内存里合并 `Row`；返回 `column_types: None`（类型推断/返回协议侧需再确认）。
  - `EXPLAIN`：
    - `EXPLAIN (ANALYZE)`：只支持 `Statement::Query`，会真实执行并追加 `Actual Rows/Execution Time`。
    - 规划侧 schema lookup：`list_tables()` + `get_schema()`；`row_count_lookup` 恒为 `1000`（plan 质量与 explain 可信度需单独评估）。
  - `execute_copy_insert()`：当 session 不在显式事务中时，每次调用会 `BEGIN`→插入→成功 `COMMIT`/失败 `ROLLBACK`；并在插入后显式写入所有 index entry（COPY FROM 的事务原子性见 Flow 8）。
- `src/sql/executor_select.rs` (Partial), `src/sql/executor_join.rs` (Partial), `src/sql/executor_subquery.rs` (Unknown), `src/sql/executor_cte.rs` (Unknown): SELECT 相关执行路径
- `src/sql/ddl.rs` (Unknown), `src/sql/dml.rs` (Unknown), `src/sql/executor_*_ops.rs` (Partial): DDL/DML 执行与具体 KV 操作
- `src/sql/expr.rs` (Unknown): 表达式与内置函数（体量最大，典型 correctness hotspot）
- `src/sql/aggregate.rs` (Unknown), `src/sql/window.rs` (Unknown): 聚合/窗口函数（correctness + perf hotspot）
- `src/sql/planner.rs` (Unknown): 规划/索引选择（perf + correctness）
- `src/sql/session.rs` (Verified): SQL session/事务语义（与 `src/txn/*` 交织）
- `src/sql/sequences.rs` (Unknown), `src/sql/triggers.rs` (Unknown), `src/sql/trigger_queue.rs` (Unknown), `src/sql/trigger_worker.rs` (Unknown): 序列/触发器/异步队列 worker
- `src/sql/information_schema.rs` (Unknown): system catalog / 信息模式（兼容性面）

**Transaction**
- `src/txn/state.rs` (Partial), `src/txn/savepoints.rs` (Partial): 事务状态与 savepoint 语义

**Storage**
- `src/storage/tikv_store.rs` (Verified): TiKV wrapper（keyspace 隔离落点之一）
  - keyspace 隔离方式：`TikvStore::new_with_keyspace()` 通过 `tikv_client::Config::with_keyspace(ks)` 创建 client；`TikvStore::key()` 只是 `to_vec()`（不做任何 prefix/namespace）。
  - Schema catalog：
    - built-in schema：`public` / `pg_catalog` / `information_schema` / `extensions`（`TikvStore::is_builtin_schema`）。
    - `list_schema_oids()` 固定内建 OID：`pg_catalog=11`, `public=2200`, `information_schema=13222`, `extensions=2201`；用户 schema OID 存在 `_sys_schemadef_*`，缺失时会分配并写回（见 `next_schema_oid()`）。
  - `get_schema(txn, table_name)`：按 **传入字符串原样** 查询 `_sys_schema_<table_name>`（没有 `public.` 补全/`search_path` 逻辑；调用方必须先做名字解析/全名化）。
  - PK 冲突错误：`insert()` 在 key 已存在时返回 Postgres 风格错误串（含 `DETAIL`），constraint 名使用 `table_name` 去掉 schema 后的 `<short_table>_pkey`。
- `src/storage/encoding.rs` (Verified): key encoding / layout（所有持久化 schema/data/index 的“地基”）

**Auth / RBAC**
- `src/auth/*` (Verified): 用户/角色/权限（与 system keys 交织，`tenancy+security` hotspot）
- `src/sql/rbac.rs` (Unknown): SQL 层的 RBAC 相关 glue（需要确认与 `src/auth/*` 分工）

**Extensions**
- `src/extensions/*` (Verified) + `src/sql/executor_extensions.rs` (Verified): 内建 extension（尤其 `http`：`security + tenancy + obs` 风险集中）
  - `src/extensions/mod.rs`: extension 描述符（内置 OID/version/default schema）+ per-tenant 安装状态 `InstalledExtension`（序列化持久化到 `_sys_*` 元数据 key；具体 key 需读 store/ddl 实现确认）。
  - `src/extensions/context.rs`: per-statement task-local `ExtensionContext { is_superuser, http_requests }`
    - `with_context(is_superuser, ...)`：由 executor 每条语句包裹（避免把 session 状态层层传递）
    - `try_consume_http_request(max)`：每 statement 计数，超限报错 `http: max_requests_per_statement exceeded`
  - `src/extensions/http.rs`: `http_*` table functions（只读外部 HTTP）
    - 权限：`execute_table_function()` 要求 `context::is_superuser()==true`，否则 `permission denied for extension "http"`
    - SSRF/安全限制（`validate_url()`）：只允许 `http/https`；默认禁用明文 `http`（需 `PGTIKV_HTTP_ALLOW_INSECURE=true`）；禁止 userinfo；只允许默认端口（`https:443`/`http:80`）；禁止 `localhost/*.localhost/*.local`；禁止直连或 DNS 解析到 loopback/private/link-local/unspecified IP
    - 资源限制：
      - 每 statement 最多 `5` 次请求（依赖 `ExtensionContext`）
      - 每 tenant 每节点并发上限 `20`（`Semaphore`，进程内）
      - connect timeout `1s`，overall timeout `5s`
      - request body ≤ `256KiB`；response body ≤ `1MiB`
      - redirects：最多 `3` 次；对 `301/302/303` 会切换成 `GET` 并清空 body/content-type
    - 响应约束：response 必须是 UTF-8；headers 被序列化为 JSON 数组 `[{field,value}, ...]`
  - `src/sql/executor_extensions.rs`：
    - `Executor::try_execute_extension_table_function(...)`：识别/执行 `extensions.http_*` table function
      - 名称解析：显式 schema 必须是 `extensions`；若省略 schema，则要求 `search_path` 包含 `extensions`（忽略大小写），否则返回 `None`（当作普通函数/表处理）。
      - 参数规则（以 http 为例）：`http_get/head/delete(url text)`、`http_post/put(url text, body text, content_type text)`；参数通过 `expr::eval_expr(expr, None, None)` 求值后强制要求非 NULL `TEXT`
      - 安装/开关：读 `store.get_extension(txn, \"http\")`；未安装报 `extension \"http\" is not installed`；禁用报 `extension \"http\" is disabled`
      - 执行：`http::execute_table_function(self.tenant_keyspace(), call).await?`（受 ExtensionContext + limiter 约束）
      - alias：支持 `AS t(col1,...)` 重命名输出列（列数不匹配直接报错）
    - `execute_create_extension_cmd()/execute_drop_extension_cmd()`：
      - 解析：基于字符串扫描（strip 注释、trim、大小写不敏感），并把 `extensions.http` 这类前缀解析成 name=`http`
      - 权限：仅 superuser 允许 create/drop
      - `CREATE EXTENSION` 会校验默认 schema（`extensions`）必须存在（`store.schema_exists()`）

**Observability**
- `src/observability.rs` (Verified): in-memory 观测与采样（`obs + perf`），以及对 portal/诊断查询的支撑点
  - 配置（env）：
    - `PGTIKV_OBS_ENABLED`（默认 true）
    - `PGTIKV_OBS_SAMPLE_EVERY`（默认 1000；1/N 采样）
    - `PGTIKV_OBS_SLOW_MS`（默认 200ms；慢查询总是采样）
    - `PGTIKV_OBS_MAX_SAMPLE_EVENTS`（默认 20000；样本事件环形上限）
    - `PGTIKV_OBS_MAX_SAMPLE_GROUPS`（默认 50；聚合后最多返回组数）
    - `PGTIKV_OBS_MAX_SQL_LEN`（默认 512；normalize 后截断并加 `…`）
  - `ObservabilityRegistry::tenant(keyspace)`：按 keyspace 获取/缓存 `Arc<TenantObservability>`（空 keyspace 会归到 `"default"`）
  - `TenantObservability`：
    - `connection_open()`：增加 `active_connections`，Drop 时递减（连接数近似）
    - `record_statement(latency, ok, sql_supplier)`：更新 rolling window，并按规则采样到 `VecDeque<SampleEvent>`
      - 采样规则：所有错误必采样；慢查询必采样；否则按 `fast_rand_u64()%sample_every==0`
      - `normalize_sql()`：压缩空白→单空格、去掉末尾 `;`、trim、长度截断；并把 `|` 替换为空格（portal pipe-delimited 兼容）
    - `record_commit()`：更新 rolling window 的 commit 计数（TPS 来源）
    - `snapshot_summary()`：统计窗口内 statement/commit/error、QPS/TPS、avg/p99 latency、active connections
    - `snapshot_query_samples()`：按 `fingerprint(fnv1a_64(normalized_sql))` 聚合样本，计算 avg/p99/max 与 last_seen

**Types**
- `src/types/*` (Unknown): `Value/Row/TableSchema/DataType` 等（贯穿 correctness + encoding）

### 1.2 Cloud Admin Portal（`cloud-admin-portal/`）

> 这一节将单独建立横向分层：backend / frontend / deploy / scripts，并标出与核心服务交互的边界与信任模型。

**Backend (`cloud-admin-portal/backend/`)**
- Status: Partial
- 入口：`cloud-admin-portal/backend/app/main.py` (Verified) `create_app()`（FastAPI + CORS；未看到全局 auth middleware/dependency）
- 租户会话：`cloud-admin-portal/backend/app/session.py` (Verified)
  - `SessionManager` 以 `ts_<hex>` 形式生成 `session_id`，TTL 默认 1 小时（`Settings.session_ttl_hours`）
  - session 内容包含 `admin_user/admin_password`（内存保存；进程重启丢失）
- pg 客户端：`cloud-admin-portal/backend/app/services/pg_client.py` (Verified)
  - 使用 `pg8000`（纯 Python）连接 pg-tikv；用户名固定拼成 `tenant.user`（点分隔）
  - `_run_sql()` 把结果格式化为 `psql -t -A` 风格（按 `|` 连接列、`\n` 分行），供 observability/SQL editor 解析
  - observability 读取：直接查询 `_pgtikv_sys_observability()` / `_pgtikv_sys_query_samples()`，按 `|` split 解析（因此服务端采样必须避免在 SQL 里出现 `|`，见 `src/observability.rs` 的替换逻辑）
  - 关键 API（未看到全局鉴权）：
  - `cloud-admin-portal/backend/app/api/tenants.py` (Verified)
    - `POST /api/tenants/{tenant_id}/connect`：校验 admin 凭据后返回 `session_id`（前端存 `sessionStorage`，后续用 `X-Tenant-Session`）
    - `POST /api/tenants/{tenant_id}/query`：依赖 `X-Tenant-Session`，以 session 内的 admin 凭据执行任意 SQL
    - `POST /api/tenants/{tenant_id}/observability/bootstrap`：用 admin 凭据创建/轮转 `_pgtikv_sys_observer`，并把密码写入 portal DB（随后 observability API 使用该账号查询）
    - `GET /api/tenants/{tenant_id}/observability`：读取 DB 中保存的 observer 账号/密码后查询（endpoint 本身未要求 `X-Tenant-Session`）
  - `cloud-admin-portal/backend/app/api/users.py` (Verified)：用户管理 API 依赖 `X-Tenant-Session`
  - `cloud-admin-portal/backend/app/api/system.py` (Verified)、`cloud-admin-portal/backend/app/api/audit.py` (Verified)：health/info/audit logs 未见鉴权

**Frontend (`cloud-admin-portal/frontend/`)**
- Status: Partial
- 基础 fetch：`cloud-admin-portal/frontend/src/api/client.ts` (Verified)
  - 对 `/tenants/<id>/...` 的请求会自动从 `sessionStorage["tenant_session:<id>"]` 注入 `X-Tenant-Session`
- session hook：`cloud-admin-portal/frontend/src/hooks/useTenantSession.tsx` (Verified)
  - connect 成功后把 `session_id` 写入 `sessionStorage`
- observability polling：`cloud-admin-portal/frontend/src/api/tenants.ts` (Verified) `useTenantObservability()`
  - 默认每 5s 轮询；若返回 HTTP 409（未 bootstrap observer）则停止轮询

**Deploy/Scripts**
- `cloud-admin-portal/deploy/`: 部署（密钥、环境变量、网络拓扑、默认权限）
- `cloud-admin-portal/scripts/`: 运维脚本（dev/build/bootstrap 等）

---

## 2. Primary Flows（纵向：主要功能路径）

> 这一节回答：“一个外部行为从哪里进来，状态如何流转，在哪里读写 TiKV，哪里做鉴权/隔离，失败如何收敛”。

### 2.1 核心服务 Top Flows（第一版清单）

1. **Connection + Tenant Routing + Auth** (`tenancy + security`)
2. **Simple Query**（`Query` 消息 → parse/execute → 返回 rowset/tag）(`correctness`)
3. **Extended Query**（Parse/Bind/Describe/Execute/Synchronize）(`correctness + security`)
4. **Autocommit vs Explicit Transaction + Savepoints** (`correctness`)
5. **DDL**（schema/table/index/view/matview/sequence/type/extension）(`correctness + tenancy`)
6. **DML**（INSERT/UPDATE/DELETE + RETURNING + 约束）(`correctness + tenancy`)
7. **SELECT Engine**（scan/index, join, agg, window, subquery, CTE）(`correctness + perf`)
8. **COPY / pg_restore path**（大导入、格式、错误恢复）(`correctness + perf`)
9. **Extensions: http**（权限模型、请求限制、输出规范、采样/审计）(`security + tenancy + obs`)
10. **Async AFTER triggers worker**（队列一致性、重试/DLQ、跨租户公平）(`correctness + tenancy + obs`)
11. **Observability sys functions**（采样、分组、截断/规范化 SQL）(`obs + security`)

### 2.1.1 Flow 1：Connection + Tenant Routing + Auth

- Status: Verified
- Facts（已读到关键入口/分支）：
  - Socket accept：`src/main.rs` 里每条连接调用 `pgwire::tokio::process_socket()`，并为该连接创建 `DynamicHandlerFactory`
  - Startup：`src/protocol/handler.rs` `StartupHandler::on_startup()`
    - 读取 `METADATA_USER`，`parse_tenant_username()` → `(keyspace, actual_user)`
    - 写入 connection metadata：`METADATA_KEYSPACE` / `METADATA_ACTUAL_USER`
    - 发送 `Authentication::CleartextPassword`（明文口令挑战）
  - Password：`StartupHandler::on_startup()` 的 `PasswordMessageFamily` 分支
    - `authenticate_user(keyspace, actual_user, password)` → `(is_authenticated, is_superuser)`
    - 成功后 `init_executor(keyspace, actual_user, is_superuser)` 初始化 `Executor` + `Session`
  - keyspace 决策优先级：`username 前缀` > `PG_KEYSPACE` > `"default"`
  - `parse_tenant_username()` 仅按 `.`/`:` 分割，不做大小写/合法性归一化
  - **auth bootstrap 兜底**：`authenticate_user()` 在错误字符串包含 `"gRPC"` / `"transport"` 时直接返回 `Ok((true, true))`（即“已认证 + superuser”）
- Risks/Assumptions（待验证/需要实验）：
  - 上述 `"gRPC"/"transport"` 放行的触发条件、设计意图与安全边界（需结合 `src/auth/*` 与部署假设验证）
  - tenant/user 规范化缺失对 keyspace/cache/权限边界的影响（需结合 `src/pool.rs` 的缓存 key 与连接实验验证）
  - 认证失败、tenant 不存在、bootstrap 分支的 pgwire 错误返回与日志路径（需继续沿 `src/protocol/handler.rs` 验证）

### 2.1.2 Flow 2：Simple Query（Query → parse/execute → results）

- Status: Partial
- Facts（已读到 Executor 核心调度）：
  - 入口：`src/protocol/handler.rs` `SimpleQueryHandler::do_query()`（Simple Query Protocol 的 `Query` 消息入口；会调用 `Executor::execute()`）
  - `Executor::execute(session, sql)`：
    - `strip_leading_sql_comments(sql)` + `trim_start()` 决定 “starts_with” 判断（影响预解析拦截是否命中）
    - 少量命令走“预解析拦截”（例如 `CREATE EXTENSION`/`CREATE FUNCTION`/`CREATE TRIGGER` 等），直接返回 `ExecuteResults::single(...)`（不经 `sqlparser` AST）
    - `parse_sql(sql)` → `Vec<Statement>`；逐条执行并把每条语句的结果 append 到同一个 `ExecuteResults(Vec<ExecuteResult>)`
  - 事务与失败边界：
    - 事务控制语句（`BEGIN/COMMIT/ROLLBACK/SAVEPOINT/...`）直接调用 `session.*` 并返回 `Empty`
    - 其它语句按 `is_autocommit = !session.is_in_transaction()` 决定是否“每语句自动开关 txn”（见 Flow 4）
    - `DROP TABLE IF EXISTS ...`：在真正执行前可能先产生 `Notice`（每个不存在的表一条）
  - 可观测性采样：对非 observability sys query，每条语句执行后调用 `TenantObservability::record_statement(elapsed, ok, || stmt.to_string())`；解析失败也会记一次失败采样（`Duration::from_millis(0)`）
- Risks/Assumptions（待验证/需要实验）：
  - Simple Query 的“结果如何映射成 pgwire 消息序列”（RowDescription/DataRow/CommandComplete/ReadyForQuery…）仍需回到 `src/protocol/handler.rs` 逐分支验证
  - `Skipped`/`Notice` 等非典型结果对客户端兼容性的影响（需结合协议层如何编码这些结果验证）

### 2.1.3 Flow 3：Extended Query（Parse/Bind/Describe/Execute → final SQL → execute）

- Status: Partial
- Facts（已读到主要执行链路）：
  - 入口：`src/protocol/handler.rs` `impl ExtendedQueryHandler for DynamicPgHandler`
    - `do_query(portal, ...)`：从 portal 取原始 SQL 字符串 → `substitute_parameters(query, portal)` → `Executor::execute(session, final_query)`
    - `do_describe_statement` / `do_describe_portal`：基于 SQL 字符串做返回字段推断（`infer_result_fields_from_query`）；参数类型不足时补齐 `Type::UNKNOWN`
  - 执行性质：extended protocol 的执行最终是“执行一段 substituted SQL 字符串”（没有独立的 prepared-plan 执行器），因此其行为/限制与 `Executor::execute()` 绑定
  - 返回性质：`do_query()` 只把 `ExecuteResults` 的 `last()` 映射成 pgwire `Response`
- Risks/Assumptions（待验证/需要实验）：
  - `infer_result_fields_from_query()` 的推断逻辑与准确性（需继续读 `src/protocol/handler.rs` 对应函数体）
  - placeholder 替换（`substitute_parameters`）的 quoting/type 规则对 correctness/security 的影响（需结合参数解码与转义实现做逐分支验证）

### 2.1.4 Flow 4：Autocommit vs Explicit Transaction + Savepoints

- Status: Partial
- Facts（已读到事务控制入口与 autocommit glue）：
  - 适用范围：
    - Simple Query：`Executor::execute(session, sql)` 内部默认分支
    - COPY FROM：`Executor::execute_copy_insert(session, ...)`
  - 事务控制语句：
    - `Statement::StartTransaction` → `session.begin().await?`
    - `Statement::Commit` → `session.commit().await?`
    - `Statement::Rollback { savepoint: None }` → `session.rollback().await?`
    - `Statement::Savepoint { name }` → `session.create_savepoint(normalize_ident(name))?`
    - `Statement::Rollback { savepoint: Some(name) }` → `session.rollback_to_savepoint(&sp).await?`
    - `Statement::ReleaseSavepoint { name }` → `session.release_savepoint(&sp)?`
  - Autocommit glue：对“非事务控制语句”，以 `is_autocommit = !session.is_in_transaction()` 判断是否自动包一层 txn
    - `true`：`session.begin()` → 执行 → 成功 `session.commit()` / 失败 `session.rollback()`
    - `false`：不自动 commit/rollback；执行失败直接向上返回错误
  - 特例：observability sys query 在 autocommit 成功时会走 `session.rollback()`（避免计入 commit/TPS）
  - savepoints task-local：`Executor::execute()` 会把 `session.savepoints()` 传给 `crate::txn::with_savepoints(...)` 包住整个执行过程
- Risks/Assumptions（待验证/需要实验）：
  - 失败后事务是否进入错误态、后续语句的行为是否贴近 PostgreSQL（需结合 `src/sql/session.rs` 的错误路径与回滚策略验证）
  - savepoints task-local 的消费方与对回滚/触发器队列/序列的影响（需继续读 `src/txn/*` + triggers 实现）

### 2.1.5 Flow 5：DDL（CREATE/ALTER/DROP → catalog/store）

- Status: Partial
- Facts（已读到 statement 分发骨架）：
  - 入口：`Executor::execute()`（Simple/Extended 最终都会走这里）→ `execute_statement_on_txn(...)`
  - `execute_statement_on_txn(...)` 对 DDL 的主要分支：
    - `CREATE TABLE` → `src/sql/ddl.rs`（或 `CREATE TABLE AS` 走 executor 内部 `execute_create_table_as`）
    - `CREATE INDEX` / `DROP INDEX` / `ALTER TABLE` / `DROP TABLE/VIEW` / `TRUNCATE` → executor 方法 + `src/sql/ddl.rs`
    - `CREATE/DROP SCHEMA` → `TikvStore::{create_schema,drop_schema_restrict}`
    - `CREATE SEQUENCE` / `DROP SEQUENCE` → `src/sql/sequences.rs`
    - `CREATE TYPE` → `src/sql/udt.rs`
    - `CREATE VIEW` / `CREATE MATERIALIZED VIEW` / `DROP MATERIALIZED VIEW` / `REFRESH MATERIALIZED VIEW` → executor + `src/sql/ddl.rs`
    - RBAC DDL：`CREATE ROLE` / `ALTER ROLE` / `GRANT` / `REVOKE` / `DROP ROLE` → `src/sql/rbac.rs` + `src/auth/*`
    - EXTENSION DDL：`CREATE/DROP EXTENSION`（预解析拦截）→ `src/sql/executor_extensions.rs`
  - 路径终点（持久化类别）：`src/storage/tikv_store.rs`（`_sys_schema_*` / `_sys_schemadef_*` / `_sys_view_*` / `_sys_matview_*` / `_sys_type_*` / `_sys_seq_*` / `_sys_ext_*` 等）
- Risks/Assumptions（待验证/需要实验）：
  - DDL 语义、命名解析（schema/search_path）、OID/约束一致性与落盘 key 的准确性仍需读 `src/sql/ddl.rs` / `src/sql/sequences.rs` / `src/sql/udt.rs` / `src/sql/rbac.rs` 收敛

### 2.1.6 Flow 6：DML（INSERT/UPDATE/DELETE → row/index → triggers?）

- Status: Partial
- Facts（已读到分发入口与实现文件线索）：
  - 入口：`Executor::execute()` → `execute_statement_on_txn(...)` → `Statement::{Insert,Update,Delete}` 分支
  - DML 具体实现入口：`src/sql/executor_dml_ops.rs` `Executor::{execute_insert,execute_update,execute_delete}`
    - 依赖：`src/sql/dml.rs`（行准备/RETURNING 组装等）+ `src/storage/tikv_store.rs`（写 row + index entries）
    - 触发器线索：`src/sql/executor_dml_ops.rs` 引入 `triggers/trigger_queue/trigger_worker`（完整语义见 Flow 10）
- Risks/Assumptions（待验证/需要实验）：
  - 约束（PK/UNIQUE/FK/CHECK）、RETURNING、并发冲突与错误码的具体实现落点仍需读 `src/sql/dml.rs` / `src/sql/executor_dml_ops.rs` + `src/storage/tikv_store.rs`
  - DML 与 triggers enqueue 的事务边界/幂等性/失败策略需沿 Flow 10 继续读实现收敛

### 2.1.7 Flow 7：SELECT Engine（Query → planner/scan/join/agg/window → rowset）

- Status: Partial
- Facts（已读到上层分流与数据源入口）：
  - 入口：
    - `Executor::execute_statement_on_txn(..., Statement::Query(query))` → `Executor::execute_query(...)`
    - `Executor::execute_query(...)` → `build_cte_context(...)` → `src/sql/executor_select.rs` `execute_query_with_ctes(...)`
  - 关键分流点（上层）：
    - set operation：`UNION/INTERSECT/EXCEPT` → `Executor::execute_set_operation(...)`（内存合并）→ 再统一应用 `ORDER BY/LIMIT/OFFSET`
    - tableless：`SELECT ...` 无 `FROM` → `Executor::execute_tableless_query(...)`（少量 SRF/pg_sleep）
    - join：存在 JOIN / 多 FROM → `src/sql/executor_join.rs` `execute_join_query_with_ctes(...)`
    - single-table：继续走 `src/sql/executor_select.rs`（内部会调用 planner/scan/expr/agg/window 等模块）
  - “数据源”入口：`src/sql/executor_join.rs` 的 `get_table_data()`
    - sys/virtual tables：`_pgtikv_sys_observability/_query_samples/_trigger_queue_stats/_trigger_dlq`、`current_schema/current_database/...`
    - CTE：从 `ctes` map 直接取
    - `information_schema.*`：`src/sql/information_schema.rs`
    - view：`TikvStore::get_view()` → 递归执行 view query
    - base table：按 `search_path` 生成候选全名 → `TikvStore::get_schema()` / `TikvStore::scan()`
- Risks/Assumptions（待验证/需要实验）：
  - planner/scan/index 选择、join/agg/window/subquery/CTE 的语义与性能边界仍需继续读 `src/sql/executor_select.rs` / `src/sql/executor_join.rs` / `src/sql/planner.rs` / `src/sql/expr.rs` / `src/sql/aggregate.rs` / `src/sql/window.rs`

### 2.1.8 Flow 8：COPY（COPY IN / COPY TO STDOUT）

- Status: Verified（协议层 + executor 路径）
- Facts（已读到 COPY TO/COPY FROM 的关键实现）：
  - COPY TO STDOUT（Simple Query）：
    - `src/protocol/handler.rs` `SimpleQueryHandler::do_query()` → `parse_copy_to_command()`（regex；支持 `schema.table`，不支持 quoted ident）
    - `handle_copy_to_stdout()`：把 COPY 改写成 `SELECT ... FROM <table>`，执行后用 `src/protocol/copy_format.rs` 输出 COPY text
  - COPY FROM STDIN（Simple Query → CopyHandler）：
    - `SimpleQueryHandler::do_query()` 识别 `COPY ... FROM stdin`（regex；仅支持裸表名或 `public.` 前缀）
    - `CopyHandler::on_copy_data()`：把每个 `CopyData` chunk 追加到 `CopyContext.data_buffer`（全量缓存在内存）
    - `CopyHandler::on_copy_done()`：
      - 读取 schema（单独 begin+rollback 一个 txn）
      - 拼接所有 buffer → `lines()` → 按 `\t` split
      - **行列数不匹配会被静默跳过**（不会报错）
      - 每行调用 `Executor::execute_copy_insert()`
  - COPY FROM 的事务边界（代码可证）：
    - `Executor::execute_copy_insert()` 在 session 不在显式事务时会“每次调用一个 txn”（autocommit：`BEGIN`→插入→`COMMIT`），因此默认 COPY FROM 不是 statement-atomic
    - `Session::begin()` 在已处于事务时是 no-op（`src/sql/session.rs`），但 `CopyHandler::on_copy_done()` 在读 schema 后无条件 `session.rollback()` 用于结束 schema-read txn
- Risks/Assumptions（待验证/需要实验）：
  - 若 COPY FROM 发生在显式事务中，上述无条件 `rollback()` 是否会回滚“外层事务”（需要结合 `SimpleQueryHandler::do_query()` 对 COPY 与多语句/显式事务的处理做实验验证）
  - COPY FROM 的 schema 名称处理是否与 `_sys_schema_<table_name>` 的 key 规则一致（regex 可能丢弃 `public.`；需以 COPY handler 实际传参为准）
  - `CopyContext.data_buffer` 全量缓存对大导入的内存与延迟影响（perf 风险点）

### 2.1.9 Flow 9：Extensions/http（table function → outbound request → rowset）

- Status: Partial（extension 本体 Verified；与 SELECT 引擎的集成待补）
- Facts（已读到 extension 识别/权限/限制）：
  - 入口：`src/sql/executor_extensions.rs` 把 `extensions.http_get(...)` 这类 table function 交给 `Executor::try_execute_extension_table_function(...)`
    - 若调用时省略 schema（`http_get(...)`），只有当 `search_path` 包含 `extensions` 才会被识别为 extension table function
  - 权限与安装状态：
    - `CREATE/DROP EXTENSION`：仅 superuser 允许（`execute_create_extension_cmd()/execute_drop_extension_cmd()`）
    - `http_*` table functions：需要 extension 已安装且 `enabled=true`，否则报错
    - `http::execute_table_function()` 额外要求 statement context 中 `is_superuser==true`（否则 `permission denied for extension "http"`）
  - 出站请求约束（`src/extensions/http.rs`）：
    - 默认只允许 `https://`（`http://` 需 `PGTIKV_HTTP_ALLOW_INSECURE=true`）
    - 仅允许默认端口（`https:443` / `http:80`），禁止 userinfo，禁止 localhost/私网/链路本地/未指定 IP（含 DNS 解析结果）
    - 每 statement 最多 5 次请求；每 tenant 每节点最多 20 个并发请求
    - connect timeout 1s，overall timeout 5s；request body ≤ 256KiB；response ≤ 1MiB；redirects ≤ 3
  - 输出：单行 rowset：`status int4`, `content_type text nullable`, `headers jsonb`, `content text`（content 必须是 UTF-8）
- Risks/Assumptions（待验证/需要实验）：
  - `Executor::try_execute_extension_table_function(...)` 在 SELECT 执行链路中的具体调用点仍未知（需读 `src/sql/executor_select.rs` / `src/sql/executor_join.rs` 的 TableFactor/FunctionScan 处理）

### 2.1.10 Flow 10：Async AFTER triggers（enqueue → worker → DLQ/stats）

- Status: Partial（worker 启动 + sys 诊断表 Verified；enqueue/worker 语义待补）
- Facts（已读到 worker 启动与 sys 诊断入口）：
  - worker 启动：`src/main.rs` `sql::trigger_worker::spawn_trigger_worker(client_pool.clone())`
  - sys 诊断表（`src/sql/executor_join.rs` `get_table_data()`）：
    - `_pgtikv_sys_trigger_queue_stats`（扫描队列前缀并聚合 pending/processing/failed、DLQ count、近 1min 事件数等）
    - `_pgtikv_sys_trigger_dlq`（扫描 DLQ 前缀并列出失败事件）
  - DML 路径中存在 triggers 相关模块线索：`src/sql/executor_dml_ops.rs`（imports `triggers/trigger_queue/trigger_worker`）
  - 事件持久化/消费实现入口：`src/sql/trigger_queue.rs` / `src/sql/trigger_worker.rs`
- Risks/Assumptions（待验证/需要实验）：
  - enqueue 的事务边界、幂等性与失败重试/DLQ 策略需继续读 `src/sql/triggers.rs` / `src/sql/trigger_queue.rs` / `src/sql/trigger_worker.rs` 收敛

### 2.1.11 Flow 11：Observability（record → snapshot → sys functions/portal）

- Status: Verified
- Facts（已读到采集、聚合与 sys table 映射）：
  - 采集入口：
    - `Executor::execute()`：对非 sys-observability 查询，每条语句执行后调用 `TenantObservability::record_statement(...)`（解析失败也会记一次失败采样）
    - `Session::commit()`：在 TiKV txn commit 成功后调用 `TenantObservability::record_commit()` 增加 commit 计数（TPS 来源，见 `src/sql/session.rs`）
    - `TenantObservability::connection_open()`：每连接持有 `ConnectionGuard`，Drop 时递减活跃连接数
  - 采样与规范化（`src/observability.rs`）：
    - 错误必采样；慢查询（≥`PGTIKV_OBS_SLOW_MS`）必采样；其余按 `1/PGTIKV_OBS_SAMPLE_EVERY` 抽样
    - SQL `normalize_sql()`（压缩空白、去掉末尾 `;`、截断至 `PGTIKV_OBS_MAX_SQL_LEN` 并加 `…`），并把 `|` 替换为空格
  - 对外快照：
    - `snapshot_summary()`：statement/commit/error、QPS/TPS、avg/p99 latency、active_connections（窗口：最近 1h / 或进程启动至今的 min）
    - `snapshot_query_samples()`：按 `fnv1a_64(normalized_sql)` 聚合，返回 top-N（按 sample_count）并带 last_seen_ms_ago
  - sys functions（SQL 层映射）：
    - SQL 执行层把 `_pgtikv_sys_observability()` / `_pgtikv_sys_query_samples()` 当作 `FROM <table>` 的“伪表”处理：`src/sql/executor_join.rs` `Executor::get_table_data()`
    - 命名匹配：支持 `_PGTIKV_SYS_OBSERVABILITY` / `_PGTIKV_SYS_QUERY_SAMPLES`，也支持带 schema 前缀（`...ends_with("._PGTIKV_SYS_*")`）
    - 数据来源：`self.observability().snapshot_summary()` / `snapshot_query_samples()`
  - 同一入口下还实现了 trigger 队列诊断表：`_pgtikv_sys_trigger_queue_stats` / `_pgtikv_sys_trigger_dlq`（通过 TiKV `txn.scan(...)` 扫描 queue/DLQ 前缀并聚合统计/列出事件）
- Risks/Assumptions（待验证/需要实验）：
  - 采样路径对高并发的锁竞争/内存占用影响与 portal polling 的负载耦合（perf/obs 风险点，需压测与采样配置验证）

### 2.2 Portal Top Flows（第一版清单）

- Status: Partial
- Flows（索引）：
  1. **Portal 登录/租户选择 → 连接 pg-tikv**（凭据/权限模型）(`security + tenancy`)
  2. **Dashboard 拉取 observability 数据**（查询频率、SQL 规范化、错误处理）(`obs + perf`)
  3. **Bootstrap/运维动作**（创建 observer 账号、权限收敛、密钥管理）(`security + tenancy`)
- Facts（已读到关键实现入口）：
  - “登录/连接”并非全局账号体系：`cloud-admin-portal/frontend/src/hooks/useTenantSession.tsx` 通过 `POST /api/tenants/{tenant_id}/connect` 交换得到 `session_id`，写入 `sessionStorage["tenant_session:<tenant_id>"]`
    - `cloud-admin-portal/frontend/src/api/client.ts` 会对 `/tenants/<id>/...` 自动注入 `X-Tenant-Session`
    - 后端校验：`cloud-admin-portal/backend/app/api/tenants.py` `get_tenant_session()` / `cloud-admin-portal/backend/app/api/users.py` `get_tenant_session()`
    - session 存储：`cloud-admin-portal/backend/app/session.py`（内存；包含 admin 密码；默认 1h 过期）
  - observability dashboard：
    - 前端：`cloud-admin-portal/frontend/src/api/tenants.ts` `useTenantObservability()` 默认每 5s 拉取；HTTP 409（未 bootstrap）则停止轮询
    - 后端：`cloud-admin-portal/backend/app/api/tenants.py` `GET /api/tenants/{tenant_id}/observability` 使用 DB 中保存的 observer 账号/密码查询 `_pgtikv_sys_observability()` / `_pgtikv_sys_query_samples()`
    - pg 客户端：`cloud-admin-portal/backend/app/services/pg_client.py`（`pg8000` + pipe-delimited 输出解析）
  - bootstrap observer：
    - 后端：`cloud-admin-portal/backend/app/api/tenants.py` `POST /api/tenants/{tenant_id}/observability/bootstrap` 用 admin 凭据创建或轮转 `_pgtikv_sys_observer`，并把密码写入 portal DB
  - 安全边界（实现观察）：多条管理 API（例如 `GET/POST /api/tenants`、`GET /api/audit-logs`、`GET /api/system/health`、`GET /api/tenants/{tenant_id}/observability`）未见全局鉴权依赖；部署侧可能依赖网络层隔离/反向代理保护（需结合 `cloud-admin-portal/deploy/` 与实际部署验证）
- Risks/Assumptions（待验证/需要实验）：
  - `GET /api/tenants/{tenant_id}/observability` 未要求 `X-Tenant-Session` 是否为刻意设计（需要结合威胁模型与部署边界确认）

---

## 3. 场景 × 层 矩阵（用于“按图索骥”）

> 规则：每个格子只放“入口函数/关键结构体/关键文件”。后续逐步填充。

| Flow \\ Layer | main/pool/tls | protocol | sql | txn | storage | auth | extensions | obs | portal |
|---|---|---|---|---|---|---|---|---|---|
| Conn + tenant + auth | `src/main.rs` + `src/pool.rs` | `src/protocol/handler.rs` `parse_tenant_username()` + `StartupHandler::on_startup()` + `authenticate_user()` + `init_executor()` | `src/sql/session.rs` + `Executor::new()` | TODO | `src/storage/tikv_store.rs` + `TikvClientPool::get_client()` | `src/auth/*` `AuthManager::{bootstrap,authenticate}` | - | `src/observability.rs` connection guard | `cloud-admin-portal/*`（per-tenant connect） |
| Simple query | - | `src/protocol/handler.rs` `SimpleQueryHandler::do_query()` | `src/sql/executor.rs` `Executor::execute()` | `src/sql/session.rs` `Session::{begin,commit,rollback}` | `src/storage/tikv_store.rs`（经 `execute_statement_on_txn()`→DDL/DML/Query） | - | `src/extensions/context.rs` `with_context()` | `src/observability.rs` `TenantObservability::record_statement()` | - |
| Extended query | - | `src/protocol/handler.rs` `ExtendedQueryHandler for DynamicPgHandler` (`do_query/do_describe_*`) | `src/sql/executor.rs` `Executor::execute()`（执行的是 substituted SQL） | `src/sql/session.rs`（同 Simple Query） | `src/storage/tikv_store.rs`（同 Simple Query） | - | `src/extensions/context.rs`（每 statement 仍会包 context） | `src/observability.rs`（采样同 statement） | - |
| DDL | - | - | `src/sql/executor.rs` `execute_statement_on_txn()`→`src/sql/ddl.rs` | `src/sql/session.rs`（autocommit glue） | `src/storage/tikv_store.rs`（schema/catalog keys） | `src/auth/*`（role/rbac DDL） | `src/sql/executor_extensions.rs` | - | - |
| DML | - | - | `src/sql/executor.rs` `execute_{insert,update,delete}()` | `src/sql/session.rs`（autocommit glue） | `src/storage/tikv_store.rs`（row/index keys） | `src/sql/rbac.rs`/`src/auth/*`（权限校验） | - | - | - |
| SELECT engine | - | - | `src/sql/executor.rs` `execute_query()`→`execute_query_with_ctes()` | `src/sql/session.rs`（txn/sequence values） | `src/storage/tikv_store.rs`（scan/get/txn） | - | - | - | - |
| COPY | - | `src/protocol/handler.rs` `parse_copy_*` + `handle_copy_to_stdout()` + `CopyHandler::*` | `Executor::{parse_value_for_copy,execute_copy_insert}` | TODO | `TikvStore::get_schema()` | - | - | - | - |
| Extensions/http | - | - | `src/sql/executor_extensions.rs` `try_execute_extension_table_function()` + `execute_{create,drop}_extension_cmd()` | `src/sql/session.rs`（autocommit glue） | `src/storage/tikv_store.rs`（`get_extension/put_extension/drop_extension`） | `src/protocol/handler.rs`/`src/sql/session.rs`（superuser） | `src/extensions/{context,http}.rs` | `src/observability.rs`（采样记录发生在 statement 层） | `cloud-admin-portal/*`（若 portal 依赖 http ext，需明确边界） |
| Async triggers | `src/main.rs` `spawn_trigger_worker()` | - | `src/sql/executor_dml_ops.rs` + `src/sql/triggers.rs` + `src/sql/trigger_queue.rs` + `src/sql/trigger_worker.rs` + `src/sql/executor_join.rs`（sys 诊断表） | `src/sql/session.rs`（触发器 enqueue 时的 txn 语义） | `src/storage/encoding.rs`（队列 key 编码线索） + `txn.scan(...)`（sys 诊断表） | - | - | - | - |
| Observability | - | - | `src/sql/executor.rs` `is_observability_*` + `execute_tableless_query()` | `src/sql/session.rs` `commit()->record_commit()` | - | `src/auth/*`（observer 用户创建/权限） | - | `src/observability.rs`（registry/rolling window/samples） | `cloud-admin-portal/*`（dashboard 查询） |

---

## 4. Priority Overlay（关注点映射，用于后续深入）

### 4.1 SQL 正确性（`correctness`）

重点落点（第一版）：
- statement dispatch + 事务语义 glue：`src/sql/executor.rs`, `src/sql/session.rs`, `src/txn/*`
- SELECT 主引擎：`src/sql/executor_select.rs`, `src/sql/executor_join.rs`, `src/sql/executor_subquery.rs`, `src/sql/executor_cte.rs`
- 表达式/内置函数：`src/sql/expr.rs`
- 聚合/窗口：`src/sql/aggregate.rs`, `src/sql/window.rs`
- planner/index 使用：`src/sql/planner.rs`, `src/sql/index_helpers.rs`

后续深挖问题（占位，后续任务逐条补证据）：
- 是否存在“不同执行路径语义不一致”（JOIN vs non-JOIN、tableless vs table scan、subquery 复用等）
- NULL/三值逻辑、类型推断/转换、排序规则、时间/时区、numeric 精度
- 事务边界：autocommit、错误时 rollback、savepoint 行为
- DDL/DML 与系统 catalog/信息模式的一致性

### 4.2 多租户隔离与安全（`tenancy + security`）

不变量（必须全局成立）：
- **任何持久化数据必须 tenant-isolated**：所有 key 必须通过 keyspace-aware client/store 写入；禁止“全局 key”跨租户共享。
- 连接级租户路由必须可靠：用户名解析、默认 keyspace、错误分支不可绕过。
- 扩展/portal 的跨租户访问必须显式禁止或强隔离。

重点落点（第一版）：
- `src/protocol/handler.rs`（租户路由+认证入口）
- `src/pool.rs` / `src/storage/tikv_store.rs`（keyspace client 创建/复用策略）
- `src/auth/*`（用户/角色/权限存储与校验）
- `src/extensions/*`（外部 IO：HTTP）
- `cloud-admin-portal/*`（凭据、权限边界、是否存在“用 admin 口令做 dashboard”这类风险）

### 4.3 性能与可观测性（`perf + obs`）

重点落点（第一版）：
- planner 与扫描策略：`src/sql/planner.rs`
- join/window 的内存与排序：`src/sql/executor_join.rs`, `src/sql/window.rs`
- TiKV 往返与批量操作：`src/storage/tikv_store.rs`, `src/storage/encoding.rs`
- 采样与系统表函数：`src/observability.rs` + `src/sql/executor_join.rs`（已有 sys functions）
- portal 的轮询/缓存：`cloud-admin-portal/frontend/*` + backend 查询聚合方式

后续深挖问题（占位，后续任务逐条补证据）：
- O(N^2) join/window、全量 sort、无谓 clone/serialize、重复扫表
- 可观测性是否会反噬性能（采样路径、SQL 规范化/截断、锁竞争）
- portal 查询频率与“慢查询自激”（dashboard 造成负载）

---

## 5. Next Reads（v1 方向：从地图到细节）

- 下一步填充建议（按“对地图精度的贡献”排序）：
  1) SELECT engine 的分流与数据源：`src/sql/executor_select.rs` / `src/sql/executor_join.rs` / `src/sql/executor_subquery.rs` / `src/sql/executor_cte.rs`
  2) DDL/DML 的 KV 写路径与约束落点：`src/sql/ddl.rs` / `src/sql/dml.rs` / `src/sql/executor_*_ops.rs` + `src/storage/tikv_store.rs`
  3) 权限模型与落点：`src/auth/*` + `src/sql/rbac.rs` + `src/protocol/handler.rs`（superuser/observer/租户路由）
  4) Trigger 队列与 worker：`src/sql/triggers.rs` / `src/sql/trigger_queue.rs` / `src/sql/trigger_worker.rs`（以及 sys tables：`src/sql/executor_join.rs`）
  5) 表达式/类型系统热点（correctness）：`src/sql/expr.rs` / `src/sql/helpers.rs` / `src/types/*`

---

## 6. Evidence Appendix（文档证据与待验证假设）

> 说明：本章只记录“文档中写了什么”与“文档之间的冲突”。它们不是实现事实；实现以代码为准。

### 6.1 已读文档清单（Sources / Claims）

核心服务（Rust）：
- `README.md`：对外宣传的 feature 列表与约束/ORM 兼容性口径。
- `AGENT.md` / `CLAUDE.md`：开发约定与“对架构/能力”的口径（只作为线索，需代码验证）。
- `docs/architecture.md`：组件分层与若干限制（**明显存在与 README 不一致的部分**，见下一节冲突清单）。
- `docs/multi-tenancy.md`：keyspace 隔离、用户名路由（`tenant.user`/`tenant:user`）、默认 keyspace、bootstrap admin 口径。
- `docs/authentication.md`：cleartext password、RBAC、fallback password（`PG_PASSWORD`）口径。
- `docs/extensions.md`：`http` extension 的安全限制与限额（端口/SSRF/并发/每语句调用次数等）。
- `docs/configuration.md`：环境变量与部署示例（含若干疑似过期的日志样例）。
- `docs/README.md`：docs 索引与“支持特性”口径（含 JOIN 支持范围等条目）。
- `docs/quickstart.md`：快速启动与连接示例。
- `docs/admin-cli.md`：管理脚本/CLI 的对外用法口径。
- `docs/sql-reference.md`：SQL/函数文档（较保守口径）。
- `docs/constraint-implementation-report.md`：外键/检查/唯一等“约束实现”测试报告（含性能备注：FK 验证可能全表扫）。
- `docs/design/README.md`：设计文档索引（P0/P1/P2）。
- `docs/design/*.md`：P0/P1/P2 设计细节（实现应以代码为准）。
- `docs/backlogs/*`：仍被标记为“未实现”的设计草案（多处与现状可能冲突，需要用代码裁决）。
- `docs/NEON_TUTORIAL_ASSESSMENT.md`：按 Neon 教程维度的功能评估清单（多处与其它 docs 口径冲突，需对照代码）。
- `TIPG_SQL_SPEC.md`：基于“当时代码与测试”的 SQL 规格摘要（也存在与现状可能不一致的条目）。
- `bug.md`：Numeric 结果 OID 错误的复现、根因假设与 portal workaround（需对照代码验证）。
- `review.md`：Async AFTER trigger queue 的设计/实现摘要（提供若干代码入口线索）。
- `PROGRESS.md` / `TODO.md`：历史进度与 TODO（与当前代码/README 存在明显时间线冲突，谨慎使用）。
- `WORK.md`：多轮实现/修复的工作记录（包含当时的验证方法与回归点，但仍需以代码为准）。

Portal（cloud-admin-portal）：
- `cloud-admin-portal/README.md`：功能、API、鉴权模型（per-tenant session）。
- `cloud-admin-portal/AGENTS.md`：代码结构与关键模式（sessionStorage + X-Tenant-Session）。
- `cloud-admin-portal/CLAUDE.md`：架构与注意事项（与 AGENTS/bug.md 对 pg client 的描述存在冲突，需要读代码裁决）。
- `cloud-admin-portal/IMPLEMENTATION_SUMMARY.md`：SQLite 元数据/审计日志/软删除等实现摘要。
- `docs/todo/admin-portal-redesign.md`：portal 重构设计文档（从 legacy scripts → `cloud-admin-portal/`）。
- `WEB_WORK.md`：portal 实施进度与阶段性结论（与设计/旧口径存在时间线冲突，需以代码为准）。

PRDs（需求/兼容性驱动的 feature 口径）：
- `prds/*`：系统函数等“快速兼容”需求。
- `prds/dify-database-compatibility-spec.md` + `prds/dify-compatibility/*`：Dify 工作负载对 JSONB/时区/bytea 函数/GIN 索引等的需求清单。

工程内说明（会影响后续读代码顺序与规范）：
- `src/sql/AGENTS.md` / `src/protocol/AGENTS.md` / `src/storage/AGENTS.md`：子模块导览与常见陷阱（只作导航，不当作实现事实）。

### 6.2 文档冲突/时间线冲突（Hypotheses to verify）

> 这些不是结论，而是“需要验证的假设/冲突点”。后续读到相关代码会把它们逐条关闭。

- **功能口径冲突**：
  - `docs/architecture.md` 的 “Not Supported” 列表（RIGHT/FULL JOIN、触发器、TLS、物化视图等）与 `README.md`、`review.md` 明显冲突。
  - `TIPG_SQL_SPEC.md` 声称 “触发器仅存储不执行 / $$ 字符串不支持”等，与 `review.md`/`docs/design/07_*` 的方向可能冲突。
  - `PROGRESS.md` 仍写 “FK/CHECK 不 enforce”，与 `docs/constraint-implementation-report.md`/`README.md` 冲突。
- **多租户 keyspace 创建**：
  - `docs/multi-tenancy.md` 说 keyspace 需手工创建；`src/main.rs` 看起来会在连接失败时尝试调用 PD HTTP API 创建（需确认错误分支与行为）。
- **认证/日志口径**：
  - `docs/configuration.md` 的日志样例中 “Password authentication: disabled”，与 `README.md`/`src/main.rs` 的 “enabled” 口径冲突。
- **Portal 的 pg 客户端实现冲突**：
  - `cloud-admin-portal/AGENTS.md` 写 `pg8000`；`cloud-admin-portal/CLAUDE.md` 写 `psql subprocess`；`bug.md` 的 workaround 也暗示 `pg8000`。需要以 `cloud-admin-portal/backend/app/services/pg_client.py` 实际代码为准。
  - `docs/todo/admin-portal-redesign.md` 的旧架构图/示例仍写 `psql subprocess`；`WORK.md`/`WEB_WORK.md` 的口径是 backend 已切换到 `pg8000` 且移除 `psql` fallback（需以代码裁决）。
- **Portal 的全局鉴权口径冲突**：
  - `cloud-admin-portal/README.md` 明确写 “No global portal auth”（tenant 列表/创建/删除不需要登录）。
  - `docs/todo/admin-portal-redesign.md`/`WEB_WORK.md` 描述了 JWT Bearer 的全局鉴权与受保护的 tenant CRUD（需以实际实现裁决，并明确威胁模型/部署假设）。
- **缺失文档/过期引用**：
  - `AGENT.md` 引用 `HOW_TO_TEST.md`，仓库内不存在该文件（需确认是否改名/迁移到 `docs/`，并决定补齐或删除引用）。
- **Backlogs vs 现状**：
  - `docs/backlogs/06_copy_to_and_options.md` 仍写 “缺 COPY TO STDOUT”；但协议层已出现 `COPY ... TO STDOUT` 路径（需要核对支持范围与语义是否满足设计口径）。
  - `docs/backlogs/11_system_catalog_coverage.md` 仍写 `pg_proc` 返回空；`WORK.md` 记录已补齐 pg_catalog + stable OIDs（需读 `src/sql/information_schema.rs` 确认现状）。
- **JOIN 支持口径冲突**：
  - `docs/README.md` 的“Supported SQL Features”表格只列 INNER/LEFT JOIN；`docs/NEON_TUTORIAL_ASSESSMENT.md` 标注 RIGHT/FULL OUTER JOIN 已支持（需在 JOIN 执行路径验证）。

### 6.3 文档声明的不变量/安全边界（Doc-stated boundaries）

多租户（keyspace）：
- **连接路由**：用户名 `tenant.user` / `tenant:user` 选择 keyspace；无前缀走默认 keyspace（`docs/multi-tenancy.md`、`README.md`）。
- **隔离范围**：每个 keyspace 拥有独立的用户/角色/表/元数据；不允许跨 keyspace join（`docs/multi-tenancy.md`）。
- **keyspace 创建**：文档口径偏向“外部创建”（pd-ctl/tiup），但核心服务可能会尝试自动创建（见冲突点，需以代码为准）。

认证 / RBAC：
- **认证方式**：Cleartext password（`docs/authentication.md`）；生产需要 TLS（同文档）。
- **bootstrap admin**：每 keyspace 默认 `admin/admin`（多处文档声称“首次连接自动创建”，需代码验证具体触发条件与幂等）。
- **fallback 密码**：`PG_PASSWORD` 作为“全用户兜底密码”的测试兼容开关（文档明确声明“不要用于生产”）。

HTTP 扩展：
- **默认权限**：仅 SUPERUSER 可执行（`docs/extensions.md`）。
- **网络限制**：默认只允许 `https://` + 443；`PGTIKV_HTTP_ALLOW_INSECURE=true` 才允许 `http://` + 80。
- **SSRF 防护**：禁止 `localhost/.localhost/.local`，禁止解析到 loopback/private/link-local/unspecified 网段。
- **资源限制**：connect timeout 1s、total timeout 5s、max body 256KiB、max response 1MiB、max redirects 3、每语句最多 5 次 HTTP、每 tenant 每节点最多 20 个并发（文档声称“固定在代码中”）。

Portal（cloud-admin-portal）：
- **无全局 portal 登录**：tenant 列表/创建/禁用可匿名；用户管理需要 per-tenant connect（`cloud-admin-portal/README.md`；与 `docs/todo/admin-portal-redesign.md`/`WEB_WORK.md` 冲突，需代码验证）。
- **tenant session**：`POST /api/tenants/{name}/connect` 返回 `session_id`；前端存 `sessionStorage`；后端内存保存，TTL 默认 1h（`cloud-admin-portal/AGENTS.md`、`cloud-admin-portal/CLAUDE.md`）。
- **tenant 删除语义**：TiKV keyspace 只能 DISABLED，无法真正删除；portal 侧还有 soft-delete（SQLite）以隐藏 UI（`cloud-admin-portal/IMPLEMENTATION_SUMMARY.md`、`docs/admin-cli.md`）。

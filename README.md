# Domainry Todo

独立的待办领域、数据仓库和业务回执，不依赖 Agent 或 Agent SDK。

- `contract`：Todo、批次、查询、版本变更、独立服务接口。
- `module.Open(database, dialect, profile, migrationRegistrar, sourceAuthorizer)`：模块内部安装 Foundation Operations / Subject Lifecycle 与 Todo 自有表，并构造本地 Store；来源校验可选，不读取 Agent 表。
- Todo 只拥有 `_agent_user_todos`；幂等回执统一写入 Foundation `_operations`，不再维护 `_agent_todo_mutations`。
- `ApplyMutation` / `MutationReceipt`：业务效果与 shared Operations 回执在同一事务提交，支持只读恢复。
- `ApplyInTransaction`：允许既有 Agent 宿主在同一事务中验证执行账本并提交 Todo 效果，不把账本实现迁入 Todo。

日期 / 时区校验、批次原子性、版本冲突和用户隔离保持原行为。独立测试在没有 Agent 会话表的数据库中创建和读取待办，验证幂等与跨用户拒绝。

`module/module.go` 是薄公开入口；日期和时区规则位于 `internal/domain/todo/service/`；数据库与回执位于 `internal/infrastructure/persistence/database/todo/`。架构检查阻止领域代码反向依赖 SQL 或外部模块实现。

支持 SQLite、MySQL、PostgreSQL：使用宿主的 ORM renderer 与对应 profile，共用一套业务 SQL。宿主负责连接池、迁移锁和唯一 `_schema_migrations`，本模块不创建私有账本。真实三库验收位于 Agent 的 `internal/infrastructure/persistence/webhost/portable_test.go`，同时由 PM / Work 产品测试验证 Agent 写入待办、回执和重启恢复。

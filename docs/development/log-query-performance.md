# 日志筛选与查询性能

> 定位：现行开发与行为约束。带日期的部署或验证段落保留为当时记录；分类入口见[文档导航](../README.md)。

## 通用日志列展示

管理员可在“查看”菜单中勾选“渠道”，隐藏或恢复渠道列；默认显示。普通用户没有渠道列或对应菜单项。

“模型”后默认展示“推理强度”列，可在“查看”中隐藏或恢复，移动端卡片也展示该字段。
列表复用日志详情中的 `other.reasoning_effort` 和颜色标签，显示日志记录的推理设置，
不代表供应商实际执行强度。`none` 和未知等级保留原值并使用中性色；历史日志、未记录或空值显示 `—`，
不根据模型名称推测，也不补写历史数据。

列显隐沿用当前表格会话状态，不持久化到账号或浏览器；刷新页面后恢复默认。
切换列不改变查询条件、接口权限或导出内容。

## 日志字段权限

普通用户的日志列表、Token 日志与导出使用同一过滤规则：渠道 ID 返回 0、渠道名称为空，
移除 `other.admin_info`、`root_info`、`audit_info`，以及历史顶层的 `channel_id`、
`channel_name`、`channel_type`、`reject_reason`。计费明细、推理强度与流状态继续对日志所有者可见。

管理员可查看 `admin_info`、`audit_info`；`root_info` 仅 Root 可见。
标准日志写入会把上述历史敏感字段归入 `admin_info`，读取旧数据也做同样的归类，
冲突时保留已有 `admin_info` 值。异常或非对象 JSON 不直接返回，按空对象处理；
过滤使用原始 JSON 值，避免大整数因浮点转换丢失精度。读取接口不改写历史数据库记录。
历史归一化通过独立的在线预迁移工具显式执行，须先部署兼容版本；
迁移遇到异常或字段冲突会保留原数据并停止，不能沿用 API 空对象兜底或嵌套值优先覆盖历史记录。

管理员及 Root 的日志列表响应同时返回顶层 `other.reject_reason`，其值来自归类后的
`admin_info.reject_reason`，供已打开的旧版页面继续显示拒绝原因。该兼容字段仅存在于
管理员响应，普通用户列表、Token 日志与导出仍会移除它，写入数据库时也不会保存该别名。

此兼容依赖修复后的后端。若回切到尚无此兼容逻辑的旧后端，旧页面查看新格式日志时
仍可能不显示拒绝原因；回切目标需具备相同的管理员响应兼容与普通用户过滤能力。
不能为兼容旧页面将拒绝原因双写到数据库顶层，因为旧后端的普通用户过滤未移除该字段。

额度统计与最近 60 秒 RPM/TPM 分开读取，后者只回填自己的字段；额度缓存命中时仍实时查询 RPM/TPM。

## 分组筛选

用量日志的分组筛选使用可搜索的下拉单选，默认显示“全部分组”。搜索只过滤已有选项，
不能创建或提交任意输入值。选定后点击“查询”应用条件；再次选择已选分组、选择“全部分组”
或点击“重置”可清除分组条件。导出沿用筛选栏中当前选中的分组。

管理员选项来自 `/api/group/`，与渠道页一致；普通用户来自 `/api/user/self/groups`。
分组查询缓存按用户和角色区分。地区展示限制不妨碍查询已产生的日志。

下拉还会补充当前日志页出现的分组和 URL/草稿中的已选值，兼容删除或改名后的历史分组。
这不枚举全量历史分组：不在当前配置、当前结果或旧链接中的分组不会自动列出。
该交互不增加历史日志扫描或更改后端查询接口。隐藏敏感信息时，已选分组、选项名称与搜索输入
同步遮蔽；接口加载失败时可重试，仍可清除筛选或选择已有历史选项。

## 模型筛选性能

管理员日志列表按模型名称和时间范围筛选时，默认仍按 `logs.id DESC` 返回。MySQL 的
`prefer_ordering_index` 优化可能为了避免排序而选择主键倒序扫描；当某个模型在时间窗口内
不足一页时，查询无法提前凑满 `LIMIT`，可能扫描数百万行后超时。

## 查询保护

带模型名称的管理员日志列表会忽略 `PRIMARY` 和 `idx_created_at_id` 的排序用途，避免它们
被当作倒序扫描捷径。该限制只作用于 `ORDER BY` 的索引选择，不改变筛选条件、分页和排序
语义，也不影响不带模型筛选的日志列表。

`logs` 表同时声明 `idx_logs_model_created_at (model_name, created_at)` 复合索引，用于缩小
模型和时间范围组合筛选的候选集，并加速相同条件下的 `COUNT`、`SUM` 查询。

## 生产部署

`Log` 模型的 `AutoMigrate` 会自动补建复合索引。对于已有大量日志的实例，建索引可能产生
明显的磁盘 I/O 并延长应用启动时间。推荐在低峰期先执行以下幂等检查，再按变更窗口创建：

```sql
SELECT COUNT(*)
FROM information_schema.statistics
WHERE table_schema = DATABASE()
  AND table_name = 'logs'
  AND index_name = 'idx_logs_model_created_at';
```

仅当结果为 `0` 时执行：

```sql
ALTER TABLE `logs`
  ADD INDEX `idx_logs_model_created_at` (`model_name`, `created_at`),
  ALGORITHM=INPLACE,
  LOCK=NONE;
```

建索引前应确认磁盘余量、当前长事务和元数据锁等待。提前建好后再部署应用，`AutoMigrate`
会检测到索引已存在并跳过创建。

## 验收

使用实际模型和时间范围执行 `EXPLAIN ANALYZE`：

```sql
SELECT *
FROM `logs` IGNORE INDEX FOR ORDER BY (`PRIMARY`, `idx_created_at_id`)
WHERE `model_name` = 'example-model'
  AND `created_at` >= UNIX_TIMESTAMP(CURRENT_DATE)
  AND `created_at` <= UNIX_TIMESTAMP(NOW())
ORDER BY `id` DESC
LIMIT 100;
```

执行计划不应出现 `PRIMARY (reverse)` 或 `idx_created_at_id (reverse)` 的数百万行扫描；
实际扫描量应收敛到指定模型或时间范围的候选记录。

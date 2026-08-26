# IceCoreAcclimationGate

IceCoreAcclimationGate 面向极地冰芯样品管理员和低温实验室质量复核员，提供从批次登记、温度阶梯适应、超限隔离与重测、污染控制证据、独立复核到授权解封归档的版本化 HTTP JSON API。服务将批次快照原子写入本地目录，并维护可校验的追加式审计摘要链。

## API 流程

- `POST /api/v1/acclimation-cases`：登记批次。请求必须包含 `request_id`、`opened_by`、`storage_temperature_c` 和 `specimen_tubes`；相同 `request_id` 与载荷返回首次结果。
- `GET /api/v1/acclimation-cases`：查询批次目录。支持 `state`、`opened_by`、`created_from`/`created_to`（也接受 `created_at_from`/`created_at_to`）、`limit` 和 `cursor`，结果按 `created_at`、`case_id` 稳定排序并返回状态统计。
- `GET /api/v1/acclimation-cases/{case_id}`：读取批次快照、证据版本、复核历史及逐阶段 `monitoring_progress`。
- `POST /api/v1/acclimation-cases/{case_id}/draft-revision`：完整替换 Draft 批次的 `specimen_tubes` 与 `storage_temperature_c`，并记录 `reason` 和字段级差异。
- `POST /api/v1/acclimation-cases/{case_id}/protocol-preview`：使用 `planned_start_at` 和正式方案字段无副作用试算逐阶段检查点日程。
- `POST /api/v1/acclimation-cases/{case_id}/protocol`：配置阶梯方案并计算检查点预算。
- `POST /api/v1/acclimation-cases/{case_id}/start`：显式开始监测；也可从首条合格的读数提交进入监测。
- `POST /api/v1/acclimation-cases/{case_id}/readings`：按检查点提交监测读数。
- `POST /api/v1/acclimation-cases/{case_id}/readings-batch`：原子提交 2 至 100 条连续检查点读数，整批仅增加一次 revision。
- `POST /api/v1/acclimation-cases/{case_id}/deviation` 与 `/retest`：登记隔离偏差并依次完成重测队列。
- `GET /api/v1/acclimation-cases/{case_id}/quarantine-progress`：查询隔离触发项、处置资料与重测阶段进度。
- `POST /api/v1/acclimation-cases/{case_id}/evidence`：提交带采集时间和完整逐管覆盖的污染证据，失败版本会保留以供补证。
- `POST /api/v1/acclimation-cases/{case_id}/review`：由独立复核员退回结构化问题或完成销项复核。
- `POST /api/v1/acclimation-cases/{case_id}/issue-responses`：由经办人原子提交问题整改说明和合格补证版本引用。
- `POST /api/v1/acclimation-cases/{case_id}/release-preflight`：无副作用检查放行条件并预生成候选档案摘要。
- `POST /api/v1/acclimation-cases/{case_id}/release`：由职责分离的授权人冻结档案；必须提供正数 `expected_revision`、非空 `request_id` 和 `authorizer_id`。
- `POST /api/v1/acclimation-cases/{case_id}/verify`：重新校验清单摘要、持久化快照、审计链、最终修订和归档状态。
- `GET /api/v1/acclimation-cases/{case_id}/evidence-events`：按 `event_type`、revision 区间和 `limit` 查询批次审计时间线及档案条目定位。

除 `start`、只读查询和健康检查外，流程写请求均需携带正数 `expected_revision` 与非空 `request_id`。服务恢复时若归档清单或审计链校验失败，会保留查询和校验能力并关闭写操作；`GET /healthz` 的 `writable` 字段反映当前状态。

## 构建

```bash
go build ./cmd/server
```

## 运行

默认监听 `127.0.0.1:19091`，可通过 `-addr=127.0.0.1:<port>` 或合法的 `PORT` 端口号调整：

```bash
go run ./cmd/server -addr=127.0.0.1:19091
```

## 自检与测试

自检会启动真实回环 HTTP 服务并执行一次完整业务闭环，随后自动退出：

```bash
go run ./cmd/server -self-check -addr=127.0.0.1:19091
```

运行全部回归测试：

```bash
go test ./...
```

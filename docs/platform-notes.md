# 平台实测记录

> 格式：每确认一项就补一行，带日期与证据来源。**未确认的标「待验证」，不要当事实用。**

## WorkBuddy

| 项 | 状态 | 证据 |
|---|---|---|
| `POST /v2/billing/meter/checkin-activity-status` 路径存在 | **已验证** 2026-11-17 | 假 token 请求返回 APISIX 网关 401（JSON/HTML），非 404 |
| `POST /billing/meter/get-user-resource-summary` 路径（无 /v2） | **已验证** 2026-11-17 | 同上，返回 401 而非 404 |
| `POST /v2/billing/meter/daily-checkin` 路径 | 待验证 | 需真实 token |
| 业务码 10001 = 今日已签 | 待验证 | 来自逆向常量，需真实响应确认 |
| 响应字段 `checked_in` / `streak` / `total_credits` | **待验证** | 逆向未见真实响应，当前为多候选兜底 |
| 余额取 `data.Packages[].CycleRemainCapacity` 求和 | 待验证 | 逆向文档字符串明示，需真实响应确认 |
| 无需设备指纹 | 待验证 | 逆向注释称「仅需 Bearer Token」，与成长计划接口不同 |

## 灵犀（kdocs）

| 项 | 状态 | 证据 |
|---|---|---|
| `/api/public/v1/tasks` 路径 | 待验证 | 需真实 cookie |
| `type=daily` + `extra.today_day` / `check_in_days` | 待验证 | 逆向文档字符串明示 |
| `status=available` 可领、`claimed` 已领 | 待验证 | 同上 |
| claim 需带 `date=YYYY-MM-DD` | 待验证 | 逆向常量 |

## 百度搭子

| 项 | 状态 | 证据 |
|---|---|---|
| 三个接口路径 | 待验证 | 需真实 cookie |
| 余额 = total − used | 待验证 | 逆向文档字符串明示 |
| 写接口需 `x-bce-request-id` | 待验证 | 逆向注释；当前用 16 字节随机 hex |

## 通用

| 项 | 状态 |
|---|---|
| 401 时网关返回 HTML | **已验证**（WorkBuddy 假 token） |
| 三家平台缺凭据时快速失败不发请求 | **已验证**（单元测试） |

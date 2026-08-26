# BENZHI_README

## 项目说明
- 项目：benzhi-project-322b6c74-8e09-49c1-a857-e32e9728197f
- 项目用途：IceCoreAcclimationGate 提供极地冰芯批次温度适应、偏差隔离重测、污染证据、独立复核、授权解封和摘要归档的版本化 HTTP JSON 服务。
- Go 工具链：`golang:1.22`
- 前端工具链：无

## 项目描述
- 项目名称：IceCoreAcclimationGate
- 项目介绍：面向极地冰芯样品管理员的解封准入服务，将一批低温样品从待规划状态依次推进到温度阶梯适应、偏差闭环、污染控制复核和授权解封，并形成可验证的只读证据档案。
- 项目概述：面向极地冰芯样品管理员的解封准入服务，将一批低温样品从待规划状态依次推进到温度阶梯适应、偏差闭环、污染控制复核和授权解封，并形成可验证的只读证据档案。
- 核心工作流：样品管理员登记冰芯解封批次并制定阶梯升温方案，按检查点提交环境读数；超限批次自动隔离，经纠正和重测恢复后补齐污染空白证据，由独立复核员退回或通过，最终授权解封并冻结完整档案。
- 对外接口：提供版本化 HTTP JSON API，调用方通过同一批次资源提交方案、读数、偏差处置、污染证据、复核和解封命令；服务支持 -addr=127.0.0.1:<port>，默认监听 127.0.0.1:19091，且不得默认绑定 0.0.0.0、8080、80 或 3000。

## 标准构建、运行和测试命令
进入容器后执行：
cd '/app' && GOTOOLCHAIN=local go build ./...

cd /app && GOTOOLCHAIN=local go run ./cmd/server -self-check -addr=127.0.0.1:19091

cd '/app' && GOTOOLCHAIN=local go test ./...

## Docker 构建和进入容器
chmod +x build_benzhi_docker.sh

./build_benzhi_docker.sh benzhi-project-322b6c74-8e09-49c1-a857-e32e9728197f-amd64 linux/amd64

./build_benzhi_docker.sh benzhi-project-322b6c74-8e09-49c1-a857-e32e9728197f-arm64 linux/arm64

docker run -it benzhi-project-322b6c74-8e09-49c1-a857-e32e9728197f-amd64:latest

## 题目验证命令
1. 预期退出码 0：`go test ./...`
2. 预期退出码 0：`go run ./cmd/server -self-check -addr=127.0.0.1:19091`

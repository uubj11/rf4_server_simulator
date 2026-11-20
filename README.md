# rf4_server_simulator

[English](README.en.md)

**由 OpenFisherCommunity 共同维护的开源钓鱼游戏本地服务端模拟器。**

在自己的电脑上，体验从配竿、选饵、抛投和搏鱼，到成长、制作、任务与收藏的钓鱼生活。项目面向本地游玩、学习研究与社区协作，也支持在可信局域网中部署。

[下载正式版](https://github.com/OpenFisherCommunity/rf4_server_simulator/releases/latest) · [体验预览版](https://github.com/OpenFisherCommunity/rf4_server_simulator/releases) · [快速开始](#快速开始) · [交流社区](#交流社区) · [文档中心](docs/README.md)

> **项目免费、开源，持续开发中。** 维护者未收取任何费用，不售卖服务端、整合包或安装服务，也不会开放充值、捐款或赞助。请勿相信冒用项目名义的收费行为。
>
> 这是服务端模拟器，不包含游戏本体；当前适配客户端 **4.0.25034**。

## 现在可以玩什么

以下是当前 `main` 分支已实现的主要玩法与功能。正式版、预览版的具体覆盖范围，请以对应的发布说明为准。

| 方向 | 已实现的玩法与功能 |
| --- | --- |
| **钓鱼与探索** | 浮钓、底钓、路亚、海钓相关链路；地图旅行、租船、多竿与多钩钓组；抛投、咬钩、刺鱼、搏鱼、入护和放生。 |
| **找鱼与诱鱼** | 鱼饵偏好、天气与昼夜鱼口、水层及落点环境、鱼群分布、窝料与 PVA、大鱼狩猎、海图声呐鱼群。部分规则需在后台开启或调整。 |
| **钓具与制作** | 钓组装拆、拟饵换钩、自制引线与窝料、配方模板保存；装备载荷、耐久与搏鱼磨损，以及工坊诊断和修理。 |
| **成长与挑战** | 等级经验、技能与装备加成、升级礼物、成就、资格任务与部分活动任务、欢乐时光。 |
| **图鉴与收藏** | 渔获纪录、稀有鱼图鉴、水域奖杯、鱼种里程碑奖座；卡片掉落、卡册收集、售卡与塑封奖励。 |
| **日常与交易** | 商店购买和退货、鱼市卖鱼、咖啡厅订单、食物饮品与状态恢复、篝火、制作加工、邮件附件、房屋仓储与战利品箱。 |
| **游戏内助手** | 通过「查鱼助手」查询鱼种、鱼饵和区域建议；通过「钓具助手」选择满红不爆保护或耐力播报。 |
| **本地管理** | 启动器统一启停服务、查看日志和启动游戏；Web 后台管理角色、背包、技能、商店、鱼口、饵料规则与邮件。 |

想了解具体机制、开关和适用范围，可以继续阅读[钓鱼机制](docs/gameplay.md)、[管理控制台](docs/admin-console.md)和[文档中心](docs/README.md)。

## 快速开始

### Windows：下载即玩

**不需要安装 Go，也不需要自行编译。**

1. 前往 [Releases](https://github.com/OpenFisherCommunity/rf4_server_simulator/releases/latest)，下载名称以 `windows-amd64.zip` 结尾的发布包，完整解压；不要选择 `Source code`。
2. 准备 **4.0.25034** 客户端，按照包内的 `快速开始.txt` 完成本机证书信任与 hosts 设置。详细步骤见[发布包与客户端接入指南](docs/deploy.md#使用-windows-发布包)。
3. 双击 `启动服务器.cmd`，或运行 `bin\launcher.exe`。打开[本地控制中心](http://127.0.0.1:9500/)，等待游戏服务就绪；首次运行会自动初始化本地数据。
4. 在控制中心保存 `rf4_x64.exe` 的路径，然后点击「启动游戏」。角色和玩法参数可在同一控制中心的管理后台中调整。

想体验最近合入的功能，可在[全部发布](https://github.com/OpenFisherCommunity/rf4_server_simulator/releases)中选择标记为 **Pre-release** 的 Preview。预览版更新更快，也可能包含尚待充分验证的改动。更新前请备份存档和配置，详见[数据与备份](docs/deploy.md#数据)。

### Docker 或源码运行

在仓库根目录，使用 Compose 启动服务端：

```sh
docker compose -f docker/compose.yaml up -d --build --wait
```

Docker 模式的管理后台为 `http://127.0.0.1:8080/`，客户端需另行配置和启动。平台要求、存档与接入步骤见 [Docker Compose 指南](docker/README.md)；另有 [Docker 单容器方案](DOCKER.md)，两种方案任选其一。

自行编译、配置和手动启动，请阅读[部署指南](docs/deploy.md)。

## 当前进展

- **已经打通**：本地登录、地图与角色、钓具装配、钓鱼结算、成长收藏、交易制作及管理后台，正在围绕实际游玩持续补齐细节。
- **持续完善**：不同钓法和装备组合的手感、数值与兼容性，活动及其他非核心系统的覆盖，以及长时间游玩的稳定性。
- **仍有边界**：部分规则采用本地模拟，未完整复刻原版的概率、生态、经济与全部活动；有协议响应也不代表对应玩法已经完整实现。其他客户端版本尚未验证。

最新变化见 [Release 发布说明](https://github.com/OpenFisherCommunity/rf4_server_simulator/releases)，问题与讨论见 [Issues](https://github.com/OpenFisherCommunity/rf4_server_simulator/issues)。欢迎反馈实际游玩体验，也欢迎帮助核对机制和补充测试。

## 交流社区

| 渠道 | 入口 |
| --- | --- |
| **Discord** | [加入 OpenFisherCommunity](https://discord.gg/qaEQEG3TS) |
| **QQ 交流群 3** | `1128152596` |
| **QQ 交流群 4** | `1134665746` |
| QQ 交流群 1 | `379233540`（Release 标注已满） |
| QQ 交流群 2 | `225460820`（Release 标注已满） |
| Bug 与建议 | [GitHub Issues](https://github.com/OpenFisherCommunity/rf4_server_simulator/issues) |

QQ 群信息沿用项目 Release 公布的交流群。反馈问题时，请说明发布版本、客户端版本、复现步骤，并附上已移除账号、token 和个人路径的日志或截图。

欢迎通过 Pull Request 参与开发、完善文档或补充回归测试。提交前请阅读[贡献指南](CONTRIBUTING.md)；开发入口与仓库约定见 [CLAUDE.md](CLAUDE.md)。感谢每一位贡献代码、参与测试、提交问题与帮助验证修复的朋友。

## 文档导航

- **开始游玩与部署**：[部署指南](docs/deploy.md) · [Docker Compose](docker/README.md) · [Docker 单容器](DOCKER.md)
- **玩法与管理**：[钓鱼机制](docs/gameplay.md) · [管理控制台](docs/admin-console.md) · [咖啡厅订单](docs/cafe_orders.md)
- **开发与排查**：[架构概览](docs/architecture.md) · [机制代码索引](docs/mechanism-map.md) · [调试工作流](docs/debugging-workflow.md)
- **完整目录**：[文档中心](docs/README.md)

## Star History

如果项目对你有帮助，欢迎点一颗 Star，或把它分享给同样喜欢钓鱼与开源的朋友。

<a href="https://www.star-history.com/#OpenFisherCommunity/rf4_server_simulator&amp;Date">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=OpenFisherCommunity/rf4_server_simulator&amp;type=Date&amp;theme=dark" />
    <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=OpenFisherCommunity/rf4_server_simulator&amp;type=Date&amp;theme=light" />
    <img alt="rf4_server_simulator 的 Star 增长曲线（手绘风格）" src="https://api.star-history.com/svg?repos=OpenFisherCommunity/rf4_server_simulator&amp;type=Date&amp;theme=light" width="800" />
  </picture>
</a>

## 许可与项目定位

采用 [MIT 许可证](LICENSE)。本项目用于本地游玩、学习研究与可信局域网交流，不以私服运营或商业服务为目标；不提供游戏本体或未经授权的客户端资源。请遵守适用的法律法规、游戏服务条款及相关权利人的要求。

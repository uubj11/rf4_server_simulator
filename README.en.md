# rf4_server_simulator

[中文](README.md)

**An open-source local server simulator for the fishing game, maintained by OpenFisherCommunity.**

Play a fishing life on your own computer, from rigging, bait selection, casting, and fighting fish to progression, crafting, quests, and collecting. The project is intended for local play, learning, research, and community collaboration, and can also run on a trusted LAN.

[Download stable release](https://github.com/OpenFisherCommunity/rf4_server_simulator/releases/latest) · [Try a preview release](https://github.com/OpenFisherCommunity/rf4_server_simulator/releases) · [Quick start](#quick-start) · [Community](#community) · [Documentation](docs/README.md)

> **Free, open source, and under active development.** The maintainers do not charge for the project, sell the server, bundles, or installation services, or accept payments, donations, or sponsorships. Be careful of anyone charging under the project name.
>
> This is a server simulator and does not include the game client. The current compatibility target is **4.0.25034**.

## What you can play today

The following is the main gameplay and functionality implemented on the current `main` branch. Check the release notes for the exact scope of each stable or preview release.

| Area | Implemented gameplay and functionality |
| --- | --- |
| **Fishing and exploration** | Float, bottom, lure, and sea-fishing related flows; map travel, boat rental, multiple rods and multi-hook rigs; casting, bites, hook sets, fights, keeping, and releasing fish. |
| **Finding and attracting fish** | Bait preferences, weather and day/night bite conditions, depth and spot environments, fish distribution, groundbait and PVA, trophy hunting, and sonar fish groups on sea charts. Some rules must be enabled or adjusted in the admin panel. |
| **Tackle and crafting** | Assemble and disassemble rigs, change lure hooks, craft leaders and groundbait, save recipe templates, and handle equipment load, durability, fight wear, workshop diagnostics, and repairs. |
| **Progression and challenges** | Level experience, skills and equipment bonuses, level-up gifts, achievements, qualification quests, selected event quests, and happy hours. |
| **Collections and records** | Catch records, rare-fish collections, water trophies, species milestone awards, card drops, album collection, card sales, and sealing rewards. |
| **Daily life and trading** | Shop purchases and returns, fish market sales, café orders, food and drinks with status recovery, campfires, processing, mail attachments, house storage, and loot boxes. |
| **In-game helpers** | The Fish Finder suggests fish, bait, and areas; the Tackle Assistant can select full-red protection or announce stamina status. |
| **Local administration** | The launcher starts and stops services, shows logs, and starts the game; the Web admin panel manages characters, inventory, skills, shops, bite and bait rules, and mail. |

For detailed mechanics, switches, and supported scope, see [Fishing mechanics](docs/gameplay.md), [Admin console](docs/admin-console.md), and the [Documentation index](docs/README.md).

## Quick start

### Windows: download and play

**You do not need to install Go or build the project yourself.**

1. Open [Releases](https://github.com/OpenFisherCommunity/rf4_server_simulator/releases/latest) and download the Windows x64 ZIP ending in `windows-amd64.zip`. Extract it completely; do not choose `Source code`.
2. Prepare the **4.0.25034** client and follow the package's `快速开始.txt` for local certificate trust and hosts setup. See the [Windows release package guide](docs/deploy.md#使用-windows-发布包) for details.
3. Double-click `启动服务器.cmd`, or run `bin\launcher.exe`. Open the [local control center](http://127.0.0.1:9500/) and wait for the game services to become ready. Local data is initialized on the first run.
4. Save the path to `rf4_x64.exe` in the control center, then click **Start game**. The same control center provides access to the admin panel for characters and gameplay settings.

To try recently merged features, choose a release marked **Pre-release** on the [all releases](https://github.com/OpenFisherCommunity/rf4_server_simulator/releases) page. Preview releases update faster and may include changes that have not received as much validation. Back up your save data and configuration before updating; see [Data and backups](docs/deploy.md#数据).

### Docker or source deployment

From the repository root, start the server with Compose:

```sh
docker compose -f docker/compose.yaml up -d --build --wait
```

The Docker admin panel is available at `http://127.0.0.1:8080/`. Configure and start the client separately. See the [Docker Compose guide](docker/README.md) for platform requirements, saves, and client access. A [single-container option](DOCKER.md) is also available.

For a source build, configuration, and manual startup, read the [deployment guide](docs/deploy.md).

## Current progress

- **Core loop working**: local login, maps and characters, tackle assembly, fishing settlement, progression and collections, trading and crafting, and the admin panel are connected. We continue to refine them through real play.
- **Being refined**: the feel, values, and compatibility of different fishing styles and equipment combinations; coverage of events and other non-core systems; and long-session stability.
- **Known boundaries**: some rules are local simulations and do not fully reproduce the original probabilities, ecology, economy, or every event. A protocol response alone does not mean the corresponding gameplay is complete. Other client versions have not been verified.

See [release notes](https://github.com/OpenFisherCommunity/rf4_server_simulator/releases) for recent changes and [Issues](https://github.com/OpenFisherCommunity/rf4_server_simulator/issues) for problems and discussions. Reports of real gameplay, mechanism checks, and regression tests are welcome.

## Community

| Channel | Entry |
| --- | --- |
| **Discord** | [Join OpenFisherCommunity](https://discord.gg/qaEQEG3TS) |
| **QQ group 3** | `1128152596` |
| **QQ group 4** | `1134665746` |
| QQ group 1 | `379233540` (marked full in the release notes) |
| QQ group 2 | `225460820` (marked full in the release notes) |
| Bugs and suggestions | [GitHub Issues](https://github.com/OpenFisherCommunity/rf4_server_simulator/issues) |

The QQ group information comes from the project release announcements. When reporting a problem, include the release version, client version, reproduction steps, and logs or screenshots with account IDs, tokens, and personal paths removed.

Pull requests are welcome for code, documentation, and regression coverage. Please read the [contribution guide](CONTRIBUTING.md); repository conventions are documented in [CLAUDE.md](CLAUDE.md).

## Documentation

- **Play and deploy**: [Deployment guide](docs/deploy.md) · [Docker Compose](docker/README.md) · [Single-container Docker](DOCKER.md)
- **Gameplay and administration**: [Fishing mechanics](docs/gameplay.md) · [Admin console](docs/admin-console.md) · [Café orders](docs/cafe_orders.md)
- **Development and debugging**: [Architecture](docs/architecture.md) · [Mechanism map](docs/mechanism-map.md) · [Debugging workflow](docs/debugging-workflow.md)
- **Complete index**: [Documentation center](docs/README.md)

## Star History

If this project helps you, please consider giving it a Star or sharing it with people who enjoy fishing and open source.

<a href="https://www.star-history.com/#OpenFisherCommunity/rf4_server_simulator&amp;Date">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=OpenFisherCommunity/rf4_server_simulator&amp;type=Date&amp;theme=dark" />
    <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=OpenFisherCommunity/rf4_server_simulator&amp;type=Date&amp;theme=light" />
    <img alt="Hand-drawn-style Star History chart for rf4_server_simulator" src="https://api.star-history.com/svg?repos=OpenFisherCommunity/rf4_server_simulator&amp;type=Date&amp;theme=light" width="800" />
  </picture>
</a>

## License and project scope

Licensed under the [MIT License](LICENSE). This project is intended for local play, learning, research, and trusted-LAN collaboration. It is not intended to operate a private server or commercial service, and it does not provide the game client or unauthorized client assets. Follow applicable laws, game service terms, and the requirements of the relevant rights holders.

# Issue tracker: Local Markdown

Issues and specs for this repo live as markdown files under `docs/`.

## Conventions

- One feature per directory: `docs/<feature-slug>/`
- The spec is `docs/<feature-slug>/spec.md`
- Implementation issues are one file per ticket at `docs/<feature-slug>/issues/<NN>-<slug>.md`, numbered from `01` — never a single combined tickets file
- Triage state is recorded as a `Status:` line near the top of each issue file (see `triage-labels.md` for the role strings)
- Comments and conversation history append to the bottom of the file under a `## Comments` heading

当前生效的 effort 是 `docs/archive/go-rewrite/`：目录名沿用归档时的命名，但 effort 未关闭（E8 尾巴仍在跟踪）。新建 effort 走 `docs/<effort-slug>/`。

## When a skill says "publish to the issue tracker"

Create a new file under `docs/<feature-slug>/` (creating the directory if needed).

## When a skill says "fetch the relevant ticket"

Read the file at the referenced path. The user will normally pass the path or the issue number directly.

## Wayfinding operations

Used by `/wayfinder`. The **map** is a file with one **child** file per ticket.

- **Map**: `docs/<effort>/map.md` — the Notes / Decisions-so-far / Fog body.
- **Child ticket**: `docs/<effort>/issues/NN-<slug>.md`, numbered from `01`, with the question in the body. A `Type:` line records the ticket type (`research`/`prototype`/`grilling`/`task`); a `Status:` line records `claimed`/`resolved`.
- **Blocking**: a `Blocked by: NN, NN` line near the top. A ticket is unblocked when every file it lists is `resolved`.
- **Frontier**: scan `docs/<effort>/issues/` for files that are open, unblocked, and unclaimed; first by number wins.
- **Claim**: set `Status: claimed` and save before any work.
- **Resolve**: append the answer under an `## Answer` heading, set `Status: resolved`, then append a context pointer (gist + link) to the map's Decisions-so-far in `map.md`.

---

*Setup decision (2026-09-06): 本仓库根目录无 git remote(非 GitHub/GitLab 仓库),工作区由三个独立子项目组成,经用户确认为 **Local markdown** 后端。*

*更新 (2026-09-19): 仓库根现已上提为单 Go 模块并存在 GitHub remote(`git@github.com:Agentropism/vtuber-agent-go.git`)。追踪后端仍为 Local markdown —— 票面与代码同仓评审,不走 GitHub Issues。*

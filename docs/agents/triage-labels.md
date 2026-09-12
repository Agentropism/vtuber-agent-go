# Triage Labels

The skills speak in terms of five canonical triage roles. This file maps those roles to the actual label strings used in this repo's issue tracker.

| Label in mattpocock/skills | Label in our tracker | Meaning                                  |
| -------------------------- | -------------------- | ---------------------------------------- |
| `needs-triage`             | `needs-triage`       | Maintainer needs to evaluate this issue  |
| `needs-info`               | `needs-info`         | Waiting on reporter for more information |
| `ready-for-agent`          | `ready-for-agent`    | Fully specified, ready for an AFK agent  |
| `ready-for-human`          | `ready-for-human`    | Requires human implementation            |
| `wontfix`                  | `wontfix`            | Will not be actioned                     |

When a skill mentions a role (e.g. "apply the AFK-ready triage label"), use the corresponding label string from this table.

Edit the right-hand column to match whatever vocabulary you actually use.

---

## 标签调色盘(Label palette)

MattSkillsDeck 在本地 Markdown 后端依据下表为标签提供色值(颜色)显示。**请勿删除本表或其中的行**——删除会导致标签色值功能损毁(未收录的标签将无法显示颜色)。自定义标签在本表加一行;想改颜色就改对应行的 Color 值。

| Label | Color | Meaning |
| --- | --- | --- |
| wayfinder:map | #8b5cf6 | The map issue of a wayfinder effort |
| wayfinder:research | #0ea5e9 | Research ticket (AFK) |
| wayfinder:prototype | #f59e0b | Prototype ticket (HITL) |
| wayfinder:grilling | #9d7cd8 | Grilling / discussion ticket (HITL) |
| wayfinder:task | #10b981 | Task ticket (HITL or AFK) |
| bug | #d73a4a | Something is broken (fix action / BUG filter) |
| needs-triage | #fbca04 | Unexamined issue awaiting diagnosis |
| needs-info | #5319e7 | Waiting on reporter for more information |
| ready-for-agent | #0e8a16 | Fully specified, ready for an AFK agent |
| ready-for-human | #b60205 | Requires human implementation |
| wontfix | #ffffff | Will not be actioned |
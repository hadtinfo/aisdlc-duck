---
name: AISDLC Web
description: The lead's control desk for AI-assisted delivery — board, gate review, live agent tracking.
colors:
  signal-blue: "#2f5fd0"
  signal-blue-ink: "#ffffff"
  cool-paper: "#f6f7f9"
  surface: "#ffffff"
  surface-sunken: "#eef0f4"
  hairline: "#dfe3ea"
  ink: "#1c2330"
  slate-muted: "#5d6679"
  pass-green: "#167343"
  pass-green-wash: "#e3f4ea"
  caution-amber: "#8a5800"
  caution-amber-wash: "#fdf1d6"
  block-red: "#b42318"
  block-red-wash: "#fde6e3"
  info-wash: "#e5ecfb"
  dark-cool-paper: "#12151b"
  dark-surface: "#1a1e26"
  dark-surface-sunken: "#232834"
  dark-hairline: "#2e3442"
  dark-ink: "#e6e9ef"
  dark-slate-muted: "#98a2b3"
  dark-signal-blue: "#6c93f0"
  dark-signal-blue-ink: "#0e1320"
  dark-pass-green: "#5fd193"
  dark-pass-green-wash: "#16301f"
  dark-caution-amber: "#f0b955"
  dark-caution-amber-wash: "#33280f"
  dark-block-red: "#f58a7f"
  dark-block-red-wash: "#3a1a17"
  dark-info: "#8fb0f7"
  dark-info-wash: "#1b2744"
typography:
  headline:
    fontFamily: "system-ui, -apple-system, Segoe UI, Roboto, Noto Sans, sans-serif"
    fontSize: "22px"
    fontWeight: 600
    lineHeight: 1.3
    letterSpacing: "0.02em"
  title:
    fontFamily: "system-ui, -apple-system, Segoe UI, Roboto, Noto Sans, sans-serif"
    fontSize: "17.5px"
    fontWeight: 600
    lineHeight: 1.35
  body:
    fontFamily: "system-ui, -apple-system, Segoe UI, Roboto, Noto Sans, sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.45
  body-small:
    fontFamily: "system-ui, -apple-system, Segoe UI, Roboto, Noto Sans, sans-serif"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.45
  label:
    fontFamily: "system-ui, -apple-system, Segoe UI, Roboto, Noto Sans, sans-serif"
    fontSize: "12px"
    fontWeight: 600
    lineHeight: 1.3
    letterSpacing: "0.06em"
  metric:
    fontFamily: "system-ui, -apple-system, Segoe UI, Roboto, Noto Sans, sans-serif"
    fontSize: "18px"
    fontWeight: 700
    lineHeight: 1.2
    fontFeature: "tnum"
  mono:
    fontFamily: "ui-monospace, SF Mono, Menlo, Consolas, monospace"
    fontSize: "12.5px"
    fontWeight: 400
    lineHeight: 1.5
rounded:
  xs: "4px"
  sm: "6px"
  md: "8px"
  lg: "12px"
  pill: "999px"
spacing:
  xxs: "4px"
  xs: "8px"
  sm: "12px"
  md: "16px"
  lg: "24px"
components:
  button-primary:
    backgroundColor: "{colors.signal-blue}"
    textColor: "{colors.signal-blue-ink}"
    rounded: "{rounded.sm}"
    padding: "7px 14px"
  button-secondary:
    backgroundColor: "transparent"
    textColor: "{colors.ink}"
    rounded: "{rounded.sm}"
    padding: "7px 14px"
  button-secondary-hover:
    backgroundColor: "{colors.surface-sunken}"
  button-ghost:
    backgroundColor: "transparent"
    textColor: "{colors.signal-blue}"
    rounded: "{rounded.sm}"
    padding: "3px 10px"
  button-ghost-danger:
    backgroundColor: "transparent"
    textColor: "{colors.block-red}"
    rounded: "{rounded.sm}"
    padding: "3px 10px"
  button-ghost-danger-hover:
    backgroundColor: "{colors.block-red-wash}"
  input:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    rounded: "{rounded.sm}"
    padding: "6px 8px"
  feature-card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    rounded: "7px"
    padding: "10px 12px"
  board-column:
    backgroundColor: "{colors.surface-sunken}"
    rounded: "{rounded.md}"
    padding: "10px"
  panel:
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.md}"
    padding: "16px 20px"
  tag:
    backgroundColor: "{colors.surface-sunken}"
    textColor: "{colors.slate-muted}"
    typography: "{typography.label}"
    rounded: "{rounded.xs}"
    padding: "1px 6px"
  status-pill-pass:
    backgroundColor: "{colors.pass-green-wash}"
    textColor: "{colors.pass-green}"
    rounded: "{rounded.pill}"
    padding: "2px 10px"
  status-pill-caution:
    backgroundColor: "{colors.caution-amber-wash}"
    textColor: "{colors.caution-amber}"
    rounded: "{rounded.pill}"
    padding: "2px 10px"
  status-pill-block:
    backgroundColor: "{colors.block-red-wash}"
    textColor: "{colors.block-red}"
    rounded: "{rounded.pill}"
    padding: "2px 10px"
  gate-item-active:
    backgroundColor: "{colors.info-wash}"
    textColor: "{colors.signal-blue}"
    rounded: "{rounded.sm}"
    padding: "8px"
  summary-tile:
    backgroundColor: "{colors.surface}"
    typography: "{typography.metric}"
    padding: "10px 14px"
---

# Design System: AISDLC Web

## Overview

**Creative North Star: "The Control Desk"**

AISDLC Web is the desk a lead sits at to see where every feature stands, read what the agent produced, and decide: approve, reject, accept a step, allow a file. The interface is an instrument panel, not a showcase. It is calm and exact: cool-grey paper, a single blue for action and selection, and three signal colours (pass green, caution amber, block red) that only appear when they mean something. Information density is high, but every row has air and every number is tabular.

Depth comes from tone, not shadow. Surfaces sit on a cool-paper ground, sunken wells (board columns, code, tags) step one shade darker, and hairline borders draw the edges. Shadows are a 1px whisper in light mode and disappear entirely in dark mode. Motion is limited to state feedback: a 1px lift on hoverable cards, a progress bar easing to its value, a pulsing dot for a live agent session (off under reduced motion).

The UI is bilingual (Vietnamese and English) and shares its vocabulary with the CLI. Copy, layout and widths must survive Vietnamese strings that run longer than English ones.

**Key Characteristics:**
- One action colour (signal blue); green, amber and red are reserved for status.
- Flat tonal layering: paper → surface → sunken well, separated by hairlines.
- System font stack at a 14px base; monospace for hashes, paths, commands and diffs.
- Small, restrained radii (4–8px); pills only for status and counts.
- Severity is stated twice — colour plus a text label — never by colour alone.
- Full light and dark themes driven by the same CSS custom properties.

## Colors

A cool, low-chroma neutral world with one blue voice and a strict traffic-light vocabulary for status.

### Primary
- **Signal Blue** (`signal-blue`): every primary button, link, focus ring, the selected gate in the review list, feature keys on board cards, and the outline of the "Waiting for me" inbox. It also doubles as the info tone (`--info`), so "informational" and "actionable" read as one family.

### Tertiary (status)
- **Pass Green** (`pass-green` on `pass-green-wash`): eval PASS, accepted steps, within-budget bars, the live-session dot.
- **Caution Amber** (`caution-amber` on `caution-amber-wash`): awaiting-dev steps, WARN flags, budget overruns under 150%, the one-time new-token reveal.
- **Block Red** (`block-red` on `block-red-wash`): HIGH flags, out-of-scope files, eval FAIL, rejected gates, destructive actions (revoke token).

Each status colour pairs with a wash of the same hue for backgrounds; text on a wash always uses the full-strength colour of the same family.

### Neutral
- **Cool Paper** (`cool-paper`): the page ground.
- **Surface** (`surface`): cards, panels, the review columns, inputs.
- **Sunken Well** (`surface-sunken`): board columns, code spans, tags, hover fill on list items and secondary buttons.
- **Hairline** (`hairline`): every border and divider.
- **Ink** (`ink`): body text and headings.
- **Slate Muted** (`slate-muted`): secondary text, uppercase labels, metadata. Tuned to stay above 4.5:1 on both paper and surface.

### Named Rules
**The Signal-Only Rule.** Green, amber and red mean pass, caution and block. They are never used for decoration, branding or emphasis.

**The Approved-Still-Red Rule.** A HIGH or WARN flag on an approved gate keeps its colour as a reminder; approval is shown by an added "accepted at approval @hash" note, not by greying the flag out.

**The 4.5 Floor Rule.** Every text/background pair, including muted text and status text on its wash, holds at least 4.5:1 in both themes. Darken the token, never the one-off instance.

## Typography

**Body Font:** the platform system UI stack (system-ui, -apple-system, Segoe UI, Roboto, Noto Sans).
**Label/Mono Font:** ui-monospace, SF Mono, Menlo, Consolas.

**Character:** native and invisible. The sans gets out of the way of the content; the mono marks anything a user might copy into a terminal or compare byte-for-byte.

### Hierarchy
- **Headline** (600, 22px): page titles — the AISDLC top bar, the feature key on the review page.
- **Title** (600, 17.5px): section headings inside panels and boxes, the active gate's heading.
- **Body** (400, 14px, 1.45): default text. Rendered markdown (BRD, design) relaxes to 1.6 line-height and caps at 90ch; agent summaries cap at 75ch.
- **Body Small** (400, 13–13.5px): card titles, flag text, event rows.
- **Label** (600, 12px, 0.06–0.07em tracking, uppercase): board column headers, gate-list heading, summary tile labels, flag levels. Never below 12px.
- **Metric** (700, 18px, tabular numerals): the values in the review summary strip.
- **Mono** (12.5px; 12px inside diffs): hashes, paths, commands, code spans.

### Named Rules
**The Copyable-Is-Mono Rule.** Anything the user may paste into a terminal or compare (commands, paths, hashes, step ids in commands) is set in mono; prose never is.

**The Twelve Floor Rule.** No UI text below 12px. The only exception is secondary annotation text inside SVG diagrams.

## Layout

Pages sit in a centred container (max 1800px, 16px/24px padding; narrow settings pages cap at 1100px). The spacing rhythm is 4 / 8 / 12 / 16 / 24px: 8px between cards, 12px between board columns, 16px between review columns and stacked boxes.

- **Board:** six equal columns (min 190px each, 12px gap). Below 1280px the board scrolls horizontally inside its own frame rather than the page; below 900px the columns stack into one vertical list.
- **Review:** three columns — gate/step list (220px, sticky), main reading column (fluid), gate panel (320px). Below 1200px the side columns shrink to 190px/280px; below 900px everything stacks into one column.
- **Live tracking:** a two-column grid that collapses to one column below 1280px.
- **Top bar:** title, filters and the user menu wrap rather than overflow; below 900px the filters drop to their own full-width row.

No page may scroll horizontally at 390px.

## Elevation & Depth

The system is flat and tonal. Depth is conveyed by three ground levels (paper, surface, sunken well) and hairline borders. A single ambient shadow exists only to lift surfaces off the paper in light mode and is removed in dark mode, where tone alone separates layers.

### Shadow Vocabulary
- **Hairline lift** (`box-shadow: 0 1px 2px rgba(16, 24, 40, 0.06)`): cards, panels, the review columns, the login box. Dark mode: `none`.
- **Status halo** (`box-shadow: 0 0 0 3px <status-wash>`): the small status dot on a board card.

### Named Rules
**The Flat-By-Default Rule.** Surfaces are flat at rest. The only motion-based depth is a 1px upward shift plus an accent border on hoverable feature cards.

## Shapes

Small, consistent radii keep the desk feeling precise: 4px for tags and inline code, 6px for buttons, inputs, code blocks and list items, 8px for cards, columns and panels, 12px only for the standalone login box. Full pills (999px) are reserved for status pills, counts and the live-session indicator. Status dots are 8–10px circles; an idle status is a hollow ring rather than a grey fill.

Borders are always 1px hairlines, except the 3px **severity bar** on the leading edge of flags, gate-panel flag rows and out-of-scope event rows.

## Components

### Buttons
Compact and quiet; only the primary button carries colour.
- **Shape:** gently rounded (6px).
- **Primary:** signal blue fill, white ink, 600 weight, 7px × 14px padding.
- **Secondary:** transparent with a hairline border and ink text; sunken-well fill on hover. Used for Cancel and other non-committal actions.
- **Ghost:** transparent, blue text, hairline border, 12px text, 3px × 10px padding — for inline row actions.
- **Ghost Danger:** ghost with red text; red border and red wash on hover — for destructive row actions such as Revoke.
- **Focus:** a 2px signal-blue outline offset by 2px on every focusable element. Disabled is 50% opacity.
- **Confirmation:** destructive or gate-changing actions open an inline confirm group (never a modal). Focus lands on Cancel, Esc closes it.

### Tags, Labels and Pills
- **Tag:** 12px semibold on the sunken well, muted text, 4px radius — for neutral metadata (repo, preset, mode).
- **Label / Status pill:** status colour on its own wash, pill-shaped — for risk level, step state, eval result and badges.

### Cards / Containers
- **Feature card:** surface, hairline border, 7px radius, 10px × 12px padding, hairline lift. Hover: accent border + 1px lift. The header holds the feature key (blue, 700) and risk label; the footer holds repo, owner and sync time in 11.5–12px muted text above a hairline divider.
- **Board column:** sunken well, 8px radius, uppercase label header with a count chip; an empty column says so in muted text.
- **Panel / Box:** surface, hairline border, 8px radius, 16px × 20px padding, title-size heading.
- **Inbox ("Waiting for me"):** a panel whose full border is signal blue and whose count chip is filled blue — the only panel that announces itself.

### Inputs / Fields
- **Style:** surface fill, hairline border, 6px radius, 6px × 8px padding, inherits the body font.
- **Focus:** the global 2px blue focus outline.

### Navigation
- **Gate/step list (review):** a vertical list of full-width rows (6px radius) with a status dot, name and muted sub-line. Hover fills with the sunken well; the active row uses the info wash with blue, semibold text. Arrow keys and j/k move the selection; Home/End jump to the ends.
- **Tabs (risk policy):** text tabs on a hairline baseline; the current tab gets ink text and a 2px blue underline.

### Review Summary Strip (signature)
A row of equal tiles separated by 1px hairline gaps inside one 8px-radius frame. Each tile shows an uppercase label, an 18px tabular metric (coloured by status only when it carries a signal) and a muted sub-line. Unknown values show "?" with an explanation rather than a misleading zero.

### Flag (signature)
A severity row: 3px leading bar in the status colour, the status wash behind, 0 6px 6px 0 radius. It shows an uppercase level label in the status colour, the message in 13px ink, an optional muted hint and mono references. On an approved gate, the level gains "· accepted" in ink.

## Do's and Don'ts

### Do:
- **Do** use signal blue for exactly one meaning: "this is actionable or selected".
- **Do** pair every status colour with a text label (PASS, HIGH, "Awaiting dev") so meaning never depends on colour alone.
- **Do** set hashes, paths and commands in mono, and numbers in tabular figures.
- **Do** define every colour through the CSS custom properties in `web/ui/src/styles.css` and give it a dark-mode value in the same file.
- **Do** keep confirmations inline, focus Cancel first, and close them with Esc.
- **Do** check every new screen at 390px, 1024px and 1440px in both themes.

### Don't:
- **Don't** use a coloured leading border (border-left wider than 1px) on cards, panels or alerts; the 3px bar is reserved for severity rows only.
- **Don't** set UI text below 12px or let muted text fall under 4.5:1 contrast.
- **Don't** add drop shadows beyond the hairline lift, and never in dark mode.
- **Don't** introduce a second accent hue or use status colours decoratively.
- **Don't** grey out or hide flags after a gate is approved; annotate them as accepted.
- **Don't** fix the page to a minimum width; layouts must reflow down to 390px.

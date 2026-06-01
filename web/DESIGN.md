# DESIGN.md — OmniHub admin console

Visual system for the embedded React admin. Restrained product palette, cool neutrals, one indigo accent, light + dark first-class. Tokens live as CSS variables in `src/index.css` and are mapped to Tailwind semantic colors in `tailwind.config.js`.

## Color (OKLCH)

Color strategy: **Restrained** — tinted-cool neutrals + a single accent ≤ 10% of surface. The mood lives in the accent and typography, never in the body background.

Brand hue deviates from the random seed (olive 130°) on purpose: a green brand would collide with the semantic "enabled/success" green. Indigo-violet (~277°) keeps the accent distinct from state colors and reads as infrastructure-grade.

### Light
| Role | OKLCH | Use |
|------|-------|-----|
| `bg` | `1 0 0` (pure white) | page background |
| `surface` | `1 0 0` | cards, table body |
| `surface-2` | `0.985 0.003 270` | nav bar, table header, inset panels |
| `border` | `0.922 0.004 270` | hairlines, dividers |
| `ink` | `0.23 0.02 277` | primary text |
| `muted` | `0.55 0.02 277` | secondary text (≥4.5:1 on surface) |
| `brand` | `0.55 0.16 277` | primary action, active nav, focus, selection |
| `brand-ink` | `0.99 0 0` | text on brand |

### Dark
| Role | OKLCH | Use |
|------|-------|-----|
| `bg` | `0.17 0.012 277` | page background (cool near-black, not pure black) |
| `surface` | `0.205 0.013 277` | cards, table body |
| `surface-2` | `0.235 0.013 277` | nav bar, table header |
| `border` | `0.30 0.012 277` | hairlines |
| `ink` | `0.96 0.005 277` | primary text |
| `muted` | `0.70 0.015 277` | secondary text (≥4.5:1 on surface) |
| `brand` | `0.67 0.15 277` | accent (lightened for dark) |
| `brand-ink` | `0.17 0.01 277` | text on brand |

### Semantic (both themes, tuned per mode)
- `success` green ~150°, `warning` amber ~75°, `danger` red ~25°, `info` blue ~250°. Used only for state (status badges, error text, destructive actions). Never decorative, never the brand.

## Typography

- **One family**: Inter (variable, self-hosted via `@fontsource-variable/inter`) for everything — headings, labels, body, data. `ui-monospace` for keys, IPs, model names, and token values.
- Fixed rem scale, ratio ~1.2: `text-xs` labels, `text-sm` body/table, `text-base`/`text-lg` section titles, `text-xl` page title, `text-2xl` stat numbers. No fluid clamp in app UI.
- `tabular-nums` on all numbers in tables and stats. `-0.01em` tracking on headings; never tighter.

## Shape & spacing

- Radius: `8px` inputs/buttons, `12px` cards/panels, `9999px` pills/badges. Never above 16px on a card.
- Hairline borders only (`1px`, `border` token). Pick border OR a soft shadow, never both ("ghost card" ban). Cards: border only. Elevated surfaces (modal, dropdown): shadow only.
- Spacing rhythm on a 4px base; generous section gaps (`24–40px`), tight control padding.

## Components

- **Button**: `.btn` base + `.btn-primary` (brand fill), `.btn-secondary` (bordered surface), `.btn-ghost` (text), `.btn-danger`. All share height, radius, focus ring; each defines hover/active/disabled.
- **Input / select**: `.field` — surface bg, border, `focus-visible` 2px brand ring, no glow.
- **Card**: `.card` — surface, 1px border, 12px radius.
- **Badge**: `.badge` + tone modifiers (brand / success / warning / danger / neutral).
- **Table**: `surface-2` header, hairline row dividers, row hover tint, no zebra. Sticky header for the long price table.
- **Modal**: centered dialog, backdrop with subtle blur + dim, panel shadow (no border), `Esc`/backdrop close.

## Motion

- 150–200ms, `ease-out` (quart-ish) on hover/focus/color/transform. No bounce, no elastic.
- Motion conveys state only: button/row hover, focus ring, modal in (fade + 4px rise), badge color. No page-load choreography.
- Every transition has a `@media (prefers-reduced-motion: reduce)` off-switch (global base rule).

## Theme switching

`darkMode: 'class'` on `<html>`. A small toggle in the nav cycles system → light → dark and persists to `localStorage`; absent an override, the OS preference wins (set on boot before paint to avoid flash).

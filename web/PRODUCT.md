# PRODUCT.md — OmniHub admin console

## Register

**Product.** This is an authenticated operator console (app UI), not a marketing surface. Design serves the task; earned familiarity beats novelty. The bar: an operator fluent in Linear / Vercel / Stripe dashboards should trust it on sight.

## Users & purpose

Operators and engineers running an OmniHub AI gateway. They come to: add upstream provider accounts, mint and revoke virtual API keys, block or rate-cap IPs, watch usage and spend, check circuit-breaker health, and manage model pricing. They are usually mid-incident or mid-config — they want to find the control, act, and leave. Density and clarity over hand-holding.

## Brand personality

Precise · quiet · trustworthy. The console should feel like infrastructure: calm surfaces, sharp hierarchy, no decoration that doesn't carry information. Polish shows up in spacing, focus states, and motion timing, not in color volume.

## References (the specific thing that fits)

- **Linear** — restraint and density done well; cool neutral surfaces, one confident accent, crisp keyboard-grade focus rings.
- **Vercel dashboard** — near-monochrome calm with a single sharp accent; tables that stay readable at high row counts.
- **Stripe** — semantic state vocabulary (success/warn/danger) that never competes with the brand accent.

## Anti-references

- Not a consumer SaaS landing page: no hero metrics, no gradient text, no oversized display type, no orchestrated load animations.
- Not "AI-cream": no warm beige/parchment body bg.
- Not playful: no rounded-32px cards, no doodle illustrations, no bouncy motion.

## Strategic principles

1. **The accent means something.** Indigo is reserved for primary actions, the current nav item, focus, and selection. State uses its own green/amber/red so "enabled" never reads as "brand".
2. **Every control has all its states** — default, hover, focus-visible, active, disabled, loading. Half a button is a bug.
3. **Tables are the product.** They must stay legible dense; the price table renders 2,000+ rows.
4. **Consistency over surprise.** Same button, same input, same badge, same modal across all seven pages.

## Accessibility

Body text ≥ 4.5:1 in both themes; large/secondary text ≥ 3:1. Visible `focus-visible` ring on every interactive element. Full `prefers-reduced-motion` fallback. Light and dark are both first-class (system-preference driven, with a manual override).

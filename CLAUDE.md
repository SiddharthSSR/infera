# INFERA.AI Frontend — Design System & Refinement Rules

You are refining the existing frontend for INFERA.AI, an LLM inference serving platform.
The UI already exists — your job is to IMPROVE, POLISH, and COMPONENTIZE it, not redesign from scratch.
Preserve the existing design language with perfect fidelity while elevating quality.

## Project Context

INFERA.AI is an LLM inference platform. The frontend is a dashboard-style application with these views:
- **Dashboard** (`/`) — Inference metrics (requests, latency, throughput, active nodes), deployed models, quick config, system logs
- **Registry** (`/models`) — Model registry table with status, quantization, context, search, deploy actions
- **Instances** (`/instances`) — GPU node overview table, scaling controls, cluster health, metrics (GPU util, memory, uptime)
- **Playground** (`/playground`) — 3-column layout: model params sidebar, prompt/response editor, request history sidebar
- **API Keys** (`/api-keys`) — Key table with create form, scopes, quota, encryption info
- **System Logs** (`/logs`) — Filterable log table with levels (INFO/WARN/ERROR/DEBUG), sources, live stream status

## Brand Identity — DO NOT DEVIATE

The INFERA.AI aesthetic is: **Technical. Minimal. Precise.**
Rooted in engineering schematics and architectural paper warmth.
Clarity of data, rigid structural grids, high-contrast typography.

### Design Tokens (MANDATORY — use these exact values)
```css
:root {
    /* Backgrounds */
    --bg-paper: #FDFBF8;          /* Primary — warm off-white, like architectural paper */
    --bg-accent: #F4F2EE;         /* Secondary panels, sidebars, accent sections */

    /* Text */
    --text-primary: #050505;       /* Ink black — headings, primary content */
    --text-secondary: #555555;     /* Muted — labels, descriptions, meta */

    /* Borders & Structure */
    --border-color: #D8D6D4;      /* Grid lines, dividers */
    --grid-line: 1px solid var(--border-color);

    /* Semantic */
    --color-success: #2E7D32;     /* Operational / Active / Running */
    --color-warning: #F9A825;     /* Deploying / Throttling / Latency */
    --color-error: #C62828;       /* Error / Revoke / Critical */

    /* Typography */
    --font-main: "DM Sans", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    --font-mono: "Space Mono", monospace;
}
```

### Typography Hierarchy (STRICT)

| Role              | Font       | Size    | Weight | Transform  | Tracking   |
|-------------------|-----------|---------|--------|------------|------------|
| Display Header    | DM Sans   | 6rem    | 600    | UPPERCASE  | -0.04em    |
| Section Title     | DM Sans   | 1.75rem | 400    | None       | -0.02em    |
| Model Name        | DM Sans   | 1.25rem | 500    | None       | -0.02em    |
| Body Copy         | DM Sans   | 1rem    | 400    | None       | normal     |
| Label Text        | DM Sans   | 0.65rem | 600    | UPPERCASE  | +0.1em     |
| Nav Link          | DM Sans   | 0.7rem  | 600    | UPPERCASE  | +0.15em    |
| Mono Data         | Space Mono| 0.85rem | 400    | None       | normal     |
| Mono Small        | Space Mono| 0.75rem | 400    | None       | normal     |

### Layout System

- **4-column flexible grid** with `grid-template-columns: repeat(4, 1fr)`
- Cells: `padding: 2rem`, separated by `border-right: var(--grid-line)`
- Grid span classes: `.grid-col-2`, `.grid-col-3`, `.grid-col-4`
- Max width: `1280px` (1600px on dashboard)
- Container: centered with left/right border lines, `border-radius: 20px`, `overflow: hidden`
- Responsive breakpoint at 1024px: collapse to 2-column grid

### Navigation Pattern

- Top nav bar: logo left, main links center (separated by ◇ diamond), utility links right
- Nav links: 0.7rem, uppercase, 0.15em tracking, weight 600
- Active state: `border-bottom: 2px solid var(--text-primary)` or `1px solid currentColor`
- Diamond separator: `◇` at 0.6rem, color `var(--text-secondary)`

### Component Patterns

**Buttons:**
- Primary action: underline-style (`border-bottom: 1px solid`, uppercase, 0.15em tracking, 0.7rem)
- Filled primary: `background: var(--text-primary); color: white; padding: 0.6rem 1.2rem`
- Destructive: same as primary but `color: #C62828; border-bottom-color: #C62828`
- Secondary: `border: 1px solid var(--text-primary); background: transparent`

**Form Inputs:**
- Underline-only style: `border: none; border-bottom: 1px solid var(--text-primary); background: transparent`
- Font: `var(--font-main)` at 1rem
- No border-radius on inputs

**Status Indicators:**
- Dot: `8px × 8px` circle — green (#2E7D32), yellow (#F9A825), gray (#D8D6D4)
- Badge: `0.65rem`, uppercase, `border: 1px solid var(--border-color)`, `border-radius: 2px`, `padding: 2px 6px`
- Log levels: colored border + text (INFO=green, WARN=yellow, ERROR=red, DEBUG=gray)

**Progress Bars:**
- Track: `height: 4px; background: #E5E3E0`
- Fill: `background: var(--text-primary)`

**Tables:**
- Header: label-text style, `border-bottom: 1px solid var(--text-primary)`
- Row dividers: `1px solid #EEEEEC`
- Row hover: `background-color: #F9F8F6`

**Metric Blocks:**
- Label (icon + text) at top, large value below, mini bar chart or progress at bottom
- Bar charts: thin vertical bars, `opacity: 0.1` default, `opacity: 1` on `.active`, hover to 0.8

**Icons:**
- SVG, 14px × 14px (20px in brand guidelines)
- `stroke-width: 1.5`, no fill
- Used sparingly to denote data categories

### Scrollbar Styling
```css
::-webkit-scrollbar { width: 4px; }
::-webkit-scrollbar-track { background: transparent; }
::-webkit-scrollbar-thumb { background: var(--border-color); }
```

## Refinement Instructions

When I ask you to refine the UI, follow these rules:

### 1. Componentization
- Extract repeated patterns into reusable React components
- Shared components: `TopNav`, `DisplayHeader`, `GridRow`, `Cell`, `LabelText`, `StatusDot`, `Badge`, `ActionButton`, `ControlInput`, `MetricBlock`, `ProgressBar`
- Each page should import from a shared component library
- Use TypeScript interfaces for all component props

### 2. Interaction Polish
- Add `transition: all 0.2s ease` to interactive elements (buttons, links, rows)
- Hover states on table rows: subtle background shift to `#F9F8F6`
- Button hover: `opacity: 0.9` for filled, `opacity: 0.6` for underline-style
- Focus states: `outline: 2px solid var(--text-primary); outline-offset: 2px` for accessibility
- Loading states: skeleton screens using `var(--bg-accent)` with subtle pulse animation
- Smooth page transitions between routes

### 3. Micro-Animations
- Page load: stagger-reveal content sections with `animation-delay` (50ms increments)
- Metric values: count-up animation on dashboard load
- Status dots: subtle pulse animation for "Active" / "Running" states
- Log entries: slide-in from left when new entries appear
- Bar charts: grow-up animation on mount
- Keep it restrained — this is an infrastructure tool, not a marketing site

### 4. Responsive Behavior
- Below 1024px: collapse 4-column grid to 2-column
- Below 768px: single column, reduce display-text to 3rem
- Navigation: collapse to hamburger menu on mobile
- Tables: horizontal scroll wrapper on narrow screens
- Playground: stack sidebars below editor on mobile

### 5. Accessibility
- All interactive elements must be keyboard navigable
- ARIA labels on icon-only buttons
- Color is never the sole indicator (pair dots with text labels — already done)
- Minimum contrast ratios met (the existing palette handles this)
- Focus-visible styles on all interactive elements
- Screen reader support for live log updates (`aria-live="polite"`)

### 6. Code Quality
- Use CSS variables exclusively — no hardcoded colors or magic numbers
- Consistent naming: BEM-like or utility-first, pick one and commit
- No inline styles in production components (convert all inline styles from the HTML designs)
- TypeScript strict mode
- Extract common layout patterns (grid-row + cells) into composable components

### 7. What NOT to Change
- DO NOT change the color palette, fonts, or spacing scale
- DO NOT add rounded corners to buttons or inputs (the sharp/underline aesthetic is intentional)
- DO NOT add shadows or elevation (the flat, schematic look is the brand)
- DO NOT introduce new fonts or font sizes outside the hierarchy
- DO NOT add gradients, glows, or decorative backgrounds
- DO NOT deviate from the 4-column grid structure
- The warmth comes from the paper background (#FDFBF8) and DM Sans — that's enough

## Plugin Usage

When refining:
- Use **frontend-design** skill for any new components or layout decisions — it will push you toward distinctive choices while you ground it in the existing system
- Use **context7** to look up current React/Tailwind/library API docs before using any API
- Use **webapp-testing** to run Playwright visual regression tests after changes — compare against the original HTML designs
- Use **senior-frontend** for React/TypeScript patterns, bundle analysis, and accessibility audits
- Use Shift+Tab (plan mode) before starting work on any page to outline the component breakdown first

## Page-Specific Refinement Notes

### Dashboard
- The bar charts in metric blocks are CSS-only — consider adding real charting (Recharts) for actual data
- Quick Configuration table rows should be editable inline, not just "EDIT" links
- System logs section should auto-scroll and support real-time WebSocket updates

### Registry
- Search bar should filter in real-time as you type
- "Deploying..." state needs a progress indicator or spinner
- MANAGE action should open a slide-over panel, not navigate away

### Instances
- Progress bars in the table should animate on value change
- Thermal Throttling row should have a subtle warning background tint
- Scaling controls should validate input (min < max, trigger is 0-100)

### Playground
- Prompt textarea needs syntax highlighting for code blocks in responses
- Response area should support streaming text (character by character)
- Parameter sliders should show real-time value updates
- History items should be expandable to show full prompt/response

### API Keys
- Key creation flow: show the key ONCE in a modal with copy button, then mask it forever
- Add confirmation dialog before REVOKE
- Scope checkboxes should use custom styled checkboxes matching the brand

### System Logs
- Implement virtual scrolling for large log volumes
- Log level filters should be toggleable pills, not a dropdown
- Add auto-refresh toggle with configurable interval
- Search should highlight matching text in log messages

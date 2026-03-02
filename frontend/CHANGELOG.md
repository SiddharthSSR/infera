# Changelog

## [0.2.0] - 2026-03-02

### Fixed
- **Logs page**: Replaced all non-existent CSS classes (`bg-surface-*`, `text-surface-*`, `text-infera-*`, `bg-accent-green`, `bg-accent-red`, `bg-accent-yellow`) with the design system's CSS variable classes (`bg-card`, `text-foreground`, `text-primary`, `bg-success`, `text-destructive`, etc.)
- **Settings page**: Replaced all non-existent CSS classes with design system equivalents, ensuring both dark and light mode render correctly
- Typography inconsistencies across stat cards, worker mini cards, and cost values

### Added
- **URL Routing** (`react-router-dom`): Replaced `useState`-based page switching with proper `<Routes>`/`<Route>` components. Pages are now accessible via URL (`/`, `/playground`, `/instances`, `/logs`, `/settings`). Browser back/forward and direct URL navigation work correctly.
- **Toast notifications** (`sonner`): Added `<Toaster>` with themed styling. Actions now provide feedback: copy to clipboard, provision/terminate/start/stop instances, regenerate API key, export logs.
- **Dashboard charts** (`recharts`): Added "Performance" section with Request Throughput and Latency Distribution area charts using the `--chart-*` CSS variable color palette. Added inline sparklines in stat cards showing recent trends.
- **Markdown rendering** (`react-markdown`, `remark-gfm`, `rehype-highlight`): Assistant messages in Playground now render markdown with syntax-highlighted code blocks, tables, lists, blockquotes, and inline code.
- **Mobile-responsive sidebar**: Sidebar is hidden on screens < 768px. Hamburger menu button in TopBar opens sidebar as an overlay with backdrop. Click outside or nav click dismisses it.
- **Active nav indicator**: Active sidebar items show a left border accent (`border-l-2 border-primary`) with highlighted text/icon.
- **Textarea auto-resize**: Playground input textarea grows with content up to `max-h-32`, resets on submit.
- **Skeleton loaders**: Logs page shows skeleton rows during initial load. Playground model selector shows skeleton while models load.
- **Highlight.js theme**: Custom syntax highlighting colors using CSS variables for dark/light mode compatibility.
- **Markdown prose styles**: `.prose-chat` class with styled headings, lists, code, tables, blockquotes, links, and horizontal rules.

### Changed
- **Stat card typography**: Values use `text-3xl font-light tabular-nums font-mono`; labels use `text-xs uppercase tracking-wider`
- **Section headings**: Added `tracking-tight` for tighter heading typography
- **Worker mini card values**: Data values use `font-mono tabular-nums`
- **Cost values**: Use `font-mono tabular-nums` for consistent number rendering
- **Dashboard**: Removed `onNavigate` prop, uses `useNavigate()` from react-router-dom
- **Sidebar**: Uses `<NavLink>` for navigation instead of button click handlers
- **Settings theme toggle**: Removed emoji from theme button, uses text-only label

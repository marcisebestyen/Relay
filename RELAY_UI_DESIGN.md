# Relay — UI/UX Design Guidelines

**Document status:** Draft v0.1  
**Product:** Relay  
**Audience:** Frontend implementation and design reference  
**Language:** English

---

## 1. Design Intent

Relay is a developer-facing operational dashboard for a background job processing system.

The interface should communicate system state clearly, make failures diagnosable, and allow common operational actions without overwhelming the user.

### Keywords

- Developer-centric.
- Dark-first.
- Calm and precise.
- Information-rich.
- Functional.
- Consistent.
- Responsive.
- Accessible.

Avoid:
- Marketing-style dashboards.
- Excessive gradients or glow effects.
- Decorative animation.
- Oversized cards that waste space.
- Dense walls of unstructured text.
- Color as the only indicator of status.

## 2. Visual Direction

### 2.1 Theme

Dark mode is the primary design target. Light mode may be considered after the MVP.

Suggested palette (design tokens should be implemented centrally and adjusted during development):

| Token | Suggested value | Use |
|---|---|---|
| App background | `#0B0F14` | Main background |
| Sidebar background | `#10151C` | Navigation |
| Surface | `#151B23` | Panels and table surfaces |
| Raised surface | `#1B232D` | Popovers and raised elements |
| Border | `#27313D` | Subtle outlines/dividers |
| Primary text | `#E6EDF3` | Main text |
| Secondary text | `#9AA7B5` | Supporting text |
| Muted text | `#718096` | Low emphasis |
| Accent | `#2DD4BF` | Primary accent, selected states |
| Info | `#60A5FA` | Informational status |
| Success | `#34D399` | Completed |
| Warning | `#FBBF24` | Warnings |
| Error | `#F87171` | Failed / destructive |

These values are a starting point, not a fixed final palette. Contrast should be checked before implementation.

### 2.2 Status system

| Status | Color treatment | Label |
|---|---|---|
| Pending | Muted blue/neutral | Pending |
| Running | Teal | Running |
| Completed | Green | Completed |
| Failed | Red | Failed |
| Cancelled | Neutral gray | Cancelled |

Use compact pill badges. Pair each badge with text and optionally an icon. Keep badges small and consistent.

### 2.3 Typography

Suggested:
- Inter or a comparable sans-serif for UI.
- Monospace font for IDs, code, payloads, and logs (e.g. JetBrains Mono).

Hierarchy:
- Page heading: 22–26 px.
- Section heading: 15–18 px.
- Body: 13–14 px.
- Supporting labels: 11–12 px.
- Table text: 12–14 px.

Typography scale should be consistent and not depend on arbitrary per-page sizes.

### 2.4 Spacing and shape

- Base spacing unit: 4 px.
- Page padding: 24 px desktop, 16 px smaller screens.
- Panel padding: 16–20 px.
- Table row height: target 44–52 px.
- Corner radius: 8–12 px for panels; smaller for controls.
- Use subtle borders, not heavy shadows.
- Use dividers sparingly to separate content.

## 3. Application Shell

### 3.1 Sidebar navigation

Suggested items:
- Overview
- Jobs
- Queues
- Workers
- Observability (or Metrics)

Sidebar behavior:
- Fixed width on desktop (approximately 220–240 px).
- Collapsible where useful.
- Icon + label.
- Selected item is indicated with a restrained accent and background.
- Product logo/name at the top.
- Optional system health indicator near the bottom.

### 3.2 Top bar

Include:
- Breadcrumb or current section context when useful.
- System status (e.g. healthy/degraded) if grounded in actual data.
- Refresh control or refresh state.
- Settings may be deferred.

Do not invent system health statuses; derive them from real indicators.

### 3.3 Content area

- Align content to a consistent grid.
- Use a clear page title, optional subtitle, and page-level actions.
- Avoid nested panels within panels unless they represent meaningful grouping.
- Use responsive layout, with tables adapting or scrolling on narrow screens.

## 4. Page Designs

### 4.1 Overview

**Purpose:** Provide an at-a-glance operational picture.

Recommended structure:

1. Page title: `Overview`.
2. Summary metric row:
   - Pending jobs.
   - Running jobs.
   - Completed (within a defined time window).
   - Failed (within a defined time window).
3. Throughput chart.
4. Queue health / backlog panel.
5. Worker activity panel.
6. Recent failed jobs.
7. Recent activity (optional, MVP stretch).

Metric cards:
- Small label.
- Prominent number.
- Optional comparison or contextual note only when meaningful.
- Avoid arbitrary percentages or unsupported trend indicators.

Charts:
- Use readable axis labels and tooltips.
- Provide empty states if no data exists.
- Keep chart decoration restrained.

### 4.2 Jobs

**Purpose:** Find, inspect, and manage jobs.

Suggested table columns:
- Job ID (shortened display, copy full ID).
- Type.
- Queue.
- Status.
- Attempts (e.g. `2 / 5`).
- Created.
- Duration / age.

Filters:
- Status.
- Queue.
- Type.
- Time range.
- Search by job ID.

Interactions:
- Row click opens job detail.
- Sorting on relevant columns.
- Pagination.
- Clear filter/reset affordance.
- Table loading skeleton.
- Empty state with a useful message.
- Error state with retry.

Do not overload the table with every property. Details belong on the job detail page.

### 4.3 Job detail

**Purpose:** Understand what happened and take an appropriate action.

Recommended layout:

**Header**
- Job ID.
- Status.
- Job type.
- Queue.
- Primary actions (Retry, Cancel) only when valid.

**Summary**
- Created.
- Scheduled.
- Started.
- Completed.
- Duration.
- Attempt count.
- Worker.

**Execution timeline**
- Created.
- Claimed / started.
- Retry events.
- Completed or failed.
- Recovery events if applicable.

**Payload**
- Readable formatted JSON.
- Copy action.
- Sensitive-data caution; payloads should not be casually exposed or logged.

**Attempts**
- Attempt number.
- Start/end.
- Duration.
- Result.
- Error details.

**Logs**
- Correlated logs.
- Timestamp and level.
- Copyable text.
- Search/filter may be deferred.

Layout can use a main content column plus a narrower summary column on large screens, collapsing to one column on smaller screens.

### 4.4 Queues

**Purpose:** Understand the backlog and workload distribution.

Suggested table or list:
- Queue name.
- Pending count.
- Running count.
- Failed count (time-bounded or clearly defined).
- Queue latency.
- Workers consuming the queue.

Queue detail (optional for MVP):
- Queue-specific jobs view.
- Queue activity.
- Worker subscriptions.

### 4.5 Workers

**Purpose:** Understand worker capacity and health.

Suggested table columns:
- Worker ID.
- Status / last heartbeat.
- Queues.
- Concurrency.
- Active jobs.
- Last seen.

Worker detail (optional):
- Current jobs.
- Heartbeat history.
- Configuration summary.
- Recent activity.

A stale worker should be visually distinct, but the UI must use a defined freshness threshold from the product/implementation.

### 4.6 Metrics / Observability

The MVP may embed or link to Grafana rather than duplicating advanced charts.

Relay's own metrics page should prioritize:
- Throughput.
- Queue wait time.
- Job execution duration.
- Failure/retry rate.
- Queue depth.
- Worker utilization.

## 5. Interaction Patterns

- Use confirmation dialogs for destructive actions when appropriate.
- Show action feedback and avoid silent failure.
- Disable actions when they are not valid for the current state.
- Use consistent pagination and filtering.
- Provide copy-to-clipboard for job IDs, payloads, and error details.
- Use tooltips only for genuinely helpful supplementary information.
- Keep navigation and filter state predictable.
- Do not rely on hover alone for critical actions.

## 6. Component Inventory

Initial frontend component concepts:
- `AppShell`
- `Sidebar`
- `TopBar`
- `PageHeader`
- `MetricCard`
- `StatusBadge`
- `DataTable`
- `FilterBar`
- `Pagination`
- `EmptyState`
- `ErrorState`
- `LoadingSkeleton`
- `JobTimeline`
- `JsonViewer`
- `LogViewer`
- `ConfirmDialog`
- `Toast` / notification
- `ChartPanel`

This is a conceptual inventory; the implementation should avoid premature abstraction.

## 7. Responsive Behavior

Desktop:
- Full sidebar.
- Multi-column overview.
- Wide job table.

Tablet:
- Sidebar may collapse.
- Metric cards may wrap.
- Table remains scrollable or selectively adapts.

Mobile:
- Navigation becomes compact/drawer-based.
- Metrics stack.
- Tables may use a reduced-column layout or horizontally scroll.
- Job detail sections stack.
- Critical actions remain accessible.

The MVP primarily targets desktop developer workflows, but should not become unusable on mobile.

## 8. Accessibility

- Keyboard navigation for all core interactions.
- Visible focus rings.
- Appropriate semantic HTML.
- Sufficient contrast.
- Accessible labels for controls.
- Status labels not conveyed by color alone.
- Reduced-motion support.
- Charts should provide accessible summaries or equivalent data where practical.

## 9. Data Presentation

- Use consistent time formats and timezone handling.
- Use relative time as a supplementary value, not the only timestamp.
- Align numeric values consistently.
- Use monospace for IDs and technical data.
- Truncate long IDs with copy/full-view affordances.
- Payloads and logs should be formatted and readable.
- Avoid displaying sensitive payload content unnecessarily.

## 10. Design References and Inspiration

The visual direction should draw inspiration from modern developer tools and observability products such as:
- Grafana (information hierarchy and metrics).
- Linear (density, restraint, interaction polish).
- GitHub (developer familiarity and tables).
- Railway / Render-style infrastructure dashboards (system-oriented navigation and clarity).

These are references for visual qualities, not for copying product layouts or branding.

## 11. MVP UI Priorities

Implement in this order:
1. App shell and navigation.
2. Jobs list and filters.
3. Job detail and lifecycle/attempt view.
4. Overview metrics.
5. Queue view.
6. Worker view.
7. Metrics integration.
8. Responsive polish and accessibility pass.

## 12. UI Acceptance Checklist

- [ ] Consistent dark-first theme and tokens.
- [ ] Navigation structure is clear.
- [ ] Overview metrics are based on real data.
- [ ] Jobs can be searched and filtered.
- [ ] Job detail shows lifecycle, payload, and attempts.
- [ ] Valid retry/cancel actions are available.
- [ ] Queues can be inspected.
- [ ] Workers can be inspected.
- [ ] Loading, empty, and error states exist.
- [ ] Statuses have accessible labels.
- [ ] Core workflows are keyboard accessible.
- [ ] Responsive behavior is usable.
- [ ] No unnecessary decorative complexity.

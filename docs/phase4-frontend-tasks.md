# Phase 4.2: Frontend Enhancements - Task Breakdown

> **Historical record.** Superseded by [`roadmap-2.x.md`](roadmap-2.x.md).
> The console was rebuilt in `v2.1.0` as the fleet service decal sheet
> (`PRODUCT.md`, `DESIGN.md`, `.impeccable/review/verdict.md`): page layout,
> theme, and component names below describe the pre-2.1 surface. Dark mode,
> responsive layout, keyboard shortcuts, i18n, virtual scrolling and optimistic
> updates all survive; the file list is a record of the 2.0 change, not a map of
> `web/src/` today.

**Status:** Complete
**Started:** 2026-09-25
**Completed:** 2026-09-25

## Task List

### Dark Mode
- [x] Add `darkMode: 'class'` to tailwind.config.ts
- [x] Define light theme CSS variables in index.css
- [x] Create theme store (Zustand) for persisting user preference
- [x] Wire up Settings page theme selector to toggle dark class
- [x] Add dark: variants to key components (Layout, Sidebar, cards, buttons)
- [x] Implement system preference detection (prefers-color-scheme)

### Responsive Layout
- [x] Add mobile sidebar toggle (hamburger menu) in Layout
- [x] Make sidebar collapsible/sliding on small screens
- [x] Add responsive breakpoints to Devices table
- [x] Add responsive breakpoints to Alerts table
- [x] Add responsive breakpoints to Users table
- [x] Add responsive breakpoints to remaining pages as needed

### Keyboard Shortcuts
- [x] Create useKeyboardShortcuts hook
- [x] Add global shortcuts: `/` for search/focus, `g d` for devices, `g a` for alerts
- [x] Add shortcut hints/indicators in UI (tooltips or badges)
- [x] Add shortcut documentation in Settings

### Accessibility (WCAG 2.1 AA)
- [x] Add skip-to-content link
- [x] Add ARIA labels to interactive elements
- [x] Add ARIA live regions for notifications/status updates
- [x] Ensure proper heading hierarchy
- [x] Add focus indicators and focus management
- [x] Ensure color contrast meets AA standards

### i18n Framework
- [x] Install i18next + react-i18next
- [x] Create translation files (en as default)
- [x] Extract hardcoded strings to t() calls
- [x] Add language selector in Settings
- [x] Persist language preference

### Virtual Scrolling
- [x] Add virtual scrolling to large device lists (react-window or similar)

### Optimistic UI Updates
- [x] Add optimistic updates for alert acknowledgment
- [x] Add optimistic updates for device actions

## Implementation Notes

- Tailwind 3.4 with custom theme colors defined as CSS variables
- Zustand for state management (theme store)
- Lucide React for icons
- i18next for internationalization
- react-window for virtual scrolling

## Files Modified (Expected)

- `tailwind.config.ts` - darkMode config
- `index.css` - light/dark theme variables
- `web/src/stores/themeStore.ts` - new theme state
- `web/src/components/Layout.tsx` - responsive layout, skip link
- `web/src/components/Sidebar.tsx` - mobile toggle
- `web/src/pages/Settings.tsx` - theme/language selectors
- `web/src/i18n/` - new directory with translation files
- All pages with tables for responsive support
- `package.json` - new dependencies

# Octopus Web Console

This directory contains the Octopus management console. It is a Next.js 16 and React 19 application that is exported as static files and embedded into the Go service.

## Architecture

- Next.js App Router provides the static application shell.
- Navigation between Home, Site, Channel, Group, Model, Log, and Setting modules is handled by the in-app route store rather than separate Next.js pages.
- React Query manages server state and API polling.
- Zustand stores authentication, navigation, appearance, and local UI preferences.
- `next-intl` provides Simplified Chinese, Traditional Chinese, and English messages from `public/locale`.
- Tailwind CSS and reusable components under `src/components/ui` provide styling.

The frontend calls the Go management API under `/api/v1`. `NEXT_PUBLIC_API_BASE_URL` can override the API origin during development; the production default is the current origin.

## Development

Requirements: Node.js 22+ and pnpm.

```bash
pnpm install --frozen-lockfile
pnpm dev
```

Open <http://localhost:3000>. Run the Go backend separately on the API origin configured by `NEXT_PUBLIC_API_BASE_URL`.

Example:

```bash
NEXT_PUBLIC_API_BASE_URL=http://localhost:8080 pnpm dev
```

## Verification

```bash
pnpm exec tsc --noEmit
pnpm lint
pnpm build
```

`pnpm build` writes the static export to `web/out`. Release builds replace `static/out` with this directory before compiling the Go binary.

## Important directories

- `src/api/endpoints`: typed API and React Query hooks
- `src/components/modules`: feature modules and screens
- `src/components/ui`: reusable UI primitives
- `src/route`: in-app navigation configuration and lazy loading
- `src/provider`: authentication, locale, theme, and query providers
- `src/stores`: shared persisted state
- `public/locale`: translation resources

See the repository-level [README](../README.md) for backend architecture, supported model APIs, deployment, and operational guidance.

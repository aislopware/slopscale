# Admin console

The headscale admin console: a client-rendered React application served by the
server at `/admin/` (see `embed.go`, which embeds `dist/` into the binary).

## Stack

- React 19 with the React Compiler (`oxc-transform-react`, through
  `@vitejs/plugin-react`), built by Vite 8.
- TanStack Router (file routes under `src/routes`, generated tree in
  `src/routeTree.gen.ts`), TanStack Query, TanStack Table v9 (`useTable` and
  `tableFeatures`; v8 examples do not apply), TanStack Form where a form has
  more than a couple of fields.
- [Cloudflare Kumo](https://kumo-ui.com) (`@cloudflare/kumo`, Base UI +
  Tailwind CSS 4) for every component, with `@phosphor-icons/react` for
  icons. `src/components/ui` holds only the console's own compositions of
  Kumo parts. `PageHeader` is the title row with a meta line and actions.
  `Section` puts a 14px title above one `LayerCard`, and cards never nest.
  `DefinitionList` is label/value rows with a copy button on hover.
  `Avatar` is the initials mark. The rest are dialog scaffolds, the toast
  manager and the code editor. `src/components/table` adds the toolbar that
  sits inside the table card (search, segmented `Tabs`, primary action),
  the "Showing N of M" footer and `DataTable` with Kumo's compact header.
  The shell in `src/components/layout` is Kumo's `Sidebar` with grouped
  navigation and live approval badges, a `CommandPalette` on Cmd-K over
  pages, machines and users, and `Breadcrumbs` in the top bar. A detail
  page calls `useBreadcrumb(name)` to put its name in the trail. Colours
  are Kumo's semantic tokens (`bg-kumo-base`, `text-kumo-subtle`, ...) and
  dark mode is `data-mode` on `<html>`. The vendored Kumo lint rules in
  `lint/` reject raw palette colours and `dark:` variants. The design rules
  come from the Kumo design skill at
  `../.claude/skills/kumo-design/SKILL.md`. The ones that matter most here
  are 14px content text, sentence case, `font-semibold` at most,
  `ring ring-kumo-line` instead of border plus shadow, `Empty size="sm"`
  inside cards and `DeleteResource` for destructive confirmations.
- The API client is `openapi-fetch` + `openapi-react-query` over types
  generated from the server's OpenAPI document (`src/api/schema.gen.ts`).
  Regenerate with `make web-generate` from the repository root after changing
  the v1 API; CI checks the committed output.
- TypeScript 7 (the Go compiler), oxlint with every category and the
  type-aware rules on, oxfmt, Vitest in Chromium through Playwright. There is
  no ESLint or Prettier here; the repository's Prettier hooks skip `web/`.

## Working on it

```console
$ bun install            # once; the lockfile pins everything
$ bun run dev            # http://localhost:5173/admin/, proxies /api and /oidc to $HEADSCALE_URL or 127.0.0.1:8080
$ bun run check          # typecheck + lint + format check + tests
$ bun run build          # writes dist/, which the Go build embeds
$ bun run e2e            # builds, starts a real server (cmd/dev) and signs in through a browser
```

`bunx playwright install chromium` once before `bun run test` or `bun run e2e`.

The console signs in only through an identity provider. For local work,
`go run ./cmd/dev -server-url http://localhost:5173` (from the repository
root) starts a headscale with a mock provider whose only user is an admin;
`-server-url` makes the provider send the browser back to Vite. The e2e
suite in `e2e/` uses the same `cmd/dev` on its own port, against the built
console embedded in the binary, so it needs no running server.

Conventions the linter enforces: explicit return types, named constants
instead of magic numbers, no nested ternaries, functions under 120 lines,
kebab-case file names, top-level `import type`. Suppressing a rule is not
allowed; change the code. Route files export `Route`; everything else exports
named components. Permission checks read `me.permissions` through
`can(me, scope)` from `src/auth/me.ts`: hide pages the caller cannot read,
disable actions it cannot take.

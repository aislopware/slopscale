# Admin console

The slopscale admin console: a client-rendered React application served by the
server at `/console/` (see `embed.go`, which embeds `dist/` into the binary).

## Stack

- React 19 through `@vitejs/plugin-react`, built by Vite 8, with the React
  Compiler (`babel-plugin-react-compiler`, pinned exactly, through
  `@rolldown/plugin-babel`). Keep `@babel/core` on 7: under Babel 8 the
  compiler 1.0 silently skips every component that destructures a prop with
  a default. Oxc's native compiler (`react({ compiler: true })`) is still
  experimental. `DataTable` takes the table as a prop, never through
  context: the compiler memoizes on identity, and a context provider created
  once would keep the rows from ever seeing a sort or a new page.
- TanStack Router (file routes under `src/routes`, generated tree in
  `src/routeTree.gen.ts`), TanStack Query, TanStack Table v9 (`useTable` and
  `tableFeatures`; v8 examples do not apply), TanStack Form where a form has
  more than a couple of fields.
- [Cloudflare Kumo](https://kumo-ui.com) (`@cloudflare/kumo`, Base UI +
  Tailwind CSS 4) for every component, with `@phosphor-icons/react` for
  icons. The look follows the Cloudflare dashboard: Inter Variable (with
  `cv02 cv03 cv04` and -0.16px tracking) and Paper Mono (vendored in
  `src/fonts`, OFL), Kumo's own blue accent, white tooltips, pill badges.
  `src/components/ui` holds only the console's own compositions of Kumo
  parts. The shell lays every page on a 1200px grid (`ui/page.tsx`), so a
  route returns its blocks as a fragment; `FullBleed` spans the gutters and
  `PageWithRail` adds the 400px right rail (`RailSection`, `RailList`,
  `RailRow`). `PageHeader` is the 20px title with a "View docs" pill
  (`docs="ref/dns"`), a description, a meta line, actions and the rule
  under it. Surfaces: `Card` (white, ringed), `Section` (a titled band over
  one white panel), `SettingsCard`/`SettingsRow` (title, description,
  control), `StatTile`/`StatGrid`, `DefinitionList` and `KeyValueList`;
  cards never nest. `Status` is an icon and a word, `Badge` Kumo's pill,
  `Tag` a blue pill, `Domain` plain text with the site stepped back.
  `ConfirmDialog` is Kumo's `LayerDialog` alert (Cancel left, action
  right, no X) and `DeleteDialog` adds the typed-name check for deleting
  a named resource; form dialogs (`ui/dialog.tsx`) put the primary action
  first, on the left. `src/components/table` has the sticky
  `TableToolbar` (it gains an elevated frame once stuck under the top
  bar; `SearchInput joined` fits Kumo's `Toolbar.InputGroup`), `DataTable`
  (44px rows on a white card, a canvas header with half-height dividers,
  the actions column pinned), the shared classes in `table/styles.ts` for
  hand-built tables, and the "Showing N of M" footer below the card. The
  shell in `src/components/layout` is Kumo's `Sidebar` on the page's canvas
  with grouped navigation and live approval badges, a `CommandPalette` on
  Cmd-K over pages, machines and users, a 58px top bar with `Breadcrumbs`
  (from two levels down), a Docs link and the user menu (appearance
  submenu, sign out). A detail page calls `useBreadcrumb(name)` to put its
  name in the trail. Colours are Kumo's semantic tokens (`bg-kumo-base`,
  `text-kumo-subtle`, ...) and dark mode is `data-mode` on `<html>`. The
  vendored Kumo lint rules in `lint/` reject raw palette colours and
  `dark:` variants. The design rules come from the Kumo design skill at
  `../.claude/skills/kumo-design/SKILL.md`. The ones that matter most here
  are 14px content text, sentence case, `font-semibold` at most and
  `ring ring-kumo-line` instead of border plus shadow.
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
$ bun run dev            # http://localhost:5173/console/, proxies /api and /oidc to $SLOPSCALE_URL or 127.0.0.1:8080
$ bun run check          # typecheck + lint + format check + tests
$ bun run build          # writes dist/, which the Go build embeds
$ bun run e2e            # builds, starts a real server (cmd/dev) and signs in through a browser
```

`bunx playwright install chromium` once before `bun run test` or `bun run e2e`.

The console signs in only through an identity provider. For local work,
`go run ./cmd/dev -server-url http://localhost:5173` (from the repository
root) starts a slopscale with a mock provider whose only user is an admin;
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

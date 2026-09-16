# Social Notes

The existing SvelteKit UI is paired with a local-only Go API. Phase 0 provides the API skeleton, SQLite migrations, strict configuration validation, and health checking. Instagram integration is disabled until the real Meta feasibility spike establishes its contract.

## Backend

Create the local runtime configuration and start the API from the project root:

```sh
cp .env.example .env
go run ./backend/cmd/server
```

The SQLite database is created as `sqlite.db` in the project root. Check the API at `GET http://127.0.0.1:8080/api/health`.

Run backend formatting and tests with:

```sh
gofmt -w backend
go test ./backend/...
```

## Frontend

Everything you need to build a Svelte project, powered by [`sv`](https://github.com/sveltejs/cli).

## Creating a project

If you're seeing this, you've probably already done this step. Congrats!

```sh
# create a new project
npx sv create my-app
```

To recreate this project with the same configuration:

```sh
# recreate this project
npx sv@0.17.0 create --template minimal --types ts --add tailwindcss="plugins:none" --no-download-check --install npm .
```

## Developing

Once you've created a project and installed dependencies with `npm install` (or `pnpm install` or `yarn`), start a development server:

```sh
npm run dev

# or start the server and open the app in a new browser tab
npm run dev -- --open
```

## Building

To create a production version of your app:

```sh
npm run build
```

You can preview the production build with `npm run preview`.

> To deploy your app, you may need to install an [adapter](https://svelte.dev/docs/kit/adapters) for your target environment.

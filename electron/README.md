# New API Electron Desktop App

This directory contains the Electron wrapper for New API, with native windows and system-tray support on Windows, macOS, and Linux.

## Prerequisites

- Go 1.25.1+
- Bun
- Node.js and npm
- An externally reachable MySQL 8.x database
- A valid MySQL DSN in the `SQL_DSN` environment variable

The desktop app does not embed a local database. The packaged backend refuses to start without `SQL_DSN`.

## Development

Development mode does not start the Go backend. Run these commands in separate terminals from the repository root:

```bash
# Backend; SQL_DSN must already be set in this shell
go run .

# Frontend development server
cd web
bun run dev -- --port 5173

# Electron wrapper
cd electron
npm install
npm run dev-app
```

Electron loads the frontend development server on port 5173, while frontend API requests target the backend on port 3000.

## Build

The bundled `build.sh` builds `web/dist`, compiles the Go backend, installs Electron dependencies, and invokes the platform-specific electron-builder target:

```bash
cd electron
./build.sh
```

For an existing frontend and Go binary, the Electron package can also be built directly:

```bash
npm install
npm run build:mac
npm run build:win
npm run build:linux
```

Artifacts are written to `electron/dist/`. The packaged application still requires `SQL_DSN` at runtime; back up the external MySQL database using normal MySQL backup tooling.

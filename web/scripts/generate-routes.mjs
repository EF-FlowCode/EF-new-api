// eflow: regenerate src/routeTree.gen.ts with the same generator the rsbuild
// plugin uses, without building the site. Run through scripts/docker-check.sh routes.
import { Generator, getConfig } from '@tanstack/router-generator'

const root = process.cwd()
const config = getConfig({ target: 'react', autoCodeSplitting: true }, root)
await new Generator({ config, root }).run()

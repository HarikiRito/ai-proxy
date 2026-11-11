# Package Manager

pnpm only - no npm, no yarn.

- Enforced by `"preinstall": "npx only-allow pnpm"` in `package.json` - `npm install` / `yarn install` fail immediately
- Pinned via `"packageManager": "pnpm@12.4.1"`
- `"engines": { "pnpm": ">=12", "node": ">=24" }`, backed by `.npmrc`'s `engine-strict=true`
- Node `>= 24` required

## Activating pnpm via corepack

```bash
corepack enable
corepack prepare pnpm@12.4.1 --activate
```

Then install as usual:

```bash
pnpm install
```

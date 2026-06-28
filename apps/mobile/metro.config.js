// Metro config for the Coding Anywhere mobile app (Expo SDK 56) inside the pnpm
// monorepo. Beyond the Expo defaults we need three things:
//
//  1. Monorepo resolution — watch the repo root and resolve modules from both the
//     app's and the workspace-root node_modules (pnpm symlinks the shared
//     @coding-anywhere/* packages into apps/mobile/node_modules).
//  2. TS-source workspace packages — the shared packages ship TypeScript *source*
//     (`main: ./src/index.ts`) and import siblings with NodeNext-style `.js`
//     specifiers. Metro transpiles the source (the packages live under the watched
//     repo root, not under node_modules), but we must map `./foo.js` -> `./foo.ts`
//     so the imports resolve.
//  3. NativeWind (Tailwind) via withNativeWind.

const { getDefaultConfig } = require('expo/metro-config');
const { withNativeWind } = require('nativewind/metro');
const path = require('path');

const projectRoot = __dirname;
const workspaceRoot = path.resolve(projectRoot, '../..');

const config = getDefaultConfig(projectRoot);

// 1. Monorepo: watch the workspace root and resolve from both node_modules trees.
config.watchFolders = [workspaceRoot];
config.resolver.nodeModulesPaths = [
  path.resolve(projectRoot, 'node_modules'),
  path.resolve(workspaceRoot, 'node_modules'),
];

// The shared packages declare an `exports` map and pure-ESM deps (micromark,
// mdast-*). Package-exports resolution is required for both.
config.resolver.unstable_enablePackageExports = true;

// 2. Map `.js` import specifiers to their `.ts` source for the TS-source packages.
//    Try the normal resolution first; only fall back to `.ts` when a `.js`
//    specifier fails to resolve (i.e. it is actually a TypeScript file).
const baseResolveRequest = config.resolver.resolveRequest;
config.resolver.resolveRequest = (context, moduleName, platform) => {
  const resolve = baseResolveRequest ?? context.resolveRequest;
  try {
    return resolve(context, moduleName, platform);
  } catch (err) {
    if (moduleName.endsWith('.js')) {
      return resolve(context, moduleName.replace(/\.js$/, '.ts'), platform);
    }
    throw err;
  }
};

module.exports = withNativeWind(config, { input: './src/global.css' });

#!/usr/bin/env tsx
/**
 * Codegen entry point: reads JSON Schemas under proto/ and emits typed
 * artefacts on both sides of the wire.
 *
 *   - TypeScript types  → packages/protocol/src/generated/
 *   - Go structs        → internal/wire/generated/
 *
 * P0 ships a placeholder that scans proto/ and prints what it would emit so
 * we can wire the rest of the toolchain (turbo task, CI) end-to-end without
 * blocking on full codegen polish. Real generation lands in P1 when we
 * actually need the types.
 */
import { readdir, stat } from 'node:fs/promises';
import { join, resolve } from 'node:path';

const REPO_ROOT = resolve(import.meta.dirname, '..', '..', '..');
const PROTO_DIR = join(REPO_ROOT, 'proto');

async function listSchemas(dir: string): Promise<string[]> {
  const entries = await readdir(dir);
  const files: string[] = [];
  for (const name of entries) {
    if (!name.endsWith('.json')) continue;
    const p = join(dir, name);
    const s = await stat(p);
    if (s.isFile()) files.push(p);
  }
  return files.sort();
}

async function main(): Promise<void> {
  const schemas = await listSchemas(PROTO_DIR);
  if (schemas.length === 0) {
    console.error(`No schemas under ${PROTO_DIR}`);
    process.exit(0);
  }
  console.log(`Found ${schemas.length} schema(s) under proto/:`);
  for (const s of schemas) {
    console.log(`  - ${s.replace(REPO_ROOT + '/', '')}`);
  }
  console.log(
    '\nP0 stub: real TypeScript + Go emission lands in P1 when channel payloads stabilize.',
  );
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});

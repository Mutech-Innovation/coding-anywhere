// Wire types for the `git` channel (used today for diff metadata + file
// versions; pure git operations like commit/push will move here later).

import { z } from 'zod';

export const DiffMethod = {
  ListChanges: 'list_changes',
  Versions: 'versions',
} as const;
export type DiffMethod = (typeof DiffMethod)[keyof typeof DiffMethod];

export const DiffChangeKind = z.enum([
  'added',
  'modified',
  'deleted',
  'renamed',
  'copied',
  'untracked',
]);
export type DiffChangeKind = z.infer<typeof DiffChangeKind>;

export const DiffChange = z.object({
  path: z.string(),
  old_path: z.string().optional(),
  kind: DiffChangeKind,
  staged: z.boolean().optional(),
});
export type DiffChange = z.infer<typeof DiffChange>;

export const DiffListChangesResponse = z.object({
  changes: z.array(DiffChange),
  is_repo: z.boolean(),
});
export type DiffListChangesResponse = z.infer<typeof DiffListChangesResponse>;

export interface DiffVersionsRequest {
  path: string;
}

export const DiffVersionsResponse = z.object({
  path: z.string(),
  original: z.string(),
  current: z.string(),
});
export type DiffVersionsResponse = z.infer<typeof DiffVersionsResponse>;

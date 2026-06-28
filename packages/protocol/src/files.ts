// Wire types for the `files` channel. Mirrors internal/wire/files.go.

import { z } from 'zod';

export const FilesMethod = {
  ListWorkspace: 'list_workspace',
  List: 'list',
  Read: 'read',
  Stat: 'stat',
  Write: 'write',
  Delete: 'delete',
  Move: 'move',
} as const;
export type FilesMethod = (typeof FilesMethod)[keyof typeof FilesMethod];

export const FilesEntryKind = z.enum(['file', 'directory', 'symlink', 'other']);
export type FilesEntryKind = z.infer<typeof FilesEntryKind>;

export const FilesEntry = z.object({
  name: z.string(),
  path: z.string(),
  kind: FilesEntryKind,
  size: z.number(),
  mod_time: z.number(),
  target: z.string().optional(),
});
export type FilesEntry = z.infer<typeof FilesEntry>;

export const FilesWorkspaceInfo = z.object({
  id: z.string(),
  name: z.string(),
  root: z.string(),
});
export type FilesWorkspaceInfo = z.infer<typeof FilesWorkspaceInfo>;

export const FilesListWorkspaceResponse = z.object({
  workspaces: z.array(FilesWorkspaceInfo),
});
export type FilesListWorkspaceResponse = z.infer<typeof FilesListWorkspaceResponse>;

export interface FilesListRequest {
  workspace_id?: string;
  path: string;
  include_hidden?: boolean;
  max_entries?: number;
}

export const FilesListResponse = z.object({
  workspace_id: z.string(),
  path: z.string(),
  entries: z.array(FilesEntry),
});
export type FilesListResponse = z.infer<typeof FilesListResponse>;

export interface FilesReadRequest {
  workspace_id?: string;
  path: string;
  offset?: number;
  length?: number;
  max_bytes?: number;
  allow_binary?: boolean;
}

export const FilesReadResponse = z.object({
  workspace_id: z.string(),
  path: z.string(),
  size: z.number(),
  truncated: z.boolean().optional(),
  is_binary: z.boolean().optional(),
  content_type: z.string().optional(),
  data: z.string(),
});
export type FilesReadResponse = z.infer<typeof FilesReadResponse>;

export interface FilesStatRequest {
  workspace_id?: string;
  path: string;
}

export const FilesStatResponse = z.object({
  workspace_id: z.string(),
  entry: FilesEntry,
});
export type FilesStatResponse = z.infer<typeof FilesStatResponse>;

// --- write ---
// Uploads stream as a sequence of write calls because the relay caps a single
// WS message at 1 MiB: the first chunk sets `truncate` (replacing any existing
// file), later chunks carry an increasing `offset`. `data` is base64.
export interface FilesWriteRequest {
  workspace_id?: string;
  path: string;
  offset?: number;
  data: string;
  truncate?: boolean;
}

export const FilesWriteResponse = z.object({
  workspace_id: z.string(),
  path: z.string(),
  size: z.number(),
});
export type FilesWriteResponse = z.infer<typeof FilesWriteResponse>;

// --- delete ---
export interface FilesDeleteRequest {
  workspace_id?: string;
  path: string;
}

export const FilesDeleteResponse = z.object({
  workspace_id: z.string(),
  path: z.string(),
});
export type FilesDeleteResponse = z.infer<typeof FilesDeleteResponse>;

// --- move (rename + relocate) ---
export interface FilesMoveRequest {
  workspace_id?: string;
  from: string;
  to: string;
}

export const FilesMoveResponse = z.object({
  workspace_id: z.string(),
  from: z.string(),
  to: z.string(),
});
export type FilesMoveResponse = z.infer<typeof FilesMoveResponse>;

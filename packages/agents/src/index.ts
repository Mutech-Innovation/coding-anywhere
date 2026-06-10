// Client-side metadata about the AI coding agents we support.
//
// This is presentation-layer metadata (display name, accent color, capability
// flags) used to render UI; the actual transport adapters live on the daemon
// in Go (internal/agent/...). Priority order matches the project's README:
// Devin for Terminal first, then Claude Code, Codex, cursor-agent.

export type AgentId = 'devin' | 'claude-code' | 'codex' | 'cursor-cli';

export interface AgentCapabilities {
  /** Streaming structured events via ACP. */
  acp: boolean;
  /** Headless single-shot via `-p`. */
  headlessPrint: boolean;
  /** Resume an existing session by id. */
  resumeBySessionId: boolean;
  /** Slash commands inside a session. */
  slashCommands: boolean;
  /** MCP server attachment. */
  mcp: boolean;
}

export interface AgentDescriptor {
  id: AgentId;
  /** Human-readable name shown in pickers. */
  name: string;
  /** One-line tagline. */
  tagline: string;
  /** Hex color used as accent in the UI. */
  accent: string;
  /** Order index — lower wins. */
  priority: number;
  capabilities: AgentCapabilities;
  /** Whether this agent is wired up in the current build. */
  available: boolean;
}

export const AGENTS: Record<AgentId, AgentDescriptor> = {
  devin: {
    id: 'devin',
    name: 'Devin for Terminal',
    tagline: 'Local coding agent with deep Devin Cloud integration',
    accent: '#306eff',
    priority: 0,
    capabilities: {
      acp: true,
      headlessPrint: true,
      resumeBySessionId: true,
      slashCommands: true,
      mcp: true,
    },
    available: true,
  },
  'claude-code': {
    id: 'claude-code',
    name: 'Claude Code',
    tagline: "Anthropic's terminal coding agent",
    accent: '#d97706',
    priority: 1,
    capabilities: {
      acp: true,
      headlessPrint: true,
      resumeBySessionId: true,
      slashCommands: true,
      mcp: true,
    },
    available: false,
  },
  codex: {
    id: 'codex',
    name: 'Codex',
    tagline: "OpenAI's coding agent CLI",
    accent: '#10b981',
    priority: 2,
    capabilities: {
      acp: false,
      headlessPrint: true,
      resumeBySessionId: true,
      slashCommands: true,
      mcp: true,
    },
    available: false,
  },
  'cursor-cli': {
    id: 'cursor-cli',
    name: 'cursor-agent',
    tagline: 'Cursor in your terminal',
    accent: '#a855f7',
    priority: 3,
    capabilities: {
      acp: true,
      headlessPrint: true,
      resumeBySessionId: true,
      slashCommands: true,
      mcp: true,
    },
    available: true,
  },
};

/** Return all agents sorted by `priority` ascending. */
export function listAgents(): AgentDescriptor[] {
  return Object.values(AGENTS).sort((a, b) => a.priority - b.priority);
}

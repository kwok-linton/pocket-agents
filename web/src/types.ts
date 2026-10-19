export interface Capabilities {
  inputMode?: "queue" | "inbox" | "bridge";
  discover: boolean;
  create: boolean;
  resume: boolean;
  attach: boolean;
  send: boolean;
  approve: boolean;
  interrupt: boolean;
  terminate?: boolean;
  terminal: boolean;
  persistent: boolean;
  reason?: string;
}
export interface Metadata {
  alias?: string;
  favorite: boolean;
  done: boolean;
  hidden: boolean;
  readAt: string;
}
export interface Approval {
  id: string;
  nativeId: string;
  executionId: string;
  kind: string;
  title: string;
  detail: string;
  options?: string[];
  data?: unknown;
}
export interface Session {
  id: string;
  nodeId: string;
  deviceName?: string;
  provider: string;
  profile: string;
  nativeId: string;
  cwd: string;
  title: string;
  updatedAt: string;
  executionId?: string;
  executionState: string;
  turnState: string;
  turnId?: string;
  capabilities: Capabilities;
  pending?: Approval[];
  error?: string;
  parentId?: string;
  metadata: Metadata;
}
export interface Diagnostic {
  createPermissionMode?: string;
  provider: string;
  profile: string;
  version: string;
  available: boolean;
  loggedIn: string;
  capabilities: Capabilities;
  details: string;
}
export interface Device {
  id: string;
  name: string;
  os: string;
  online: boolean;
  lastSeen: string;
  diagnostics: Diagnostic[];
  roots: string[];
  tailnetId?: string;
}
export interface Message {
  id: string;
  role: string;
  text: string;
  kind?: string;
  at: string;
}
export interface History {
  messages: Message[];
  cursor?: string;
  hasMore: boolean;
  boundary?: string;
}
export interface Action {
  model?: string;
  effort?: string;
  files?: string[];
  id: string;
  nodeId: string;
  provider: string;
  profile: string;
  nativeId?: string;
  executionId?: string;
  kind: string;
  cwd?: string;
  text?: string;
  terminalId?: string;
  requestId?: string;
  decision?: string;
  data?: unknown;
}
export interface Result {
  actionId: string;
  status: "submitted" | "accepted" | "unknown" | "rejected";
  nativeId?: string;
  executionId?: string;
  message?: string;
  data?: unknown;
}
export interface Event {
  id: number;
  kind: string;
  sessionId?: string;
  nodeId?: string;
  at: string;
  data?: unknown;
}
export interface Candidates {
  ready: boolean;
  reason?: string;
  peers: {
    id: string;
    name: string;
    os: string;
    online: boolean;
    registered: boolean;
  }[];
}
export interface DirectoryListing {
  path: string;
  roots: string[];
  entries: { name: string; path: string }[];
}
export interface TrackedAction {
  action: Pick<
    Action,
    "id" | "kind" | "nodeId" | "provider" | "nativeId" | "profile"
  >;
  result: Result;
  at: string;
}
export interface WorkspacePreferences {
  workspaceScope: "all" | "focused";
  workspaces: { nodeId: string; path: string }[];
}

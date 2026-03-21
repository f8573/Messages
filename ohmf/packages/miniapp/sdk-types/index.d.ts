export const BRIDGE_VERSION: "1.0";

export type KnownCapability =
  | "conversation.read_context"
  | "conversation.send_message"
  | "participants.read_basic"
  | "storage.session"
  | "storage.shared_conversation"
  | "realtime.session"
  | "media.pick_user"
  | "notifications.in_app";

export interface MiniAppParticipant {
  user_id: string;
  role: string;
  display_name?: string;
}

export interface MiniAppLaunchContext {
  bridge_version?: string;
  app_id: string;
  app_version?: string;
  app_session_id: string;
  conversation_id: string;
  viewer: MiniAppParticipant;
  participants: MiniAppParticipant[];
  capabilities_granted: KnownCapability[] | string[];
  host_capabilities?: KnownCapability[] | string[];
  state_snapshot: Record<string, unknown>;
  state_version: number;
  joinable: boolean;
}

export interface BridgeError extends Error {
  code?: string;
  details?: Record<string, unknown>;
}

export declare class OHMFMiniAppClient {
  constructor(options: { channel: string; targetOrigin: string; targetWindow?: Window });
  destroy(): void;
  on(eventName: string, handler: (payload: unknown) => void): () => void;
  off(eventName: string, handler: (payload: unknown) => void): void;
  call(method: string, params?: Record<string, unknown>): Promise<unknown>;
  getLaunchContext(): Promise<MiniAppLaunchContext>;
  readConversationContext(): Promise<unknown>;
  sendConversationMessage(params: Record<string, unknown>): Promise<unknown>;
  readParticipants(): Promise<{ participants: MiniAppParticipant[] }>;
  getSessionStorage(key: string): Promise<unknown>;
  setSessionStorage(key: string, value: unknown): Promise<unknown>;
  getSharedConversationStorage(key: string): Promise<unknown>;
  setSharedConversationStorage(key: string, value: unknown): Promise<unknown>;
  updateSessionState(snapshot: Record<string, unknown>): Promise<unknown>;
  pickUserMedia(options?: Record<string, unknown>): Promise<unknown>;
  showInAppNotification(notification: Record<string, unknown>): Promise<unknown>;
}

export declare function createMiniAppClientFromLocation(search?: string): OHMFMiniAppClient;

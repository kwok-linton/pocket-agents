export class PushSetupError extends Error {}
export type PushEnvironment = { secure: boolean; ios: boolean; standalone: boolean; supported: boolean; permission: NotificationPermission };
export function pushStage(env: PushEnvironment, loaded: boolean, subscribed: boolean, registered: boolean) {
  if (!env.secure) return "https";
  if (env.ios && !env.standalone) return "install";
  if (!env.supported) return "unsupported";
  if (env.permission === "denied") return "denied";
  if (env.permission !== "granted") return "permission";
  if (!loaded) return "loading";
  if (!subscribed || !registered) return "register";
  return "ready";
}
export function withTimeout<T>(operation: Promise<T>, ms: number, message: string): Promise<T> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new PushSetupError(message)), ms);
    operation.then(resolve, reject).finally(() => clearTimeout(timer));
  });
}

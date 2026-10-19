// Invoke permission synchronously from the tap, even while a background check
// is pending. Queue registration after that check instead of dropping the tap.
export async function allowPush(
  requestPermission: () => Promise<NotificationPermission>,
  pendingCheck: Promise<void> | null,
  register: () => Promise<void>,
  permissionChanged: () => void,
) {
  const request = requestPermission();
  const permission = await request;
  permissionChanged();
  if (permission !== "granted") return;
  await pendingCheck?.catch(() => {});
  await register();
}

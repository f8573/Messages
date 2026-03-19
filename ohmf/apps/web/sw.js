self.addEventListener("install", (event) => {
  event.waitUntil(self.skipWaiting());
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

self.addEventListener("push", (event) => {
  let payload = {};
  try {
    payload = event.data ? event.data.json() : {};
  } catch {
    payload = {};
  }
  const title = payload.title || "OHMF";
  const body = payload.body || "New message";
  const data = payload.data || {};
  if (payload.conversation_id && !data.conversation_id) data.conversation_id = payload.conversation_id;
  event.waitUntil(
    self.registration.showNotification(title, {
      body,
      data,
      tag: data.conversation_id || "ohmf-message",
    })
  );
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const conversationId = event.notification.data?.conversation_id || "";
  const target = new URL("./", self.location.origin);
  if (conversationId) target.searchParams.set("conversation_id", conversationId);
  event.waitUntil(
    self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((clients) => {
      for (const client of clients) {
        if ("focus" in client) {
          client.postMessage({ type: "notification.open", conversation_id: conversationId });
          return client.focus();
        }
      }
      return self.clients.openWindow(target.toString());
    })
  );
});

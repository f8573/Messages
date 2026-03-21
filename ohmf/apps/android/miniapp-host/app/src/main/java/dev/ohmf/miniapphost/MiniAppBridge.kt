package dev.ohmf.miniapphost

import android.webkit.JavascriptInterface
import org.json.JSONArray
import org.json.JSONObject
import java.util.UUID

class MiniAppBridge(
    private val appId: String,
    private val appVersion: String,
    private val grantedPermissions: List<String>
) {
    private val sessionStorage = linkedMapOf<String, Any?>()
    private val sharedStorage = linkedMapOf<String, Any?>()
    private val transcript = mutableListOf<Map<String, Any?>>()
    private val stateSnapshot = linkedMapOf<String, Any?>()
    private var stateVersion = 1

    @JavascriptInterface
    fun handleRequest(requestJson: String): String {
        val request = JSONObject(requestJson)
        val requestId = request.optString("request_id")
        return try {
            val result = when (request.optString("method")) {
                "host.getLaunchContext" -> buildLaunchContext()
                "conversation.readContext" -> {
                    requirePermission("conversation.read_context")
                    JSONObject()
                    .put("conversation_id", "conv_demo_android")
                    .put("title", "Android Mini-App Host")
                    .put("recent_messages", JSONArray(transcript.map { JSONObject(it) }))
                }
                "participants.readBasic" -> {
                    requirePermission("participants.read_basic")
                    JSONObject()
                    .put("participants", JSONArray().put(JSONObject().put("user_id", "usr_android").put("role", "PLAYER").put("display_name", "Android Tester")))
                }
                "storage.session.get" -> {
                    requirePermission("storage.session")
                    val key = request.optJSONObject("params")?.optString("key").orEmpty()
                    JSONObject().put("key", key).put("value", sessionStorage[key])
                }
                "storage.session.set" -> {
                    requirePermission("storage.session")
                    val params = request.optJSONObject("params") ?: JSONObject()
                    val key = params.optString("key")
                    sessionStorage[key] = params.opt("value")
                    stateVersion += 1
                    JSONObject().put("key", key).put("value", params.opt("value")).put("state_version", stateVersion)
                }
                "storage.sharedConversation.get" -> {
                    requirePermission("storage.shared_conversation")
                    val key = request.optJSONObject("params")?.optString("key").orEmpty()
                    JSONObject().put("key", key).put("value", sharedStorage[key])
                }
                "storage.sharedConversation.set" -> {
                    requirePermission("storage.shared_conversation")
                    val params = request.optJSONObject("params") ?: JSONObject()
                    val key = params.optString("key")
                    sharedStorage[key] = params.opt("value")
                    stateVersion += 1
                    JSONObject().put("key", key).put("value", params.opt("value")).put("state_version", stateVersion)
                }
                "session.updateState" -> {
                    requirePermission("realtime.session")
                    val params = request.optJSONObject("params") ?: JSONObject()
                    params.keys().forEach { key -> stateSnapshot[key] = params.opt(key) }
                    stateVersion += 1
                    JSONObject().put("state_version", stateVersion).put("state_snapshot", JSONObject(stateSnapshot as Map<*, *>))
                }
                "conversation.sendMessage" -> {
                    requirePermission("conversation.send_message")
                    val params = request.optJSONObject("params") ?: JSONObject()
                    transcript += mapOf(
                        "id" to "msg_${UUID.randomUUID()}",
                        "author" to "Mini-App",
                        "text" to (params.optString("text").ifBlank { "Mini-app update" })
                    )
                    stateVersion += 1
                    JSONObject().put("message_id", "msg_${UUID.randomUUID()}").put("state_version", stateVersion)
                }
                "notifications.inApp.show" -> {
                    requirePermission("notifications.in_app")
                    JSONObject().put("displayed", true)
                }
                else -> throw IllegalArgumentException("Unknown bridge method: ${request.optString("method")}")
            }
            JSONObject()
                .put("bridge_version", "1.0")
                .put("channel", request.optString("channel"))
                .put("request_id", requestId)
                .put("ok", true)
                .put("result", result)
                .toString()
        } catch (error: Throwable) {
            JSONObject()
                .put("bridge_version", "1.0")
                .put("channel", request.optString("channel"))
                .put("request_id", requestId)
                .put("ok", false)
                .put("error", JSONObject().put("code", "bridge_error").put("message", error.message))
                .toString()
        }
    }

    private fun requirePermission(permission: String) {
        if (!grantedPermissions.contains(permission)) {
            throw IllegalStateException("Permission denied: $permission")
        }
    }

    private fun buildLaunchContext(): JSONObject {
        return JSONObject()
            .put("app_id", appId)
            .put("app_version", appVersion)
            .put("app_session_id", "aps_${UUID.randomUUID()}")
            .put("conversation_id", "conv_demo_android")
            .put("viewer", JSONObject().put("user_id", "usr_android").put("role", "PLAYER").put("display_name", "Android Tester"))
            .put("participants", JSONArray().put(JSONObject().put("user_id", "usr_android").put("role", "PLAYER").put("display_name", "Android Tester")))
            .put("capabilities_granted", JSONArray(grantedPermissions))
            .put("state_snapshot", JSONObject(stateSnapshot as Map<*, *>))
            .put("state_version", stateVersion)
            .put("joinable", true)
    }
}

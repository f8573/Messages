package dev.ohmf.miniapphost

data class MiniAppCatalogEntry(
    val appId: String,
    val title: String,
    val version: String,
    val reviewStatus: String,
    val sourceType: String,
    val entrypointUrl: String,
    val installedVersion: String?,
    val updateAvailable: Boolean
)

data class MiniAppLaunchContext(
    val app_id: String,
    val app_version: String,
    val app_session_id: String,
    val conversation_id: String,
    val viewer: Map<String, Any?>,
    val participants: List<Map<String, Any?>>,
    val capabilities_granted: List<String>,
    val state_snapshot: Map<String, Any?>,
    val state_version: Int,
    val joinable: Boolean
)

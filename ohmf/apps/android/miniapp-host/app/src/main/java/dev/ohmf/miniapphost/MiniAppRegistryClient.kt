package dev.ohmf.miniapphost

import org.json.JSONArray
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

class MiniAppRegistryClient(
    private val apiBaseUrl: String,
    private val bearerToken: String
) {
    fun listApps(): List<MiniAppCatalogEntry> {
        val payload = request("/v1/apps", "GET")
        val items = payload.optJSONArray("items") ?: JSONArray()
        return buildList {
            for (index in 0 until items.length()) {
                val raw = items.optJSONObject(index) ?: continue
                val manifest = raw.optJSONObject("manifest") ?: JSONObject()
                val install = raw.optJSONObject("install") ?: JSONObject()
                add(
                    MiniAppCatalogEntry(
                        appId = raw.optString("app_id", manifest.optString("app_id")),
                        title = manifest.optString("name", raw.optString("app_id")),
                        version = raw.optString("version", manifest.optString("version")),
                        reviewStatus = raw.optString("review_status", "approved"),
                        sourceType = raw.optString("source_type", "external"),
                        entrypointUrl = manifest.optJSONObject("entrypoint")?.optString("url").orEmpty(),
                        installedVersion = install.optString("installed_version").ifBlank { null },
                        updateAvailable = raw.optBoolean("update_available", false)
                    )
                )
            }
        }
    }

    fun install(appId: String): JSONObject = request("/v1/apps/$appId/install", "POST")

    private fun request(route: String, method: String): JSONObject {
        val connection = (URL("${apiBaseUrl.trimEnd('/')}$route").openConnection() as HttpURLConnection).apply {
            requestMethod = method
            setRequestProperty("Authorization", "Bearer $bearerToken")
            setRequestProperty("Content-Type", "application/json")
            doInput = true
        }
        val stream = if (connection.responseCode in 200..299) connection.inputStream else connection.errorStream
        val body = stream?.bufferedReader()?.use { it.readText() }.orEmpty()
        if (connection.responseCode !in 200..299) {
            throw IllegalStateException(body.ifBlank { "$method $route failed with ${connection.responseCode}" })
        }
        return if (body.isBlank()) JSONObject() else JSONObject(body)
    }
}

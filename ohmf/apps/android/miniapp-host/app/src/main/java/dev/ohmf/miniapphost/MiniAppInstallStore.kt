package dev.ohmf.miniapphost

import android.content.Context

class MiniAppInstallStore(context: Context) {
    private val prefs = context.getSharedPreferences("miniapp.installs", Context.MODE_PRIVATE)

    fun installedVersion(appId: String): String? = prefs.getString(appId, null)

    fun markInstalled(appId: String, version: String) {
        prefs.edit().putString(appId, version).apply()
    }

    fun uninstall(appId: String) {
        prefs.edit().remove(appId).apply()
    }
}

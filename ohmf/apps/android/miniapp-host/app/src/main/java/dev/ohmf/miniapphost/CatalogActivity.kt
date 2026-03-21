package dev.ohmf.miniapphost

import android.content.Intent
import android.os.Bundle
import android.widget.ArrayAdapter
import android.widget.Button
import android.widget.EditText
import android.widget.ListView
import android.widget.Switch
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity
import kotlin.concurrent.thread

class CatalogActivity : AppCompatActivity() {
    private lateinit var apiBaseInput: EditText
    private lateinit var bearerTokenInput: EditText
    private lateinit var devModeSwitch: Switch
    private lateinit var refreshButton: Button
    private lateinit var statusText: TextView
    private lateinit var listView: ListView
    private lateinit var installStore: MiniAppInstallStore

    private var catalog: List<MiniAppCatalogEntry> = emptyList()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_catalog)

        installStore = MiniAppInstallStore(this)
        apiBaseInput = findViewById(R.id.api_base_input)
        bearerTokenInput = findViewById(R.id.bearer_token_input)
        devModeSwitch = findViewById(R.id.dev_mode_switch)
        refreshButton = findViewById(R.id.refresh_button)
        statusText = findViewById(R.id.status_text)
        listView = findViewById(R.id.catalog_list)

        apiBaseInput.setText("http://10.0.2.2:18080")
        refreshButton.setOnClickListener { refreshCatalog() }
        listView.setOnItemClickListener { _, _, position, _ -> openSelected(catalog[position]) }
    }

    private fun refreshCatalog() {
        val apiBase = apiBaseInput.text.toString().trim()
        val token = bearerTokenInput.text.toString().trim()
        if (apiBase.isBlank() || token.isBlank()) {
            Toast.makeText(this, "API base URL and bearer token are required.", Toast.LENGTH_SHORT).show()
            return
        }
        statusText.text = "Loading catalog…"
        thread {
            try {
                val items = MiniAppRegistryClient(apiBase, token).listApps()
                    .filter { devModeSwitch.isChecked || it.reviewStatus == "approved" || it.sourceType == "dev" }
                catalog = items
                val labels = items.map {
                    buildString {
                        append(it.title)
                        append(" (")
                        append(it.version)
                        append(")")
                        if (it.installedVersion != null) append(" installed")
                        if (it.updateAvailable) append(" update")
                        if (it.reviewStatus != "approved" && it.sourceType != "dev") append(" ${it.reviewStatus}")
                    }
                }
                runOnUiThread {
                    listView.adapter = ArrayAdapter(this, android.R.layout.simple_list_item_1, labels)
                    statusText.text = "Loaded ${items.size} app(s). Tap to install/open."
                }
            } catch (error: Throwable) {
                runOnUiThread { statusText.text = error.message ?: "Failed to load catalog." }
            }
        }
    }

    private fun openSelected(entry: MiniAppCatalogEntry) {
        if (entry.reviewStatus != "approved" && entry.sourceType != "dev" && !devModeSwitch.isChecked) {
            Toast.makeText(this, "This app is not approved for install yet.", Toast.LENGTH_SHORT).show()
            return
        }
        installStore.markInstalled(entry.appId, entry.version)
        startActivity(
            Intent(this, MiniAppRuntimeActivity::class.java)
                .putExtra("app_id", entry.appId)
                .putExtra("app_version", entry.version)
                .putExtra("entrypoint_url", entry.entrypointUrl)
        )
    }
}

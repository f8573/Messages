package dev.ohmf.miniapphost

import android.annotation.SuppressLint
import android.os.Bundle
import android.webkit.WebChromeClient
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import java.net.URLEncoder
import java.util.UUID

class MiniAppRuntimeActivity : AppCompatActivity() {
    private lateinit var statusView: TextView
    private lateinit var webView: WebView

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_runtime)

        statusView = findViewById(R.id.runtime_status)
        webView = findViewById(R.id.runtime_webview)

        val appId = intent.getStringExtra("app_id").orEmpty()
        val appVersion = intent.getStringExtra("app_version").orEmpty()
        val entrypointUrl = intent.getStringExtra("entrypoint_url").orEmpty()
        val channel = "chan_${UUID.randomUUID()}".replace("-", "")

        val bridge = MiniAppBridge(
            appId = appId,
            appVersion = appVersion,
            grantedPermissions = listOf(
                "conversation.read_context",
                "conversation.send_message",
                "participants.read_basic",
                "storage.session",
                "storage.shared_conversation",
                "realtime.session",
                "notifications.in_app"
            )
        )

        webView.settings.javaScriptEnabled = true
        webView.settings.domStorageEnabled = true
        webView.settings.allowFileAccess = false
        webView.settings.allowContentAccess = false
        webView.settings.javaScriptCanOpenWindowsAutomatically = false
        webView.addJavascriptInterface(bridge, "MiniAppHostBridge")
        webView.webChromeClient = WebChromeClient()
        webView.webViewClient = object : WebViewClient() {}

        val shellUrl = buildString {
            append("file:///android_asset/miniapp_host_shell.html")
            append("?entrypoint=")
            append(URLEncoder.encode(entrypointUrl, Charsets.UTF_8.name()))
            append("&app_id=")
            append(URLEncoder.encode(appId, Charsets.UTF_8.name()))
            append("&channel=")
            append(URLEncoder.encode(channel, Charsets.UTF_8.name()))
            append("&parent_origin=")
            append(URLEncoder.encode("app://ohmf-miniapp-host", Charsets.UTF_8.name()))
        }

        statusView.text = "Launching $appId"
        webView.loadUrl(shellUrl)
    }
}

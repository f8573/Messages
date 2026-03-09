package types

// MiniAppEntrypoint describes how to load the mini-app.
type MiniAppEntrypoint struct {
    Type    string `json:"type"`              // "url" or "inline"
    URL     string `json:"url,omitempty"`
    MimeType string `json:"mime_type,omitempty"`
}

// MiniAppIcon is a UI asset reference for the mini-app.
type MiniAppIcon struct {
    Src   string `json:"src"`
    Sizes string `json:"sizes"`
    Type  string `json:"type,omitempty"`
}

// MiniAppSignature holds an integrity signature for the manifest.
type MiniAppSignature struct {
    Alg string `json:"alg"`
    Kid string `json:"kid"`
    Sig string `json:"sig"`
}

// MiniAppManifest is the top-level manifest for a mini-app.
type MiniAppManifest struct {
    AppID       string                 `json:"app_id"`
    Name        string                 `json:"name"`
    Version     string                 `json:"version"`
    Entrypoint  MiniAppEntrypoint      `json:"entrypoint"`
    Icons       []MiniAppIcon          `json:"icons,omitempty"`
    Permissions []string               `json:"permissions"`
    Capabilities map[string]interface{} `json:"capabilities"`
    Metadata    map[string]interface{} `json:"metadata,omitempty"`
    Signature   MiniAppSignature       `json:"signature"`
}

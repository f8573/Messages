package types

// MiniAppEntrypoint describes how to load the mini-app.
type MiniAppEntrypoint struct {
	Type     string `json:"type"` // "url", "inline", or "web_bundle"
	URL      string `json:"url,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
}

// MiniAppIcon is a UI asset reference for the mini-app.
type MiniAppIcon struct {
	Src   string `json:"src"`
	Sizes string `json:"sizes"`
	Type  string `json:"type,omitempty"`
	URL   string `json:"url,omitempty"`
	Size  int    `json:"size,omitempty"`
}

// MiniAppMessagePreview defines the preview shown when a mini-app is shared into a conversation.
type MiniAppMessagePreview struct {
	Type    string `json:"type"` // "static_image" or "live"
	URL     string `json:"url"`  // Preview asset or live preview URL
	AltText string `json:"alt_text,omitempty"`
	FitMode string `json:"fit_mode,omitempty"` // "scale" or "crop"; defaults to "scale"
}

// MiniAppSignature holds an integrity signature for the manifest.
type MiniAppSignature struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Sig string `json:"sig"`
}

// MiniAppManifest is the top-level manifest for a mini-app.
type MiniAppManifest struct {
	ManifestVersion string                 `json:"manifest_version,omitempty"`
	AppID           string                 `json:"app_id"`
	Name            string                 `json:"name"`
	Version         string                 `json:"version"`
	Entrypoint      MiniAppEntrypoint      `json:"entrypoint"`
	Icons           []MiniAppIcon          `json:"icons,omitempty"`
	MessagePreview  MiniAppMessagePreview  `json:"message_preview"`
	Permissions     []string               `json:"permissions"`
	Capabilities    map[string]interface{} `json:"capabilities"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	Signature       *MiniAppSignature      `json:"signature,omitempty"`
}

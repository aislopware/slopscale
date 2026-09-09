package hscontrol

import (
	"bytes"
	"net/http"
	textTemplate "text/template"
	"uuid"

	"github.com/aislopware/slopscale/hscontrol/templates"
	"github.com/go-chi/chi/v5"
)

// WindowsConfigMessage shows a simple message in the browser for how to configure the Windows Tailscale client.
func (h *Slopscale) WindowsConfigMessage(
	writer http.ResponseWriter,
	_ *http.Request,
) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write([]byte(templates.Windows(h.cfg.ServerURL).Render()))
}

// AppleConfigMessage shows a simple message in the browser to point the user to
// the iOS/MacOS profile and instructions for how to install it.
func (h *Slopscale) AppleConfigMessage(
	writer http.ResponseWriter,
	_ *http.Request,
) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write([]byte(templates.Apple(h.cfg.ServerURL).Render()))
}

func (h *Slopscale) ApplePlatformConfig(
	writer http.ResponseWriter,
	req *http.Request,
) {
	platform := chi.URLParam(req, "platform")
	if platform == "" {
		httpError(writer, NewHTTPError(http.StatusBadRequest, "no platform specified", nil))
		return
	}

	id := uuid.NewV4()
	contentID := uuid.NewV4()

	platformConfig := AppleMobilePlatformConfig{
		UUID: contentID,
		URL:  h.cfg.ServerURL,
	}

	payloadType, ok := applePayloadType[platform]
	if !ok {
		httpError(
			writer,
			NewHTTPError(http.StatusBadRequest, "platform must be ios, macos-app-store or macos-standalone", nil),
		)

		return
	}

	platformConfig.PayloadType = payloadType

	var payload bytes.Buffer

	err := payloadTemplate.Execute(&payload, platformConfig)
	if err != nil {
		httpError(writer, err)
		return
	}

	config := AppleMobileConfig{
		UUID:    id,
		URL:     h.cfg.ServerURL,
		Payload: payload.String(),
	}

	var content bytes.Buffer

	err = commonTemplate.Execute(&content, config)
	if err != nil {
		httpError(writer, err)
		return
	}

	writer.Header().
		Set("Content-Type", "application/x-apple-aspen-config; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(content.Bytes())
}

type AppleMobileConfig struct {
	UUID    uuid.UUID
	URL     string
	Payload string
}

type AppleMobilePlatformConfig struct {
	UUID        uuid.UUID
	URL         string
	PayloadType string
}

// applePayloadType maps a platform request path to the Tailscale IPN
// PayloadType emitted in the rendered Apple profile.
var applePayloadType = map[string]string{
	"ios":              "io.tailscale.ipn.ios",
	"macos-app-store":  "io.tailscale.ipn.macos",
	"macos-standalone": "io.tailscale.ipn.macsys",
}

var commonTemplate = textTemplate.Must(
	textTemplate.New("mobileconfig").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>PayloadUUID</key>
    <string>{{.UUID}}</string>
    <key>PayloadDisplayName</key>
    <string>Slopscale</string>
    <key>PayloadDescription</key>
    <string>Configure Tailscale login server to: {{.URL}}</string>
    <key>PayloadIdentifier</key>
    <string>com.github.juanfont.slopscale</string>
    <key>PayloadRemovalDisallowed</key>
    <false/>
    <key>PayloadType</key>
    <string>Configuration</string>
    <key>PayloadVersion</key>
    <integer>1</integer>
    <key>PayloadContent</key>
    <array>
    {{.Payload}}
    </array>
  </dict>
</plist>`),
)

var payloadTemplate = textTemplate.Must(textTemplate.New("payloadTemplate").Parse(`
    <dict>
        <key>PayloadType</key>
        <string>{{.PayloadType}}</string>
        <key>PayloadUUID</key>
        <string>{{.UUID}}</string>
        <key>PayloadIdentifier</key>
        <string>com.github.juanfont.slopscale</string>
        <key>PayloadVersion</key>
        <integer>1</integer>
        <key>PayloadEnabled</key>
        <true/>

        <key>ControlURL</key>
        <string>{{.URL}}</string>
    </dict>
`))

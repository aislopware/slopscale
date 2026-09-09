package integration

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/types"
)

// kolide is Kolide, now 1Password Extended Access Management: an API key
// against the public API, devices searched by serial. The one attribute
// Tailscale exposes is the device's authentication state.
type kolide struct {
	*httpDoer

	cfg types.PostureIntegrationConfig
}

// kolideURL is the public API; BaseURL replaces it in tests.
const kolideURL = "https://api.kolide.com"

func (k *kolide) Check(ctx context.Context) error {
	return k.doJSON(ctx, request{
		method:  http.MethodGet,
		url:     k.base() + "/devices?per_page=1",
		headers: k.headers(),
	}, nil)
}

type kolideDevice struct {
	Serial    string `json:"serial"`
	AuthState string `json:"auth_state"`
}

// authState renders Kolide's state the way Tailscale's attribute reads:
// Good, Notified, Will Block, Blocked.
func authState(s string) string {
	words := strings.Fields(strings.ReplaceAll(strings.ToLower(s), "_", " "))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}

	return strings.Join(words, " ")
}

func (d kolideDevice) attributes() Attributes {
	return Attributes{"authState": authState(d.AuthState)}
}

func (k *kolide) Lookup(ctx context.Context, serials []string) (map[string]Attributes, error) {
	out := map[string]Attributes{}

	for _, serial := range serials {
		q := url.Values{"query": {"serial:" + strip(serial)}, "per_page": {"5"}}

		var resp struct {
			Data []kolideDevice `json:"data"`
		}

		err := k.doJSON(ctx, request{
			method:  http.MethodGet,
			url:     k.base() + "/devices?" + q.Encode(),
			headers: k.headers(),
		}, &resp)
		if err != nil {
			return nil, err
		}

		for _, d := range resp.Data {
			if strings.EqualFold(d.Serial, serial) && d.AuthState != "" {
				out[serial] = d.attributes()

				break
			}
		}
	}

	return out, nil
}

func itoa(i int) string {
	return strconv.Itoa(i)
}

func (k *kolide) base() string {
	if k.cfg.BaseURL != "" {
		return k.cfg.BaseURL
	}

	return kolideURL
}

func (k *kolide) headers() map[string]string {
	return map[string]string{authorization: "Bearer " + k.cfg.APIToken}
}

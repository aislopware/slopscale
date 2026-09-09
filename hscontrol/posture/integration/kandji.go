package integration

import (
	"context"
	"net/http"
	"net/url"

	"github.com/aislopware/slopscale/hscontrol/types"
)

// kandji is Kandji, now Iru: an API token against the tenant's API host,
// devices listed one serial at a time.
type kandji struct {
	*httpDoer

	cfg types.PostureIntegrationConfig
}

func (k *kandji) Check(ctx context.Context) error {
	return k.doJSON(ctx, request{
		method:  http.MethodGet,
		url:     k.cfg.BaseURL + "/api/v1/devices?limit=1",
		headers: k.headers(),
	}, nil)
}

type kandjiDevice struct {
	SerialNumber   string `json:"serial_number"`
	MDMEnabled     bool   `json:"mdm_enabled"`
	AgentInstalled bool   `json:"agent_installed"`
}

func (d kandjiDevice) attributes() Attributes {
	return Attributes{"mdmEnabled": d.MDMEnabled, "agentInstalled": d.AgentInstalled}
}

func (k *kandji) Lookup(ctx context.Context, serials []string) (map[string]Attributes, error) {
	out := map[string]Attributes{}

	for _, serial := range serials {
		q := url.Values{"serial_number": {serial}, "limit": {"5"}}

		var devices []kandjiDevice

		err := k.doJSON(ctx, request{
			method:  http.MethodGet,
			url:     k.cfg.BaseURL + "/api/v1/devices?" + q.Encode(),
			headers: k.headers(),
		}, &devices)
		if err != nil {
			return nil, err
		}

		for _, d := range devices {
			if matchSerial([]string{serial}, d.SerialNumber) == serial && d.SerialNumber != "" {
				out[serial] = d.attributes()

				break
			}
		}
	}

	return out, nil
}

func (k *kandji) headers() map[string]string {
	return map[string]string{authorization: "Bearer " + k.cfg.APIToken}
}

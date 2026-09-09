package integration

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
)

// jamf is Jamf Pro: an API client with the Read Computers privilege,
// exchanged for a bearer token. Computers are read from the inventory
// with an RSQL filter on the hardware serial number.
type jamf struct {
	*httpDoer

	cfg types.PostureIntegrationConfig

	mu    sync.Mutex
	token bearerToken
}

// jamfBatch is how many serials go in one RSQL filter.
const jamfBatch = 20

func (j *jamf) Check(ctx context.Context) error {
	token, err := j.bearer(ctx)
	if err != nil {
		return err
	}

	return j.doJSON(ctx, request{
		method:  http.MethodGet,
		url:     j.cfg.BaseURL + "/api/v1/computers-inventory?section=GENERAL&page-size=1",
		headers: map[string]string{authorization: "Bearer " + token},
	}, nil)
}

type jamfComputer struct {
	General struct {
		Supervised       bool `json:"supervised"`
		RemoteManagement struct {
			Managed bool `json:"managed"`
		} `json:"remoteManagement"`
	} `json:"general"`
	Hardware struct {
		SerialNumber string `json:"serialNumber"`
	} `json:"hardware"`
	Security struct {
		FirewallEnabled  bool   `json:"firewallEnabled"`
		FileVault2Status string `json:"fileVault2Status"`
		SIPStatus        string `json:"sipStatus"`
	} `json:"security"`
}

func (c jamfComputer) attributes() Attributes {
	return Attributes{
		"remoteManaged":   c.General.RemoteManagement.Managed,
		"supervised":      c.General.Supervised,
		"firewallEnabled": c.Security.FirewallEnabled,
		"fileVaultStatus": c.Security.FileVault2Status,
		"SIPEnabled":      c.Security.SIPStatus,
	}
}

func (j *jamf) Lookup(ctx context.Context, serials []string) (map[string]Attributes, error) {
	out := map[string]Attributes{}

	for _, batch := range batches(serials, jamfBatch) {
		clauses := make([]string, 0, len(batch))
		for _, s := range batch {
			clauses = append(clauses, `hardware.serialNumber=="`+strip(s)+`"`)
		}

		page := 0

		for {
			token, err := j.bearer(ctx)
			if err != nil {
				return nil, err
			}

			q := url.Values{
				"section":   {"GENERAL", "HARDWARE", "SECURITY"},
				"page":      {itoa(page)},
				"page-size": {"100"},
				"filter":    {strings.Join(clauses, ",")},
			}

			var resp struct {
				TotalCount int            `json:"totalCount"`
				Results    []jamfComputer `json:"results"`
			}

			err = j.doJSON(ctx, request{
				method:  http.MethodGet,
				url:     j.cfg.BaseURL + "/api/v1/computers-inventory?" + q.Encode(),
				headers: map[string]string{authorization: "Bearer " + token},
			}, &resp)
			if err != nil {
				return nil, err
			}

			for _, c := range resp.Results {
				if c.Hardware.SerialNumber != "" {
					out[matchSerial(batch, c.Hardware.SerialNumber)] = c.attributes()
				}
			}

			page++
			if len(resp.Results) == 0 || page*100 >= resp.TotalCount {
				break
			}
		}
	}

	return out, nil
}

func (j *jamf) bearer(ctx context.Context) (string, error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	now := time.Now()
	if j.token.valid(now) {
		return j.token.value, nil
	}

	var resp tokenResponse

	err := j.doJSON(ctx, request{
		method: http.MethodPost,
		url:    j.cfg.BaseURL + "/api/v1/oauth/token",
		form: url.Values{
			"grant_type":    {"client_credentials"},
			"client_id":     {j.cfg.ClientID},
			"client_secret": {j.cfg.ClientSecret},
		},
	}, &resp)
	if err != nil {
		return "", err
	}

	j.token, err = resp.token(now)
	if err != nil {
		return "", err
	}

	return j.token.value, nil
}

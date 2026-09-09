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

// intune is Microsoft Intune through the Graph API: an Entra app
// registration with the DeviceManagementManagedDevices.Read.All
// application permission. Devices are filtered by serial number.
type intune struct {
	*httpDoer

	cfg types.PostureIntegrationConfig

	mu    sync.Mutex
	token bearerToken
}

// The Microsoft endpoints; BaseURL replaces both in tests.
const (
	intuneLoginURL = "https://login.microsoftonline.com"
	intuneGraphURL = "https://graph.microsoft.com"
)

// intuneBatch is how many serials go in one $filter.
const intuneBatch = 15

func (i *intune) Check(ctx context.Context) error {
	token, err := i.bearer(ctx)
	if err != nil {
		return err
	}

	return i.doJSON(ctx, request{
		method:  http.MethodGet,
		url:     i.graphURL() + "/v1.0/deviceManagement/managedDevices?$top=1&$select=id",
		headers: map[string]string{authorization: "Bearer " + token},
	}, nil)
}

type intuneDevice struct {
	SerialNumber            string `json:"serialNumber"`
	ComplianceState         string `json:"complianceState"`
	AzureADRegistered       bool   `json:"azureADRegistered"`
	DeviceRegistrationState string `json:"deviceRegistrationState"`
	IsSupervised            bool   `json:"isSupervised"`
	IsEncrypted             bool   `json:"isEncrypted"`
	ManagedDeviceOwnerType  string `json:"managedDeviceOwnerType"`
}

func (d intuneDevice) attributes() Attributes {
	return Attributes{
		"complianceState":         d.ComplianceState,
		"azureADRegistered":       d.AzureADRegistered,
		"deviceRegistrationState": d.DeviceRegistrationState,
		"isSupervised":            d.IsSupervised,
		"isEncrypted":             d.IsEncrypted,
		"managedDeviceOwnerType":  d.ManagedDeviceOwnerType,
	}
}

func (i *intune) Lookup(ctx context.Context, serials []string) (map[string]Attributes, error) {
	out := map[string]Attributes{}

	for _, batch := range batches(serials, intuneBatch) {
		clauses := make([]string, 0, len(batch))
		for _, s := range batch {
			clauses = append(clauses, "serialNumber eq '"+strings.ReplaceAll(s, "'", "''")+"'")
		}

		q := url.Values{
			"$filter": {strings.Join(clauses, " or ")},
			"$select": {"serialNumber,complianceState,azureADRegistered,deviceRegistrationState," +
				"isSupervised,isEncrypted,managedDeviceOwnerType"},
		}

		next := i.graphURL() + "/v1.0/deviceManagement/managedDevices?" + q.Encode()

		for next != "" {
			token, err := i.bearer(ctx)
			if err != nil {
				return nil, err
			}

			var resp struct {
				Value    []intuneDevice `json:"value"`
				NextLink string         `json:"@odata.nextLink"`
			}

			err = i.doJSON(ctx, request{
				method:  http.MethodGet,
				url:     next,
				headers: map[string]string{authorization: "Bearer " + token},
			}, &resp)
			if err != nil {
				return nil, err
			}

			for _, d := range resp.Value {
				if d.SerialNumber != "" {
					out[matchSerial(batch, d.SerialNumber)] = d.attributes()
				}
			}

			next = resp.NextLink
		}
	}

	return out, nil
}

func (i *intune) loginURL() string {
	if i.cfg.BaseURL != "" {
		return i.cfg.BaseURL
	}

	return intuneLoginURL
}

func (i *intune) graphURL() string {
	if i.cfg.BaseURL != "" {
		return i.cfg.BaseURL
	}

	return intuneGraphURL
}

func (i *intune) bearer(ctx context.Context) (string, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	now := time.Now()
	if i.token.valid(now) {
		return i.token.value, nil
	}

	var resp tokenResponse

	err := i.doJSON(ctx, request{
		method: http.MethodPost,
		url:    i.loginURL() + "/" + url.PathEscape(i.cfg.TenantID) + "/oauth2/v2.0/token",
		form: url.Values{
			"grant_type":    {"client_credentials"},
			"client_id":     {i.cfg.ClientID},
			"client_secret": {i.cfg.ClientSecret},
			"scope":         {intuneGraphURL + "/.default"},
		},
	}, &resp)
	if err != nil {
		return "", err
	}

	i.token, err = resp.token(now)
	if err != nil {
		return "", err
	}

	return i.token.value, nil
}

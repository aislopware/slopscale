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

// falcon is CrowdStrike Falcon: an OAuth client with the Hosts:Read and
// Zero Trust Assessment:Read scopes. The one attribute Tailscale exposes
// is the Zero Trust Assessment score.
//
// Hosts are found by serial number through the hosts query, then the
// assessment is read per host id.
type falcon struct {
	*httpDoer

	cfg types.PostureIntegrationConfig

	mu    sync.Mutex
	token bearerToken
}

// falconBatch is how many ids a Falcon entity request takes.
const falconBatch = 100

func (f *falcon) Check(ctx context.Context) error {
	_, err := f.bearer(ctx)

	return err
}

func (f *falcon) Lookup(ctx context.Context, serials []string) (map[string]Attributes, error) {
	out := map[string]Attributes{}

	for _, batch := range batches(serials, falconBatch) {
		ids, err := f.hostIDs(ctx, batch)
		if err != nil {
			return nil, err
		}

		if len(ids) == 0 {
			continue
		}

		serialByID, err := f.serials(ctx, ids)
		if err != nil {
			return nil, err
		}

		scores, err := f.assessments(ctx, ids)
		if err != nil {
			return nil, err
		}

		for id, score := range scores {
			serial, ok := serialByID[id]
			if !ok {
				continue
			}

			out[serial] = Attributes{"ztaScore": score}
		}
	}

	return out, nil
}

// hostIDs resolves serial numbers to host ids with an FQL filter.
func (f *falcon) hostIDs(ctx context.Context, serials []string) ([]string, error) {
	token, err := f.bearer(ctx)
	if err != nil {
		return nil, err
	}

	quoted := make([]string, 0, len(serials))
	for _, s := range serials {
		quoted = append(quoted, "'"+strip(s)+"'")
	}

	q := url.Values{
		"filter": {"serial_number:[" + strings.Join(quoted, ",") + "]"},
		"limit":  {"500"},
	}

	var resp struct {
		Resources []string `json:"resources"`
	}

	err = f.doJSON(ctx, request{
		method:  http.MethodGet,
		url:     f.cfg.BaseURL + "/devices/queries/devices/v1?" + q.Encode(),
		headers: map[string]string{authorization: "Bearer " + token},
	}, &resp)
	if err != nil {
		return nil, err
	}

	return resp.Resources, nil
}

// serials reads the serial number of each host id.
func (f *falcon) serials(ctx context.Context, ids []string) (map[string]string, error) {
	token, err := f.bearer(ctx)
	if err != nil {
		return nil, err
	}

	q := url.Values{"ids": ids}

	var resp struct {
		Resources []struct {
			DeviceID     string `json:"device_id"`
			SerialNumber string `json:"serial_number"`
		} `json:"resources"`
	}

	err = f.doJSON(ctx, request{
		method:  http.MethodGet,
		url:     f.cfg.BaseURL + "/devices/entities/devices/v2?" + q.Encode(),
		headers: map[string]string{authorization: "Bearer " + token},
	}, &resp)
	if err != nil {
		return nil, err
	}

	out := make(map[string]string, len(resp.Resources))
	for _, r := range resp.Resources {
		if r.SerialNumber != "" {
			out[r.DeviceID] = r.SerialNumber
		}
	}

	return out, nil
}

// assessments reads the Zero Trust Assessment score of each host id.
func (f *falcon) assessments(ctx context.Context, ids []string) (map[string]float64, error) {
	token, err := f.bearer(ctx)
	if err != nil {
		return nil, err
	}

	q := url.Values{"ids": ids}

	var resp struct {
		Resources []struct {
			AID        string `json:"aid"`
			Assessment struct {
				Overall float64 `json:"overall"`
			} `json:"assessment"`
		} `json:"resources"`
	}

	err = f.doJSON(ctx, request{
		method:  http.MethodGet,
		url:     f.cfg.BaseURL + "/zero-trust-assessment/entities/assessments/v1?" + q.Encode(),
		headers: map[string]string{authorization: "Bearer " + token},
	}, &resp)
	if err != nil {
		return nil, err
	}

	out := make(map[string]float64, len(resp.Resources))
	for _, r := range resp.Resources {
		out[r.AID] = r.Assessment.Overall
	}

	return out, nil
}

func (f *falcon) bearer(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := time.Now()
	if f.token.valid(now) {
		return f.token.value, nil
	}

	var resp tokenResponse

	err := f.doJSON(ctx, request{
		method: http.MethodPost,
		url:    f.cfg.BaseURL + "/oauth2/token",
		form:   url.Values{"client_id": {f.cfg.ClientID}, "client_secret": {f.cfg.ClientSecret}},
	}, &resp)
	if err != nil {
		return "", err
	}

	f.token, err = resp.token(now)
	if err != nil {
		return "", err
	}

	return f.token.value, nil
}

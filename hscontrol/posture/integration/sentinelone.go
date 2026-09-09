package integration

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/types"
)

// sentinelOne is SentinelOne: a service user's API token against the
// tenant console. Agents are listed with a serial number filter; the
// filter is a contains match, so the answer is matched exactly here.
type sentinelOne struct {
	*httpDoer

	cfg types.PostureIntegrationConfig
}

// sentinelOneBatch is how many serials go in one filter.
const sentinelOneBatch = 50

func (s *sentinelOne) Check(ctx context.Context) error {
	return s.doJSON(ctx, request{
		method:  http.MethodGet,
		url:     s.cfg.BaseURL + "/web/api/v2.1/agents?limit=1",
		headers: s.headers(),
	}, nil)
}

type sentinelOneAgent struct {
	SerialNumber          string `json:"serialNumber"`
	OperationalState      string `json:"operationalState"`
	ActiveThreats         int    `json:"activeThreats"`
	AgentVersion          string `json:"agentVersion"`
	EncryptedApplications bool   `json:"encryptedApplications"`
	FirewallEnabled       bool   `json:"firewallEnabled"`
	Infected              bool   `json:"infected"`
}

func (a sentinelOneAgent) attributes() Attributes {
	return Attributes{
		"operationalState":      a.OperationalState,
		"activeThreats":         a.ActiveThreats,
		"agentVersion":          a.AgentVersion,
		"encryptedApplications": a.EncryptedApplications,
		"firewallEnabled":       a.FirewallEnabled,
		"infected":              a.Infected,
	}
}

func (s *sentinelOne) Lookup(ctx context.Context, serials []string) (map[string]Attributes, error) {
	out := map[string]Attributes{}

	for _, batch := range batches(serials, sentinelOneBatch) {
		wanted := make(map[string]bool, len(batch))
		clean := make([]string, 0, len(batch))

		for _, serial := range batch {
			wanted[strings.ToLower(serial)] = true
			clean = append(clean, strip(serial))
		}

		cursor := ""

		for {
			q := url.Values{
				"serialNumberContains": {strings.Join(clean, ",")},
				"limit":                {strconv.Itoa(sentinelOneBatch * 2)},
			}
			if cursor != "" {
				q.Set("cursor", cursor)
			}

			var resp struct {
				Data       []sentinelOneAgent `json:"data"`
				Pagination struct {
					NextCursor string `json:"nextCursor"`
				} `json:"pagination"`
			}

			err := s.doJSON(ctx, request{
				method:  http.MethodGet,
				url:     s.cfg.BaseURL + "/web/api/v2.1/agents?" + q.Encode(),
				headers: s.headers(),
			}, &resp)
			if err != nil {
				return nil, err
			}

			for _, agent := range resp.Data {
				if wanted[strings.ToLower(agent.SerialNumber)] {
					out[matchSerial(batch, agent.SerialNumber)] = agent.attributes()
				}
			}

			cursor = resp.Pagination.NextCursor
			if cursor == "" {
				break
			}
		}
	}

	return out, nil
}

// matchSerial returns the serial as the caller spelled it, so the answer
// is keyed the way the node reports it.
func matchSerial(serials []string, found string) string {
	for _, s := range serials {
		if strings.EqualFold(s, found) {
			return s
		}
	}

	return found
}

func (s *sentinelOne) headers() map[string]string {
	return map[string]string{authorization: "ApiToken " + s.cfg.APIToken}
}

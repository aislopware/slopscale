package apiv2

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/api/principal"
	"github.com/aislopware/slopscale/hscontrol/audit"
	policyv2 "github.com/aislopware/slopscale/hscontrol/policy/v2"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/danielgtaylor/huma/v2"
	"github.com/tailscale/hujson"
)

func init() {
	registrations = append(registrations, registerACLValidate)
}

// aclTest is Tailscale's ACLTest wire shape, the entries the Terraform
// provider and the Go client send to /acl/validate. Src/Accept are the
// current spelling; User/Allow are the older one Tailscale still accepts.
type aclTest struct {
	User            string         `json:"user,omitempty"`
	Allow           []string       `json:"allow,omitempty"`
	Deny            []string       `json:"deny,omitempty"`
	Source          string         `json:"src,omitempty"`
	Accept          []string       `json:"accept,omitempty"`
	SrcPostureAttrs map[string]any `json:"srcPostureAttrs,omitempty"`
}

// validateACLInput takes the policy document (JSON or HuJSON) or a test
// list; the bytes are captured raw so HuJSON survives, as in setACL.
type validateACLInput struct {
	Tailnet string `path:"tailnet"`
	RawBody []byte `contentType:"application/json"`
}

// validateACLOutput is Tailscale's validate response: 200 either way, with
// an empty message when the policy and its tests pass and the failure in
// message/data when they do not. The Go client (and so the Terraform
// provider) reads a non-empty message as the failure.
type validateACLOutput struct {
	Body apiError
}

func registerACLValidate(api huma.API, b Backend) {
	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID: "validateACL",
		Method:      http.MethodPost,
		Path:        "/api/v2/tailnet/{tailnet}/acl/validate",
		Summary:     "Validate a policy file",
		Description: "Checks a policy document (JSON or HuJSON) against the tailnet's users and nodes " +
			"and runs its tests, without storing it. A body of just `{\"tests\":[…]}`, or a bare list of " +
			"tests, runs those tests against the policy in force instead. Both outcomes answer 200: the " +
			"message is empty when everything passed and carries the failure when it did not.",
		Tags:     []string{"Policy", tagTailscaleCompat},
		Security: security,
		// The body is an opaque HuJSON document captured raw; skip huma's
		// schema validation, which would expect a base64 string.
		SkipValidateBody: true,
		Errors: []int{
			http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
			http.StatusNotFound, http.StatusInternalServerError,
		},
	}, scope.PolicyFileRead), "policy.validate", "policy", ""), func(
		ctx context.Context, in *validateACLInput,
	) (*validateACLOutput, error) {
		err := requireDefaultTailnet(in.Tailnet)
		if err != nil {
			return nil, err
		}

		audit.Detail(ctx, "bytes", len(in.RawBody))

		document, err := policyToValidate(b, in.RawBody)
		if err != nil {
			return nil, err
		}

		return &validateACLOutput{Body: validationResult(b, document)}, nil
	})
}

// policyToValidate returns the policy document the request asks to check:
// the body itself for a policy, or the stored policy with its tests block
// replaced for a test list.
func policyToValidate(b Backend, body []byte) ([]byte, error) {
	tests, testsOnly, err := parseTestList(body)
	if err != nil {
		return nil, err
	}

	if !testsOnly {
		return body, nil
	}

	current, err := currentPolicy(b)
	if err != nil {
		return nil, err
	}

	return withTests(current, tests)
}

// parseTestList reports whether the body is a test list rather than a whole
// policy, and decodes it when it is. Tailscale's client sends a bare JSON
// array; its API documents {"tests":[…]}. Anything else (including a policy
// that happens to carry a tests block) is a policy document.
func parseTestList(body []byte) ([]aclTest, bool, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, false, nil
	}

	if trimmed[0] == '[' {
		var tests []aclTest

		err := json.Unmarshal(trimmed, &tests)
		if err != nil {
			return nil, false, huma.Error400BadRequest("parsing the test list", err)
		}

		return tests, true, nil
	}

	var members map[string]json.RawMessage

	// A HuJSON body may carry comments and trailing commas, so it is
	// standardized before the shape check; a parse failure here is left to
	// the policy path, which reports it as a validation failure.
	standard, err := standardize(trimmed)
	if err != nil {
		return nil, false, nil //nolint:nilerr // a malformed document is a validation failure, not a request error
	}

	err = json.Unmarshal(standard, &members)
	if err != nil {
		return nil, false, nil //nolint:nilerr // ditto
	}

	raw, ok := members["tests"]
	if !ok || len(members) != 1 {
		return nil, false, nil
	}

	var tests []aclTest

	err = json.Unmarshal(raw, &tests)
	if err != nil {
		return nil, false, huma.Error400BadRequest("parsing the test list", err)
	}

	return tests, true, nil
}

// withTests returns the policy with its tests block replaced by the given
// list, so a test-only request runs against the policy in force.
func withTests(policy []byte, tests []aclTest) ([]byte, error) {
	standard, err := standardize(policy)
	if err != nil {
		return nil, internalError("reading the stored policy", err)
	}

	members := map[string]json.RawMessage{}

	err = json.Unmarshal(standard, &members)
	if err != nil {
		return nil, internalError("reading the stored policy", err)
	}

	converted, err := slopscaleTests(tests)
	if err != nil {
		return nil, err
	}

	members["tests"] = converted

	out, err := json.Marshal(members)
	if err != nil {
		return nil, internalError("rebuilding the policy", err)
	}

	return out, nil
}

// slopscaleTests renders Tailscale's test entries in slopscale's spelling
// (src/accept/deny). srcPostureAttrs has no equivalent: slopscale evaluates
// posture against a node's collected attributes, not against attributes the
// request invents, so a test that asks for it is refused rather than passed
// vacuously.
func slopscaleTests(tests []aclTest) (json.RawMessage, error) {
	out := make([]policyv2.PolicyTest, 0, len(tests))

	for _, t := range tests {
		if len(t.SrcPostureAttrs) > 0 {
			return nil, huma.Error400BadRequest(
				"srcPostureAttrs in an ACL test is not supported; posture is evaluated " +
					"against the attributes a node reports",
			)
		}

		src := t.Source
		if src == "" {
			src = t.User
		}

		out = append(out, policyv2.PolicyTest{
			Src:    src,
			Accept: append(append([]string{}, t.Accept...), t.Allow...),
			Deny:   t.Deny,
		})
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("encoding policy tests: %w", err)
	}

	return encoded, nil
}

// standardize turns a HuJSON document into plain JSON. hujson aliases its
// input and blanks comments in place, so the bytes are cloned first.
func standardize(document []byte) ([]byte, error) {
	ast, err := hujson.Parse(bytes.Clone(document))
	if err != nil {
		return nil, fmt.Errorf("parsing HuJSON: %w", err)
	}

	ast.Standardize()

	return ast.Pack(), nil
}

// validationResult compiles the document against the live users and nodes
// and runs its tests, the same boundary a write goes through, and renders
// the outcome in Tailscale's body.
func validationResult(b Backend, document []byte) apiError {
	users, err := b.State.ListAllUsers()
	if err != nil {
		return failedValidation(err)
	}

	nodes := b.State.ListNodes()

	pm, err := policyv2.NewPolicyManager(document, users, nodes)
	if err != nil {
		return failedValidation(err)
	}

	// SetPolicy is the user-write boundary: it is what runs the tests and
	// sshTests blocks that NewPolicyManager deliberately skips.
	_, err = pm.SetPolicy(document)
	if err != nil {
		return failedValidation(err)
	}

	return apiError{Status: http.StatusOK}
}

// failedValidation renders one failure in Tailscale's body. The engine
// reports a test breakdown as one error per line, which becomes the data
// entry's errors list; the message is the first line, the summary.
func failedValidation(err error) apiError {
	lines := strings.Split(err.Error(), "\n")

	out := apiError{Message: lines[0], Status: http.StatusOK}
	if len(lines) > 1 {
		out.Data = []apiErrorData{{Errors: lines[1:]}}
	}

	return out
}

package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"tailscale.com/tailcfg"
)

// PostureIdentity is what a client reported when the server asked it for
// its device identity over c2n; see docs/ref/device-trust.md. The map
// request path never writes it.
type PostureIdentity struct {
	// SerialNumbers are the hardware serial numbers the client found,
	// empty when it found none or refused.
	SerialNumbers []string `json:"serialNumbers"`
	// Disabled is set when the client has posture checking switched off
	// (`tailscale set --posture-checking=false`, or an MDM key), in which
	// case it reports nothing.
	Disabled bool `json:"disabled,omitempty"`
	// CollectedAt is when the client answered.
	CollectedAt time.Time `json:"collectedAt"`
}

// NodeAttribute is one custom posture attribute on a node, set through
// the API the way Tailscale's device posture attributes are. The key
// starts with "custom:"; the value is a string, a number or a bool.
type NodeAttribute struct {
	Key   string         `json:"key"`
	Value AttributeValue `json:"value"`
	// ExpiresAt, when not zero, is when the attribute disappears on its
	// own.
	ExpiresAt time.Time `json:"expiresAt,omitzero"`
	Comment   string    `json:"comment,omitempty"`
}

// AttributeValue is a string, a number or a bool, one of them set. It is
// a struct rather than an interface so the generated cloner and viewer
// can copy it; [AttributeValue.Any] gives the plain value back.
type AttributeValue struct {
	Kind   AttributeKind `json:"kind"`
	String string        `json:"string,omitempty"`
	Number float64       `json:"number,omitempty"`
	Bool   bool          `json:"bool,omitempty"`
}

// AttributeKind tells which field of an [AttributeValue] is set.
type AttributeKind string

// The kinds an attribute value can be.
const (
	AttributeString AttributeKind = "string"
	AttributeNumber AttributeKind = "number"
	AttributeBool   AttributeKind = "bool"
)

// Any returns the value as JSON would decode it: a string, a float64 or a
// bool.
func (v AttributeValue) Any() any {
	switch v.Kind {
	case AttributeNumber:
		return v.Number
	case AttributeBool:
		return v.Bool
	case AttributeString:
		return v.String
	}

	return v.String
}

// AttributeValueOf wraps a plain value; it accepts what JSON decodes and
// the integer types Go callers use.
func AttributeValueOf(value any) (AttributeValue, error) {
	switch v := value.(type) {
	case string:
		return AttributeValue{Kind: AttributeString, String: v}, nil
	case float64:
		return AttributeValue{Kind: AttributeNumber, Number: v}, nil
	case int:
		return AttributeValue{Kind: AttributeNumber, Number: float64(v)}, nil
	case int64:
		return AttributeValue{Kind: AttributeNumber, Number: float64(v)}, nil
	case bool:
		return AttributeValue{Kind: AttributeBool, Bool: v}, nil
	default:
		return AttributeValue{}, fmt.Errorf("%w: got %T", ErrAttributeValueInvalid, value)
	}
}

// Expired reports whether the attribute is past its expiry.
func (a *NodeAttribute) Expired(now time.Time) bool {
	return !a.ExpiresAt.IsZero() && !a.ExpiresAt.After(now)
}

// Custom attribute rules, Tailscale's: the key is "custom:" and up to 50
// characters of letters, digits, underscores and dashes; the value is a
// string of up to 50 characters, a number or a bool.
const (
	CustomAttributePrefix   = "custom:"
	maxAttributeKeyLength   = 50
	maxAttributeValueLength = 50
)

var (
	ErrAttributeKeyInvalid = errors.New(
		"attribute key must be custom: followed by up to 50 letters, digits, underscores or dashes",
	)
	ErrAttributeValueInvalid = errors.New(
		"attribute value must be a string of up to 50 characters, a number or a boolean",
	)
	ErrAttributeExpiryPast = errors.New("attribute expiry must be in the future")
	ErrAttributeNotFound   = errors.New("attribute not found")
)

var attributeKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidateNodeAttribute checks a custom attribute before it is stored.
func ValidateNodeAttribute(a NodeAttribute, now time.Time) error {
	name, ok := strings.CutPrefix(a.Key, CustomAttributePrefix)
	if !ok || name == "" || len(name) > maxAttributeKeyLength || !attributeKeyRe.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrAttributeKeyInvalid, a.Key)
	}

	switch a.Value.Kind {
	case AttributeString:
		if len(a.Value.String) > maxAttributeValueLength {
			return fmt.Errorf("%w: %q is too long", ErrAttributeValueInvalid, a.Value.String)
		}
	case AttributeNumber, AttributeBool:
	default:
		return fmt.Errorf("%w: got kind %q", ErrAttributeValueInvalid, a.Value.Kind)
	}

	if !a.ExpiresAt.IsZero() && !a.ExpiresAt.After(now) {
		return ErrAttributeExpiryPast
	}

	return nil
}

// AttributeValueFromJSON decodes an attribute value, refusing objects,
// arrays and null.
func AttributeValueFromJSON(raw json.RawMessage) (AttributeValue, error) {
	var v any

	err := json.Unmarshal(raw, &v)
	if err != nil {
		return AttributeValue{}, fmt.Errorf("%w: %w", ErrAttributeValueInvalid, err)
	}

	switch v.(type) {
	case string, float64, bool:
		return AttributeValueOf(v)
	default:
		return AttributeValue{}, fmt.Errorf("%w: got %s", ErrAttributeValueInvalid, strings.TrimSpace(string(raw)))
	}
}

// Attribute names the server derives from what the client reports,
// named as Tailscale names its posture attributes so its policy examples
// apply; the node:hostname and following are additions.
const (
	AttrOS             = "node:os"
	AttrOSVersion      = "node:osVersion"
	AttrTSVersion      = "node:tsVersion"
	AttrTSReleaseTrack = "node:tsReleaseTrack"
	AttrTSAutoUpdate   = "node:tsAutoUpdate"
	AttrSerialNumber   = "node:serialNumber"
	AttrHostname       = "node:hostname"
	AttrMachine        = "node:machine"
	AttrDistro         = "node:distro"
	AttrDistroVersion  = "node:distroVersion"
	AttrDeviceModel    = "node:deviceModel"
	AttrPackage        = "node:package"
	AttrTagged         = "node:tagged"
)

// ReleaseTrack values of [AttrTSReleaseTrack].
const (
	ReleaseTrackStable   = "stable"
	ReleaseTrackUnstable = "unstable"
)

// PostureAttributes is the attribute map of one node at one moment:
// what the client reports, what the server collected and what an
// operator set, keyed by attribute name. A serial number list is a
// []string; everything else is a string, a float64 or a bool.
type PostureAttributes map[string]any

// postureAttributes builds the map from the node's report and its custom
// attributes, dropping expired ones.
func (node *Node) postureAttributes(now time.Time) PostureAttributes {
	attrs := PostureAttributes{
		AttrTagged: node.IsTagged(),
	}

	// Only what the client actually reported: an attribute that is absent
	// fails every check but NOT SET, while an empty string is present and
	// would let "node:os NOT IN [...]" match a node that reported nothing.
	if hi := node.Hostinfo; hi != nil {
		setReported(attrs, AttrOS, normalizeOS(hi.OS))
		setReported(attrs, AttrOSVersion, hi.OSVersion)
		setReported(attrs, AttrHostname, hi.Hostname)
		setReported(attrs, AttrMachine, hi.Machine)
		setReported(attrs, AttrDistro, hi.Distro)
		setReported(attrs, AttrDistroVersion, hi.DistroVersion)
		setReported(attrs, AttrDeviceModel, hi.DeviceModel)
		setReported(attrs, AttrPackage, hi.Package)

		if v := clientVersion(hi.IPNVersion); v != "" {
			attrs[AttrTSVersion] = v
			attrs[AttrTSReleaseTrack] = releaseTrack(v)
		}

		// AllowsUpdate is a bool with no unset value, so it only counts
		// once the client has reported a version and is a real report.
		if hi.IPNVersion != "" {
			attrs[AttrTSAutoUpdate] = hi.AllowsUpdate
		}
	}

	if node.Posture != nil && len(node.Posture.SerialNumbers) > 0 {
		attrs[AttrSerialNumber] = slices.Clone(node.Posture.SerialNumbers)
	}

	for _, a := range node.Attributes {
		if !a.Expired(now) {
			attrs[a.Key] = a.Value.Any()
		}
	}

	return attrs
}

// setReported stores a reported string attribute, leaving it out when the
// client reported nothing for it.
func setReported(attrs PostureAttributes, key, value string) {
	if value != "" {
		attrs[key] = value
	}
}

// postureInputsEqual reports whether two nodes feed [Node.postureAttributes]
// the same values, comparing the inputs in place rather than building
// the two maps. It runs for every node on every NodeStore write, in
// [NodeView.HasPolicyChange]. Expiry is left out, as the map compared
// there is built at the zero time; a custom attribute that shadows a
// reported one counts as a change here, which only costs a recompile.
func (node *Node) postureInputsEqual(other *Node) bool {
	if node.IsTagged() != other.IsTagged() {
		return false
	}

	if !hostinfoPostureEqual(node.Hostinfo, other.Hostinfo) {
		return false
	}

	if !slices.Equal(node.serialNumbers(), other.serialNumbers()) {
		return false
	}

	return slices.EqualFunc(node.Attributes, other.Attributes, func(a, b NodeAttribute) bool {
		return a.Key == b.Key && a.Value == b.Value
	})
}

func (node *Node) serialNumbers() []string {
	if node.Posture == nil {
		return nil
	}

	return node.Posture.SerialNumbers
}

// hostinfoPostureEqual compares the Hostinfo fields the attribute map
// carries.
func hostinfoPostureEqual(a, b *tailcfg.Hostinfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	return a.OS == b.OS &&
		a.OSVersion == b.OSVersion &&
		a.IPNVersion == b.IPNVersion &&
		a.AllowsUpdate == b.AllowsUpdate &&
		a.Hostname == b.Hostname &&
		a.Machine == b.Machine &&
		a.Distro == b.Distro &&
		a.DistroVersion == b.DistroVersion &&
		a.DeviceModel == b.DeviceModel &&
		a.Package == b.Package
}

// PostureAttributes returns the node's attribute map now.
func (nv NodeView) PostureAttributes(now time.Time) PostureAttributes {
	if !nv.Valid() {
		return nil
	}

	return nv.ж.postureAttributes(now)
}

// normalizeOS maps the client's OS name to Tailscale's lowercase
// attribute values: linux, windows, macos, ios, android, tvos, freebsd.
func normalizeOS(os string) string {
	return strings.ToLower(os)
}

// clientVersion strips the build suffix from IPNVersion ("1.86.2-t1a2b"
// becomes "1.86.2").
func clientVersion(v string) string {
	before, _, _ := strings.Cut(v, "-")

	return before
}

// releaseTrack tells a stable client (even minor version) from an
// unstable one, as Tailscale numbers them.
func releaseTrack(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return ""
	}

	var minor int

	_, err := fmt.Sscanf(parts[1], "%d", &minor)
	if err != nil {
		return ""
	}

	if minor%2 == 0 {
		return ReleaseTrackStable
	}

	return ReleaseTrackUnstable
}

// PostureFromResponse turns the client's c2n answer into a record.
func PostureFromResponse(resp tailcfg.C2NPostureIdentityResponse, now time.Time) PostureIdentity {
	serials := slices.Clone(resp.SerialNumbers)
	slices.Sort(serials)

	if serials == nil {
		serials = []string{}
	}

	return PostureIdentity{
		SerialNumbers: serials,
		Disabled:      resp.PostureDisabled,
		CollectedAt:   now,
	}
}

// SortAttributes orders attributes by key, the order every read returns.
func SortAttributes(attrs []NodeAttribute) {
	slices.SortFunc(attrs, func(a, b NodeAttribute) int { return strings.Compare(a.Key, b.Key) })
}

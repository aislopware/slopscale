package types

import (
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/juanfont/headscale/hscontrol/posture"
)

// PostureID identifies a posture in the postures table.
type PostureID uint64

// String renders the ID in base 10.
func (id PostureID) String() string {
	return strconv.FormatUint(uint64(id), 10)
}

// Posture is a reusable set of conditions a source node must satisfy for
// an access rule that names it: every expression must hold and, when
// there is a schedule, the time must fall in its window. Postures are the
// database counterpart of the policy file's postures section and use the
// same expression language.
type Posture struct {
	ID          PostureID
	Name        string
	Description string
	// Expressions are posture expressions such as "node:os == 'macos'".
	Expressions []string
	// Schedule, when set, limits the posture to a weekly window.
	Schedule  *posture.Schedule
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Errors returned by posture validation and lookup.
var (
	ErrPostureNameEmpty   = errors.New("posture name must not be empty")
	ErrPostureNameTooLong = errors.New("posture name must be at most 64 characters")
	ErrPostureNameInvalid = errors.New(
		"posture name may only contain letters, digits, spaces, dots, dashes and underscores",
	)
	ErrPostureNameTaken    = errors.New("posture name already exists")
	ErrPostureEmpty        = errors.New("posture needs at least one expression or a schedule")
	ErrPostureTooMany      = errors.New("posture may have at most 32 expressions")
	ErrPostureNotFound     = errors.New("posture not found")
	ErrPostureInUse        = errors.New("posture is used by an access rule")
	ErrPostureCountryNoGeo = errors.New("ip:country needs posture.geoip_database in the server configuration")
)

const maxPostureExpressions = 32

var postureNameRe = regexp.MustCompile(`^[\pL\pN][\pL\pN ._-]*$`)

// ValidatePosture checks the fields the operator controls and parses
// every expression.
func ValidatePosture(p Posture) error {
	switch {
	case strings.TrimSpace(p.Name) == "":
		return ErrPostureNameEmpty
	case len([]rune(p.Name)) > maxAccessNameLength:
		return ErrPostureNameTooLong
	case !postureNameRe.MatchString(p.Name):
		return ErrPostureNameInvalid
	case len(p.Expressions) == 0 && p.Schedule == nil:
		return ErrPostureEmpty
	case len(p.Expressions) > maxPostureExpressions:
		return ErrPostureTooMany
	}

	_, err := posture.ParseAll(p.Expressions)
	if err != nil {
		return err
	}

	if p.Schedule != nil {
		err = p.Schedule.Validate()
		if err != nil {
			return err
		}
	}

	return nil
}

// Parsed returns the parsed expressions; an invalid posture, which the
// store never holds, yields none.
func (p Posture) Parsed() []posture.Expr {
	exprs, err := posture.ParseAll(p.Expressions)
	if err != nil {
		return nil
	}

	return exprs
}

// Posture returns the posture with the ID.
func (m AccessModel) Posture(id PostureID) (Posture, bool) {
	for _, p := range m.Postures {
		if p.ID == id {
			return p, true
		}
	}

	return Posture{}, false
}

// RulesUsingPosture lists the rules that require the posture.
func (m AccessModel) RulesUsingPosture(id PostureID) []AccessRule {
	var rules []AccessRule

	for _, r := range m.Rules {
		if slices.Contains(r.PostureIDs, id) {
			rules = append(rules, r)
		}
	}

	return rules
}

// PostureAttributeIPAddress and PostureAttributeIPCountry are the
// attributes a posture reads from where the node connects from: the
// address of its control connection and, with a GeoIP database, the
// country that address is in.
const (
	PostureAttributeIPAddress = "ip:address"
	PostureAttributeIPCountry = "ip:country"
)

package db

import (
	"encoding"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"net/netip"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	jet "github.com/go-jet/jet/v2/sqlite"
	"tailscale.com/tailcfg"
)

// Row types mirror the tables in schema.sql column for column. Field names
// follow jet's naming (the camel case of the column name), which is what
// both the result mapper and INSERT/UPDATE ... MODEL match on. Columns that
// hold serialised values are kept as text here and converted to the domain
// types by the accompanying methods, so the domain types stay free of
// persistence concerns.
//
// [types.User], [types.APIKey] and [types.Policy] already have the shape of
// their tables and are used as row types directly.

// nodeRow is a row of the nodes table.
type nodeRow struct {
	ID             uint64 `sql:"primary_key"`
	MachineKey     string
	NodeKey        string
	DiscoKey       string
	Endpoints      string
	HostInfo       string
	Ipv4           *string
	Ipv6           *string
	Hostname       string
	GivenName      string
	UserID         *uint
	RegisterMethod string
	Tags           string
	AuthKeyID      *uint64
	LastSeen       *time.Time
	Expiry         *time.Time
	ApprovedRoutes string
	ApprovedAt     *time.Time
	SuspendedAt    *time.Time
	Posture        *string
	GlobalExitNode bool
	Ephemeral      bool
	// VipServices is the JSON of [types.NodeServices]; ApprovedServices
	// the JSON list of names. See schema.sql.
	VipServices      *string
	ApprovedServices *string
	// KeySignature is the tailnet lock node key signature as base64;
	// NlKey the node's lock key in tlpub: form. See schema.sql.
	KeySignature *string
	NlKey        *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    *time.Time
}

// nodeRecord is the destination of node queries: the node with its owning
// user and its pre-auth key (and that key's user) joined in.
type nodeRecord struct {
	Node        nodeRow        `alias:"nodes"`
	User        *types.User    `alias:"users"`
	AuthKey     *preAuthKeyRow `alias:"auth_key"`
	AuthKeyUser *types.User    `alias:"auth_key_user"`
}

func (r *nodeRecord) node() (*types.Node, error) {
	node, err := r.Node.node()
	if err != nil {
		return nil, err
	}

	node.User = r.User

	if r.AuthKey != nil {
		key, err := r.AuthKey.preAuthKey()
		if err != nil {
			return nil, err
		}

		key.User = r.AuthKeyUser
		node.AuthKey = key
	}

	return node, nil
}

func nodeRecordsToNodes(records []nodeRecord) (types.Nodes, error) {
	nodes := make(types.Nodes, 0, len(records))

	for i := range records {
		node, err := records[i].node()
		if err != nil {
			return nil, err
		}

		nodes = append(nodes, node)
	}

	return nodes, nil
}

func (r *nodeRow) node() (*types.Node, error) {
	node := &types.Node{
		ID:             types.NodeID(r.ID),
		Hostname:       r.Hostname,
		GivenName:      r.GivenName,
		UserID:         r.UserID,
		RegisterMethod: r.RegisterMethod,
		AuthKeyID:      r.AuthKeyID,
		Expiry:         r.Expiry,
		LastSeen:       r.LastSeen,
		ApprovedAt:     r.ApprovedAt,
		SuspendedAt:    r.SuspendedAt,
		GlobalExitNode: r.GlobalExitNode,
		Ephemeral:      r.Ephemeral,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
		DeletedAt:      r.DeletedAt,
	}

	err := unmarshalTextColumn(r.MachineKey, &node.MachineKey)
	if err != nil {
		return nil, fmt.Errorf("node %d machine_key: %w", r.ID, err)
	}

	err = unmarshalTextColumn(r.NodeKey, &node.NodeKey)
	if err != nil {
		return nil, fmt.Errorf("node %d node_key: %w", r.ID, err)
	}

	err = unmarshalTextColumn(r.DiscoKey, &node.DiscoKey)
	if err != nil {
		return nil, fmt.Errorf("node %d disco_key: %w", r.ID, err)
	}

	node.IPv4, err = parseAddrColumn(r.Ipv4)
	if err != nil {
		return nil, fmt.Errorf("node %d ipv4: %w", r.ID, err)
	}

	node.IPv6, err = parseAddrColumn(r.Ipv6)
	if err != nil {
		return nil, fmt.Errorf("node %d ipv6: %w", r.ID, err)
	}

	err = unmarshalJSONColumn(r.Endpoints, &node.Endpoints)
	if err != nil {
		return nil, fmt.Errorf("node %d endpoints: %w", r.ID, err)
	}

	if hasJSONValue(r.HostInfo) {
		node.Hostinfo = new(tailcfg.Hostinfo)

		err = unmarshalJSONColumn(r.HostInfo, node.Hostinfo)
		if err != nil {
			return nil, fmt.Errorf("node %d host_info: %w", r.ID, err)
		}
	}

	if r.Posture != nil && hasJSONValue(*r.Posture) {
		node.Posture = new(types.PostureIdentity)

		err = unmarshalJSONColumn(*r.Posture, node.Posture)
		if err != nil {
			return nil, fmt.Errorf("node %d posture: %w", r.ID, err)
		}
	}

	err = unmarshalJSONColumn(r.Tags, &node.Tags)
	if err != nil {
		return nil, fmt.Errorf("node %d tags: %w", r.ID, err)
	}

	err = unmarshalJSONColumn(r.ApprovedRoutes, &node.ApprovedRoutes)
	if err != nil {
		return nil, fmt.Errorf("node %d approved_routes: %w", r.ID, err)
	}

	if r.VipServices != nil && hasJSONValue(*r.VipServices) {
		node.Services = new(types.NodeServices)

		err = unmarshalJSONColumn(*r.VipServices, node.Services)
		if err != nil {
			return nil, fmt.Errorf("node %d vip_services: %w", r.ID, err)
		}
	}

	if r.ApprovedServices != nil && hasJSONValue(*r.ApprovedServices) {
		err = unmarshalJSONColumn(*r.ApprovedServices, &node.ApprovedServices)
		if err != nil {
			return nil, fmt.Errorf("node %d approved_services: %w", r.ID, err)
		}
	}

	err = r.lockKeys(node)
	if err != nil {
		return nil, err
	}

	return node, nil
}

// lockKeys decodes the tailnet lock columns into node.
func (r *nodeRow) lockKeys(node *types.Node) error {
	if r.KeySignature != nil && *r.KeySignature != "" {
		sig, err := base64.StdEncoding.DecodeString(*r.KeySignature)
		if err != nil {
			return fmt.Errorf("node %d key_signature: %w", r.ID, err)
		}

		node.KeySignature = sig
	}

	if r.NlKey != nil && *r.NlKey != "" {
		err := node.NLKey.UnmarshalText([]byte(*r.NlKey))
		if err != nil {
			return fmt.Errorf("node %d nl_key: %w", r.ID, err)
		}
	}

	return nil
}

// nodeRowFrom serialises node for INSERT/UPDATE ... MODEL.
func nodeRowFrom(node *types.Node) (nodeRow, error) {
	row := nodeRow{
		ID:             node.ID.Uint64(),
		MachineKey:     node.MachineKey.String(),
		NodeKey:        node.NodeKey.String(),
		DiscoKey:       node.DiscoKey.String(),
		Ipv4:           addrColumn(node.IPv4),
		Ipv6:           addrColumn(node.IPv6),
		Hostname:       node.Hostname,
		GivenName:      node.GivenName,
		UserID:         node.UserID,
		RegisterMethod: node.RegisterMethod,
		AuthKeyID:      node.AuthKeyID,
		LastSeen:       node.LastSeen,
		Expiry:         node.Expiry,
		ApprovedAt:     node.ApprovedAt,
		SuspendedAt:    node.SuspendedAt,
		GlobalExitNode: node.GlobalExitNode,
		Ephemeral:      node.Ephemeral,
		CreatedAt:      node.CreatedAt,
		UpdatedAt:      node.UpdatedAt,
		DeletedAt:      node.DeletedAt,
	}

	var err error

	row.Endpoints, err = marshalJSONColumn(node.Endpoints)
	if err != nil {
		return nodeRow{}, fmt.Errorf("endpoints: %w", err)
	}

	if node.Posture != nil {
		var posture string

		posture, err = marshalJSONColumn(node.Posture)
		if err != nil {
			return nodeRow{}, fmt.Errorf("posture: %w", err)
		}

		row.Posture = &posture
	}

	row.HostInfo, err = marshalJSONColumn(node.Hostinfo)
	if err != nil {
		return nodeRow{}, fmt.Errorf("host_info: %w", err)
	}

	row.Tags, err = marshalJSONColumn(node.Tags)
	if err != nil {
		return nodeRow{}, fmt.Errorf("tags: %w", err)
	}

	row.ApprovedRoutes, err = marshalJSONColumn(node.ApprovedRoutes)
	if err != nil {
		return nodeRow{}, fmt.Errorf("approved_routes: %w", err)
	}

	if node.Services != nil {
		var services string

		services, err = marshalJSONColumn(node.Services)
		if err != nil {
			return nodeRow{}, fmt.Errorf("vip_services: %w", err)
		}

		row.VipServices = &services
	}

	if len(node.ApprovedServices) > 0 {
		var approved string

		approved, err = marshalJSONColumn(node.ApprovedServices)
		if err != nil {
			return nodeRow{}, fmt.Errorf("approved_services: %w", err)
		}

		row.ApprovedServices = &approved
	}

	if len(node.KeySignature) > 0 {
		row.KeySignature = new(base64.StdEncoding.EncodeToString(node.KeySignature))
	}

	if !node.NLKey.IsZero() {
		var nlKey []byte

		nlKey, err = node.NLKey.MarshalText()
		if err != nil {
			return nodeRow{}, fmt.Errorf("nl_key: %w", err)
		}

		row.NlKey = new(string(nlKey))
	}

	return row, nil
}

// preAuthKeyRow is a row of the pre_auth_keys table.
type preAuthKeyRow struct {
	ID            uint64 `sql:"primary_key"`
	Key           string
	Prefix        string
	Hash          []byte
	UserID        *uint
	Description   string
	Reusable      bool
	Ephemeral     bool
	Used          bool
	Tags          string
	Preauthorized bool
	Groups        string
	Expiration    *time.Time
	Revoked       *time.Time
	CreatedAt     *time.Time
}

// preAuthKeyRecord is the destination of pre-auth key queries: the key with
// its user joined in.
type preAuthKeyRecord struct {
	Key  preAuthKeyRow `alias:"pre_auth_keys"`
	User *types.User   `alias:"users"`
}

func (r *preAuthKeyRecord) preAuthKey() (*types.PreAuthKey, error) {
	key, err := r.Key.preAuthKey()
	if err != nil {
		return nil, err
	}

	key.User = r.User

	return key, nil
}

func preAuthKeyRecordsToKeys(records []preAuthKeyRecord) ([]types.PreAuthKey, error) {
	keys := make([]types.PreAuthKey, 0, len(records))

	for i := range records {
		key, err := records[i].preAuthKey()
		if err != nil {
			return nil, err
		}

		keys = append(keys, *key)
	}

	return keys, nil
}

func (r *preAuthKeyRow) preAuthKey() (*types.PreAuthKey, error) {
	key := &types.PreAuthKey{
		ID:            r.ID,
		Key:           r.Key,
		Prefix:        r.Prefix,
		Hash:          r.Hash,
		UserID:        r.UserID,
		Description:   r.Description,
		Reusable:      r.Reusable,
		Ephemeral:     r.Ephemeral,
		Used:          r.Used,
		Preauthorized: r.Preauthorized,
		Expiration:    r.Expiration,
		Revoked:       r.Revoked,
		CreatedAt:     r.CreatedAt,
	}

	err := unmarshalJSONColumn(r.Tags, &key.Tags)
	if err != nil {
		return nil, fmt.Errorf("pre-auth key %d tags: %w", r.ID, err)
	}

	err = unmarshalJSONColumn(r.Groups, &key.Groups)
	if err != nil {
		return nil, fmt.Errorf("pre-auth key %d groups: %w", r.ID, err)
	}

	return key, nil
}

func preAuthKeyRowFrom(key *types.PreAuthKey) (preAuthKeyRow, error) {
	tags, err := marshalJSONColumn(key.Tags)
	if err != nil {
		return preAuthKeyRow{}, fmt.Errorf("tags: %w", err)
	}

	groups, err := marshalJSONColumn(key.Groups)
	if err != nil {
		return preAuthKeyRow{}, fmt.Errorf("groups: %w", err)
	}

	return preAuthKeyRow{
		ID:            key.ID,
		Key:           key.Key,
		Prefix:        key.Prefix,
		Hash:          key.Hash,
		UserID:        key.UserID,
		Description:   key.Description,
		Reusable:      key.Reusable,
		Ephemeral:     key.Ephemeral,
		Used:          key.Used,
		Tags:          tags,
		Preauthorized: key.Preauthorized,
		Groups:        groups,
		Expiration:    key.Expiration,
		Revoked:       key.Revoked,
		CreatedAt:     key.CreatedAt,
	}, nil
}

// oauthClientRow is a row of the oauth_clients table.
type oauthClientRow struct {
	ID          uint64 `sql:"primary_key"`
	ClientID    string
	SecretHash  []byte
	Scopes      string
	Tags        string
	Description string
	UserID      *uint
	CreatedAt   *time.Time
	Revoked     *time.Time
}

func (r *oauthClientRow) client() (*types.OAuthClient, error) {
	client := &types.OAuthClient{
		ID:          r.ID,
		ClientID:    r.ClientID,
		SecretHash:  r.SecretHash,
		Description: r.Description,
		UserID:      r.UserID,
		CreatedAt:   r.CreatedAt,
		Revoked:     r.Revoked,
	}

	err := unmarshalJSONColumn(r.Scopes, &client.Scopes)
	if err != nil {
		return nil, fmt.Errorf("oauth client %d scopes: %w", r.ID, err)
	}

	err = unmarshalJSONColumn(r.Tags, &client.Tags)
	if err != nil {
		return nil, fmt.Errorf("oauth client %d tags: %w", r.ID, err)
	}

	return client, nil
}

func oauthClientRowFrom(client *types.OAuthClient) (oauthClientRow, error) {
	scopes, err := marshalJSONColumn(client.Scopes)
	if err != nil {
		return oauthClientRow{}, fmt.Errorf("scopes: %w", err)
	}

	tags, err := marshalJSONColumn(client.Tags)
	if err != nil {
		return oauthClientRow{}, fmt.Errorf("tags: %w", err)
	}

	return oauthClientRow{
		ID:          client.ID,
		ClientID:    client.ClientID,
		SecretHash:  client.SecretHash,
		Scopes:      scopes,
		Tags:        tags,
		Description: client.Description,
		UserID:      client.UserID,
		CreatedAt:   client.CreatedAt,
		Revoked:     client.Revoked,
	}, nil
}

// oauthAccessTokenRow is a row of the oauth_access_tokens table.
type oauthAccessTokenRow struct {
	ID         uint64 `sql:"primary_key"`
	Prefix     string
	Hash       []byte
	ClientID   string
	Scopes     string
	Tags       string
	Expiration *time.Time
	CreatedAt  *time.Time
}

func (r *oauthAccessTokenRow) token() (*types.OAuthAccessToken, error) {
	token := &types.OAuthAccessToken{
		ID:         r.ID,
		Prefix:     r.Prefix,
		Hash:       r.Hash,
		ClientID:   r.ClientID,
		Expiration: r.Expiration,
		CreatedAt:  r.CreatedAt,
	}

	err := unmarshalJSONColumn(r.Scopes, &token.Scopes)
	if err != nil {
		return nil, fmt.Errorf("oauth access token %d scopes: %w", r.ID, err)
	}

	err = unmarshalJSONColumn(r.Tags, &token.Tags)
	if err != nil {
		return nil, fmt.Errorf("oauth access token %d tags: %w", r.ID, err)
	}

	return token, nil
}

func oauthAccessTokenRowFrom(token *types.OAuthAccessToken) (oauthAccessTokenRow, error) {
	scopes, err := marshalJSONColumn(token.Scopes)
	if err != nil {
		return oauthAccessTokenRow{}, fmt.Errorf("scopes: %w", err)
	}

	tags, err := marshalJSONColumn(token.Tags)
	if err != nil {
		return oauthAccessTokenRow{}, fmt.Errorf("tags: %w", err)
	}

	return oauthAccessTokenRow{
		ID:         token.ID,
		Prefix:     token.Prefix,
		Hash:       token.Hash,
		ClientID:   token.ClientID,
		Scopes:     scopes,
		Tags:       tags,
		Expiration: token.Expiration,
		CreatedAt:  token.CreatedAt,
	}, nil
}

// idRow receives the id of a row written with RETURNING.
type idRow struct {
	ID uint64
}

// hasJSONValue reports whether a JSON column holds a value. Empty and NULL
// columns and the literal null all mean "unset".
func hasJSONValue(column string) bool {
	return column != "" && column != "null"
}

// unmarshalJSONColumn decodes a JSON column into dest, leaving dest untouched
// when the column is unset.
func unmarshalJSONColumn(column string, dest any) error {
	if !hasJSONValue(column) {
		return nil
	}

	err := json.Unmarshal([]byte(column), dest)
	if err != nil {
		return fmt.Errorf("decoding json column: %w", err)
	}

	return nil
}

// marshalJSONColumn encodes v for a JSON column. A nil slice or map is
// stored as null, as encoding/json v1 stored it, so it reads back as nil
// rather than as an empty value.
func marshalJSONColumn(v any) (string, error) {
	data, err := json.Marshal(v, json.FormatNilSliceAsNull(true), json.FormatNilMapAsNull(true))
	if err != nil {
		return "", fmt.Errorf("encoding json column: %w", err)
	}

	return string(data), nil
}

// unmarshalTextColumn decodes a text column into dest, leaving dest as its
// zero value when the column is empty.
func unmarshalTextColumn(column string, dest encoding.TextUnmarshaler) error {
	if column == "" {
		return nil
	}

	err := dest.UnmarshalText([]byte(column))
	if err != nil {
		return fmt.Errorf("decoding text column: %w", err)
	}

	return nil
}

func parseAddrColumn(column *string) (*netip.Addr, error) {
	if column == nil || *column == "" {
		return nil, nil //nolint:nilnil // an unset column is an unset address
	}

	addr, err := netip.ParseAddr(*column)
	if err != nil {
		return nil, fmt.Errorf("parsing address column: %w", err)
	}

	return &addr, nil
}

func addrColumn(addr *netip.Addr) *string {
	if addr == nil {
		return nil
	}

	s := addr.String()

	return &s
}

// Single-table destinations. QRM matches a top-level destination on its Go
// type name, so tables whose domain type is not named like the table are
// read through a record with an alias tag.

type userRecord struct {
	User types.User `alias:"users"`
}

func userRecordsToUsers(records []userRecord) []types.User {
	users := make([]types.User, len(records))
	for i := range records {
		users[i] = records[i].User
	}

	return users
}

// apiKeyRow is a row of the api_keys table; scopes is a JSON array.
type apiKeyRow struct {
	ID          uint64 `sql:"primary_key"`
	Prefix      string
	Hash        []byte
	UserID      *uint
	Scopes      string
	Description string
	CreatedAt   *time.Time
	Expiration  *time.Time
	LastSeen    *time.Time
}

func (r *apiKeyRow) key() (*types.APIKey, error) {
	key := &types.APIKey{
		ID:          r.ID,
		Prefix:      r.Prefix,
		Hash:        r.Hash,
		UserID:      r.UserID,
		Description: r.Description,
		CreatedAt:   r.CreatedAt,
		Expiration:  r.Expiration,
		LastSeen:    r.LastSeen,
	}

	err := unmarshalJSONColumn(r.Scopes, &key.Scopes)
	if err != nil {
		return nil, fmt.Errorf("api key %d scopes: %w", r.ID, err)
	}

	return key, nil
}

func apiKeyRowFrom(key *types.APIKey) (apiKeyRow, error) {
	scopes, err := marshalJSONColumn(key.Scopes)
	if err != nil {
		return apiKeyRow{}, fmt.Errorf("scopes: %w", err)
	}

	return apiKeyRow{
		ID:          key.ID,
		Prefix:      key.Prefix,
		Hash:        key.Hash,
		UserID:      key.UserID,
		Scopes:      scopes,
		Description: key.Description,
		CreatedAt:   key.CreatedAt,
		Expiration:  key.Expiration,
		LastSeen:    key.LastSeen,
	}, nil
}

type apiKeyRecord struct {
	Key apiKeyRow `alias:"api_keys"`
}

type policyRecord struct {
	Policy types.Policy `alias:"policies"`
}

type oauthClientRecord struct {
	Client oauthClientRow `alias:"oauth_clients"`
}

type oauthAccessTokenRecord struct {
	Token oauthAccessTokenRow `alias:"oauth_access_tokens"`
}

// timeArg binds t as a query argument. jet has no timestamp literal for
// SQLite, so times are passed through as raw parameters; both drivers bind
// time.Time natively.
func timeArg(t time.Time) jet.Expression {
	return jet.Raw("#t", jet.RawArgs{"#t": t})
}

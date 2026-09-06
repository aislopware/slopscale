package db

import (
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	jet "github.com/go-jet/jet/v2/sqlite"
	"github.com/juanfont/headscale/gen/jet/table"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/util"
)

var (
	ErrUserExists        = errors.New("user already exists")
	ErrUserNotFound      = errors.New("user not found")
	ErrUserStillHasNodes = errors.New("user not empty: node(s) found")
	ErrUserNotUnique     = errors.New("expected exactly one user")
)

// selectUsers is the base query for users that have not been soft-deleted,
// ordered by id.
func selectUsers() jet.SelectStatement {
	return jet.SELECT(table.Users.AllColumns).
		FROM(table.Users).
		WHERE(table.Users.DeletedAt.IS_NULL()).
		ORDER_BY(table.Users.ID.ASC())
}

func queryUsers(q Querier, stmt jet.SelectStatement) ([]types.User, error) {
	var records []userRecord

	err := q.executor().query(stmt, &records)
	if err != nil {
		return nil, err
	}

	return userRecordsToUsers(records), nil
}

// queryUser returns the first user matched by where, or [ErrUserNotFound].
func queryUser(q Querier, where jet.BoolExpression) (*types.User, error) {
	var record userRecord

	err := q.executor().query(
		jet.SELECT(table.Users.AllColumns).FROM(table.Users).
			WHERE(where.AND(table.Users.DeletedAt.IS_NULL())).
			ORDER_BY(table.Users.ID.ASC()).LIMIT(1),
		&record,
	)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrUserNotFound
	}

	if err != nil {
		return nil, err
	}

	return &record.User, nil
}

func (hsdb *HSDatabase) CreateUser(user types.User) (*types.User, error) {
	return Write(hsdb, func(tx *Tx) (*types.User, error) {
		return CreateUser(tx, user)
	})
}

// CreateUser creates a new [types.User]. Returns error if could not be created
// or another user already exists.
func CreateUser(q Querier, user types.User) (*types.User, error) {
	err := util.ValidateUsername(user.Name)
	if err != nil {
		return nil, err
	}

	err = SaveUser(q, &user)
	if err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}

	return &user, nil
}

// SaveUser overwrites every column of user's row when a row with its ID
// exists and inserts it otherwise, keeping an explicit ID. The timestamps
// are stamped when unset.
func SaveUser(q Querier, user *types.User) error {
	if user.ID != 0 {
		affected, err := updateUser(q, user)
		if err != nil || affected > 0 {
			return err
		}
	}

	now := time.Now()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}

	if user.UpdatedAt.IsZero() {
		user.UpdatedAt = now
	}

	columns := table.Users.MutableColumns
	if user.ID != 0 {
		columns = table.Users.AllColumns
	}

	var inserted idRow

	err := q.executor().query(
		table.Users.INSERT(columns).MODEL(user).RETURNING(table.Users.ID.AS("id_row.id")),
		&inserted,
	)
	if err != nil {
		return err
	}

	user.ID = uint(inserted.ID)

	return nil
}

// UpdateUser writes every column of user's row by id and stamps UpdatedAt.
func UpdateUser(q Querier, user *types.User) error {
	_, err := updateUser(q, user)

	return err
}

func updateUser(q Querier, user *types.User) (int64, error) {
	user.UpdatedAt = time.Now()

	return q.executor().exec(
		table.Users.UPDATE(table.Users.MutableColumns).MODEL(user).
			WHERE(table.Users.ID.EQ(jet.Uint64(uint64(user.ID)))),
	)
}

func (hsdb *HSDatabase) DestroyUser(uid types.UserID) error {
	return hsdb.Write(func(tx *Tx) error {
		return DestroyUser(tx, uid)
	})
}

// DestroyUser destroys a [types.User]. Returns error if the [types.User] does
// not exist or if there are user-owned nodes associated with it.
// Tagged nodes have user_id = NULL so they do not block deletion.
func DestroyUser(q Querier, uid types.UserID) error {
	user, err := GetUserByID(q, uid)
	if err != nil {
		return err
	}

	nodes, err := ListNodesByUser(q, uid)
	if err != nil {
		return err
	}

	if len(nodes) > 0 {
		return ErrUserStillHasNodes
	}

	keys, err := ListPreAuthKeysByUser(q, uid)
	if err != nil {
		return err
	}

	for _, key := range keys {
		err = DestroyPreAuthKey(q, key.ID)
		if err != nil {
			return err
		}
	}

	_, err = q.executor().exec(table.Users.DELETE().WHERE(table.Users.ID.EQ(jet.Uint64(uint64(user.ID)))))

	return err
}

func (hsdb *HSDatabase) RenameUser(uid types.UserID, newName string) error {
	return hsdb.Write(func(tx *Tx) error {
		return RenameUser(tx, uid, newName)
	})
}

var ErrCannotChangeOIDCUser = errors.New("cannot edit OIDC user")

// RenameUser renames a [types.User]. Returns error if the [types.User] does
// not exist or if another [types.User] exists with the new name.
func RenameUser(q Querier, uid types.UserID, newName string) error {
	oldUser, err := GetUserByID(q, uid)
	if err != nil {
		return err
	}

	valErr := util.ValidateUsername(newName)
	if valErr != nil {
		return valErr
	}

	if oldUser.Provider == util.RegisterMethodOIDC {
		return ErrCannotChangeOIDCUser
	}

	oldUser.Name = newName

	return UpdateUser(q, oldUser)
}

func (hsdb *HSDatabase) GetUserByID(uid types.UserID) (*types.User, error) {
	return GetUserByID(hsdb, uid)
}

func GetUserByID(q Querier, uid types.UserID) (*types.User, error) {
	return queryUser(q, table.Users.ID.EQ(jet.Uint64(uint64(uid))))
}

func (hsdb *HSDatabase) GetUserByOIDCIdentifier(id string) (*types.User, error) {
	return Read(hsdb, func(rx *Tx) (*types.User, error) {
		return GetUserByOIDCIdentifier(rx, id)
	})
}

func GetUserByOIDCIdentifier(q Querier, id string) (*types.User, error) {
	return queryUser(q, table.Users.ProviderIdentifier.EQ(jet.String(id)))
}

func (hsdb *HSDatabase) ListUsers(filter *types.User) ([]types.User, error) {
	return ListUsers(hsdb, filter)
}

// ListUsers gets all the existing users, optionally filtered by a non-nil
// filter. Every set field of the filter must match.
func ListUsers(q Querier, filter *types.User) ([]types.User, error) {
	stmt := selectUsers()

	if filter != nil {
		if where := userFilter(filter); where != nil {
			stmt = stmt.WHERE(table.Users.DeletedAt.IS_NULL().AND(where))
		}
	}

	return queryUsers(q, stmt)
}

// userFilter turns the set fields of filter into a conjunction, or nil when
// no field is set.
func userFilter(filter *types.User) jet.BoolExpression {
	var conds []jet.BoolExpression

	if filter.ID != 0 {
		conds = append(conds, table.Users.ID.EQ(jet.Uint64(uint64(filter.ID))))
	}

	if filter.Name != "" {
		conds = append(conds, table.Users.Name.EQ(jet.String(filter.Name)))
	}

	if filter.DisplayName != "" {
		conds = append(conds, table.Users.DisplayName.EQ(jet.String(filter.DisplayName)))
	}

	if filter.Email != "" {
		conds = append(conds, table.Users.Email.EQ(jet.String(filter.Email)))
	}

	if filter.ProviderIdentifier.Valid {
		conds = append(conds, table.Users.ProviderIdentifier.EQ(jet.String(filter.ProviderIdentifier.String)))
	}

	if filter.Provider != "" {
		conds = append(conds, table.Users.Provider.EQ(jet.String(filter.Provider)))
	}

	if filter.ProfilePicURL != "" {
		conds = append(conds, table.Users.ProfilePicURL.EQ(jet.String(filter.ProfilePicURL)))
	}

	if len(conds) == 0 {
		return nil
	}

	return jet.AND(conds...)
}

// GetUserByName returns a user if the provided username is
// unique, and otherwise an error.
func (hsdb *HSDatabase) GetUserByName(name string) (*types.User, error) {
	users, err := hsdb.ListUsers(&types.User{Name: name})
	if err != nil {
		return nil, err
	}

	if len(users) == 0 {
		return nil, ErrUserNotFound
	}

	if len(users) != 1 {
		return nil, fmt.Errorf("%w, found %d", ErrUserNotUnique, len(users))
	}

	return &users[0], nil
}

// ListNodesByUser gets all the nodes in a given user.
func ListNodesByUser(q Querier, uid types.UserID) (types.Nodes, error) {
	return queryNodes(q, selectNodes().WHERE(table.Nodes.UserID.EQ(jet.Uint64(uint64(uid)))))
}

func (hsdb *HSDatabase) CreateUserForTest(name ...string) *types.User {
	if !testing.Testing() {
		panic("CreateUserForTest can only be called during tests")
	}

	userName := firstOr("testuser", name)

	user, err := hsdb.CreateUser(types.User{Name: userName})
	if err != nil {
		panic(fmt.Sprintf("failed to create test user: %v", err))
	}

	return user
}

func (hsdb *HSDatabase) CreateUsersForTest(count int, namePrefix ...string) []*types.User {
	if !testing.Testing() {
		panic("CreateUsersForTest can only be called during tests")
	}

	prefix := firstOr("testuser", namePrefix)

	users := make([]*types.User, count)
	for i := range count {
		name := prefix + "-" + strconv.Itoa(i)
		users[i] = hsdb.CreateUserForTest(name)
	}

	return users
}

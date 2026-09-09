package cli

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/spf13/cobra"
)

const cmdCreate = "create"

func init() {
	rootCmd.AddCommand(groupsCmd)

	groupsCmd.AddCommand(listGroupsCmd)

	createGroupCmd.Flags().StringP("name", "n", "", "Group name")
	mustMarkRequired(createGroupCmd, "name")
	createGroupCmd.Flags().StringP("description", "d", "", "Group description")
	createGroupCmd.Flags().Bool("requestable", false, "Let members request to join the group for a while")
	groupsCmd.AddCommand(createGroupCmd)

	renameGroupCmd.Flags().Uint64P("identifier", "i", 0, "Group identifier (ID)")
	mustMarkRequired(renameGroupCmd, "identifier")
	renameGroupCmd.Flags().StringP("name", "n", "", "New group name")
	mustMarkRequired(renameGroupCmd, "name")
	renameGroupCmd.Flags().StringP("description", "d", "", "Group description")
	renameGroupCmd.Flags().Bool("requestable", false, "Let members request to join the group for a while")
	groupsCmd.AddCommand(renameGroupCmd)

	deleteGroupCmd.Flags().Uint64P("identifier", "i", 0, "Group identifier (ID)")
	mustMarkRequired(deleteGroupCmd, "identifier")
	groupsCmd.AddCommand(deleteGroupCmd)

	addGroupNodeCmd.Flags().Uint64P("identifier", "i", 0, "Group identifier (ID)")
	mustMarkRequired(addGroupNodeCmd, "identifier")
	addGroupNodeCmd.Flags().String("node", "", "Machine identifier (ID)")
	addGroupNodeCmd.Flags().String("expires", "", "End the membership after a duration (4h) or at an RFC 3339 time")
	mustMarkRequired(addGroupNodeCmd, "node")
	groupsCmd.AddCommand(addGroupNodeCmd)

	removeGroupNodeCmd.Flags().Uint64P("identifier", "i", 0, "Group identifier (ID)")
	mustMarkRequired(removeGroupNodeCmd, "identifier")
	removeGroupNodeCmd.Flags().String("node", "", "Machine identifier (ID)")
	mustMarkRequired(removeGroupNodeCmd, "node")
	groupsCmd.AddCommand(removeGroupNodeCmd)

	addGroupUserCmd.Flags().Uint64P("identifier", "i", 0, "Group identifier (ID)")
	mustMarkRequired(addGroupUserCmd, "identifier")
	addGroupUserCmd.Flags().StringP("user", "u", "", "User identifier (ID)")
	addGroupUserCmd.Flags().String("expires", "", "End the membership after a duration (4h) or at an RFC 3339 time")
	mustMarkRequired(addGroupUserCmd, "user")
	groupsCmd.AddCommand(addGroupUserCmd)

	removeGroupUserCmd.Flags().Uint64P("identifier", "i", 0, "Group identifier (ID)")
	mustMarkRequired(removeGroupUserCmd, "identifier")
	removeGroupUserCmd.Flags().StringP("user", "u", "", "User identifier (ID)")
	mustMarkRequired(removeGroupUserCmd, "user")
	groupsCmd.AddCommand(removeGroupUserCmd)
}

var groupsCmd = &cobra.Command{
	Use:     "groups",
	Short:   "Manage groups of machines and users",
	Aliases: []string{"group"},
}

var listGroupsCmd = &cobra.Command{
	Use:     cmdList,
	Short:   "List groups",
	Aliases: []string{"ls"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.ListGroupsWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("listing groups: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			groups := resp.JSON200.Groups

			return printListOutput(cmd, groups, func() error {
				rows := make([][]string, 0, len(groups))
				for _, group := range groups {
					builtin := ""
					if group.Builtin != "" {
						builtin = "yes"
					}

					rows = append(
						rows,
						[]string{
							group.Id,
							group.Name,
							group.Description,
							strconv.Itoa(len(group.NodeIds)),
							strconv.Itoa(len(group.UserIds)),
							builtin,
						},
					)
				}

				return renderTable([]string{"ID", "Name", "Description", "Machines", "Users", "Builtin"}, rows)
			})
		},
	),
}

var createGroupCmd = &cobra.Command{
	Use:     cmdCreate,
	Short:   "Create a group",
	Aliases: []string{"c", cmdNew},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")

			requestable, _ := cmd.Flags().GetBool("requestable")

			req := clientv1.CreateGroupJSONRequestBody{
				Name:        name,
				Requestable: &requestable,
			}

			if cmd.Flags().Changed("description") {
				desc, _ := cmd.Flags().GetString("description")
				req.Description = &desc
			}

			resp, err := client.CreateGroupWithResponse(ctx, req)
			if err != nil {
				return fmt.Errorf("creating group: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.Group, "Group created")
		},
	),
}

var renameGroupCmd = &cobra.Command{
	Use:     "rename",
	Short:   "Rename a group",
	Aliases: []string{"mv"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			groupID := strconv.FormatUint(identifier, util.Base10)

			name, _ := cmd.Flags().GetString("name")

			requestable, _ := cmd.Flags().GetBool("requestable")

			req := clientv1.UpdateGroupJSONRequestBody{
				Name:        name,
				Requestable: &requestable,
			}

			if cmd.Flags().Changed("description") {
				desc, _ := cmd.Flags().GetString("description")
				req.Description = &desc
			}

			resp, err := client.UpdateGroupWithResponse(ctx, groupID, req)
			if err != nil {
				return fmt.Errorf("updating group: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.Group, "Group updated")
		},
	),
}

var deleteGroupCmd = &cobra.Command{
	Use:     cmdDelete,
	Short:   "Delete a group",
	Aliases: []string{aliasDel},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			groupID := strconv.FormatUint(identifier, util.Base10)

			name, err := groupNameForID(ctx, client, groupID)
			if err != nil {
				return err
			}

			if !confirmAction(cmd, fmt.Sprintf("Do you want to remove the group %s?", name)) {
				return printOutput(cmd, map[string]string{colResult: "Group not deleted"}, "Group not deleted")
			}

			deleteResponse, err := client.DeleteGroupWithResponse(ctx, groupID)
			if err != nil {
				return fmt.Errorf("deleting group: %w", err)
			}

			if deleteResponse.StatusCode() != http.StatusOK {
				return apiError(deleteResponse.StatusCode(), deleteResponse.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, map[string]string{colResult: "Group deleted"}, "Group deleted")
		},
	),
}

func groupNameForID(ctx context.Context, client *clientv1.ClientWithResponses, id string) (string, error) {
	resp, err := client.GetGroupWithResponse(ctx, id)
	if err != nil {
		return "", fmt.Errorf("getting group: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return "", apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
	}

	return resp.JSON200.Group.Name, nil
}

var addGroupNodeCmd = &cobra.Command{
	Use:   "add-node",
	Short: "Add a machine to a group",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			nodeID, err := groupNodeFlag(cmd)
			if err != nil {
				return err
			}

			return addGroupMember(ctx, client, cmd, clientv1.AddGroupMemberJSONRequestBody{NodeId: &nodeID},
				"Machine added to group")
		},
	),
}

// addGroupMember sends one membership with the --expires flag applied and
// prints the group.
func addGroupMember(
	ctx context.Context,
	client *clientv1.ClientWithResponses,
	cmd *cobra.Command,
	body clientv1.AddGroupMemberJSONRequestBody,
	done string,
) error {
	identifier, _ := cmd.Flags().GetUint64("identifier")
	groupID := strconv.FormatUint(identifier, util.Base10)

	expiresAt, err := ruleExpiryFlag(cmd)
	if err != nil {
		return err
	}

	body.ExpiresAt = expiresAt

	resp, err := client.AddGroupMemberWithResponse(ctx, groupID, body)
	if err != nil {
		return fmt.Errorf("adding group member: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
	}

	return printOutput(cmd, resp.JSON200.Group, done)
}

var removeGroupNodeCmd = &cobra.Command{
	Use:   "remove-node",
	Short: "Remove a machine from a group",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			groupID := strconv.FormatUint(identifier, util.Base10)

			nodeID, err := groupNodeFlag(cmd)
			if err != nil {
				return err
			}

			resp, err := client.RemoveGroupNodeWithResponse(ctx, groupID, nodeID)
			if err != nil {
				return fmt.Errorf("removing machine from group: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.Group, "Machine removed from group")
		},
	),
}

var addGroupUserCmd = &cobra.Command{
	Use:   "add-user",
	Short: "Add a user to a group",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			userID, err := groupUserFlag(cmd)
			if err != nil {
				return err
			}

			return addGroupMember(ctx, client, cmd, clientv1.AddGroupMemberJSONRequestBody{UserId: &userID},
				"User added to group")
		},
	),
}

var removeGroupUserCmd = &cobra.Command{
	Use:   "remove-user",
	Short: "Remove a user from a group",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			groupID := strconv.FormatUint(identifier, util.Base10)

			userID, err := groupUserFlag(cmd)
			if err != nil {
				return err
			}

			resp, err := client.RemoveGroupUserWithResponse(ctx, groupID, userID)
			if err != nil {
				return fmt.Errorf("removing user from group: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.Group, "User removed from group")
		},
	),
}

// groupNodeFlag reads --node as a machine id.
func groupNodeFlag(cmd *cobra.Command) (string, error) {
	node, _ := cmd.Flags().GetString("node")

	_, err := strconv.ParseUint(node, util.Base10, 64)
	if err != nil {
		return "", fmt.Errorf("--node must be a node id: %w", err)
	}

	return node, nil
}

// groupUserFlag reads --user as a user id.
func groupUserFlag(cmd *cobra.Command) (string, error) {
	user, _ := cmd.Flags().GetString("user")

	_, err := strconv.ParseUint(user, util.Base10, 64)
	if err != nil {
		return "", fmt.Errorf("--user must be a user id: %w", err)
	}

	return user, nil
}

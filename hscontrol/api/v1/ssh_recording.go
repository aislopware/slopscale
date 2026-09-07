package apiv1

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/audit"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/rs/zerolog/log"
)

func init() {
	registrations = append(registrations, registerSSHRecordings, registerSSHRecordingFiles)
}

const (
	tagSSHRecordings         = "SSH recordings"
	sshRecordingsDefaultPage = 50
	castContentType          = "application/x-asciicast"
)

// SSHRecording is one recorded SSH session; the terminal stream itself
// is downloaded separately as an asciinema file.
type SSHRecording struct {
	ID        string     `format:"uint64"  json:"id"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt"`
	// SrcNode is the connecting node's name as the client reported it,
	// SrcNodeID its stable ID, SrcUser its user's login name (empty for
	// a tagged node).
	SrcNode   string `json:"srcNode"`
	SrcNodeID string `json:"srcNodeId"`
	SrcUser   string `json:"srcUser"`
	// DstNodeID and DstNode name the node the session ran on; empty
	// when the upload came from an address no node holds.
	DstNodeID string `format:"uint64" json:"dstNodeId"`
	DstNode   string `json:"dstNode"`
	// SSHUser is the name the client asked for, LocalUser the account
	// the session got.
	SSHUser   string `json:"sshUser"`
	LocalUser string `json:"localUser"`
	// Command is what ran instead of a shell; empty for a shell.
	Command string `json:"command"`
	// Size is the file's size in bytes.
	Size int64 `json:"size"`
	// Complete reports whether the upload ended cleanly.
	Complete bool `json:"complete"`
}

type (
	sshRecordingIDInput struct {
		ID string `format:"uint64" path:"id"`
	}
	listSSHRecordingsInput struct {
		Before string `doc:"Page: recordings with an ID below this one." format:"uint64" query:"before"`
		Limit  int    `doc:"Page size, at most 500."                     maximum:"500"   minimum:"1"    query:"limit"`
	}
	sshRecordingOutput struct {
		Body struct {
			Recording SSHRecording `json:"recording"`
		}
	}
	listSSHRecordingsOutput struct {
		Body struct {
			Recordings []SSHRecording `json:"recordings" nullable:"false"`
			// NextBefore is the cursor for the next page, empty on the last one.
			NextBefore string `json:"nextBefore"`
		}
	}
)

func parseSSHRecordingID(s string) (types.SSHRecordingID, error) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, huma.Error400BadRequest("invalid recording id", err)
	}

	return types.SSHRecordingID(id), nil
}

func sshRecordingFrom(r types.SSHRecording) SSHRecording {
	out := SSHRecording{
		ID:        formatID(uint64(r.ID)),
		StartedAt: r.StartedAt,
		EndedAt:   r.EndedAt,
		SrcNode:   r.SrcNode,
		SrcNodeID: r.SrcNodeID,
		SrcUser:   r.SrcUser,
		DstNode:   r.DstNode,
		SSHUser:   r.SSHUser,
		LocalUser: r.LocalUser,
		Command:   r.Command,
		Size:      r.Size,
		Complete:  r.Complete,
	}

	if r.DstNodeID != 0 {
		out.DstNodeID = formatID(uint64(r.DstNodeID))
	}

	return out
}

func registerSSHRecordings(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "listSSHRecordings",
		Method:      http.MethodGet,
		Path:        "/api/v1/ssh-recording",
		Summary:     "List SSH session recordings",
		Description: "Newest first; page with before=<last id>. See docs/ref/ssh-recording.md.",
		Tags:        []string{tagSSHRecordings},
		Security:    bearerAuth,
	}, scope.LogsConfigurationRead), func(
		_ context.Context, in *listSSHRecordingsInput,
	) (*listSSHRecordingsOutput, error) {
		before, limit, err := sshRecordingPage(in)
		if err != nil {
			return nil, err
		}

		recordings, err := b.Recorder.List(before, limit)
		if err != nil {
			return nil, mapError("listing SSH recordings", err)
		}

		out := &listSSHRecordingsOutput{}
		out.Body.Recordings = make([]SSHRecording, 0, len(recordings))

		for _, r := range recordings {
			out.Body.Recordings = append(out.Body.Recordings, sshRecordingFrom(r))
		}

		if len(recordings) == limit {
			out.Body.NextBefore = out.Body.Recordings[len(recordings)-1].ID
		}

		return out, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "getSSHRecording",
		Method:      http.MethodGet,
		Path:        "/api/v1/ssh-recording/{id}",
		Summary:     "Get SSH session recording",
		Tags:        []string{tagSSHRecordings},
		Security:    bearerAuth,
	}, scope.LogsConfigurationRead), func(_ context.Context, in *sshRecordingIDInput) (*sshRecordingOutput, error) {
		id, err := parseSSHRecordingID(in.ID)
		if err != nil {
			return nil, err
		}

		r, err := b.Recorder.Get(id)
		if err != nil {
			return nil, mapError("getting SSH recording", err)
		}

		out := &sshRecordingOutput{}
		out.Body.Recording = sshRecordingFrom(r)

		return out, nil
	})
}

// sshRecordingPage reads the paging query.
func sshRecordingPage(in *listSSHRecordingsInput) (types.SSHRecordingID, int, error) {
	var before types.SSHRecordingID

	if in.Before != "" {
		id, err := parseSSHRecordingID(in.Before)
		if err != nil {
			return 0, 0, huma.Error400BadRequest("invalid before cursor", err)
		}

		before = id
	}

	limit := in.Limit
	if limit == 0 {
		limit = sshRecordingsDefaultPage
	}

	return before, limit, nil
}

// registerSSHRecordingFiles is the download and the delete, which touch
// the file as well as the index.
func registerSSHRecordingFiles(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "downloadSSHRecording",
		Method:      http.MethodGet,
		Path:        "/api/v1/ssh-recording/{id}/cast",
		Summary:     "Download SSH session recording",
		Description: "The session as an asciinema v2 file, playable with asciinema or asciinema-player.",
		Tags:        []string{tagSSHRecordings},
		Security:    bearerAuth,
		Responses: map[string]*huma.Response{
			"200": {
				Description: "The recording.",
				Content: map[string]*huma.MediaType{
					castContentType: {Schema: &huma.Schema{Type: "string", Format: "binary"}},
				},
			},
		},
	}, scope.LogsConfigurationRead), func(_ context.Context, in *sshRecordingIDInput) (*huma.StreamResponse, error) {
		id, err := parseSSHRecordingID(in.ID)
		if err != nil {
			return nil, err
		}

		r, err := b.Recorder.Get(id)
		if err != nil {
			return nil, mapError("getting SSH recording", err)
		}

		file, err := b.Recorder.Open(r)
		if err != nil {
			return nil, mapError("opening SSH recording", err)
		}

		return &huma.StreamResponse{
			Body: func(ctx huma.Context) {
				defer file.Close()

				ctx.SetHeader("Content-Type", castContentType)
				ctx.SetHeader("Content-Disposition",
					`attachment; filename="ssh-recording-`+formatID(uint64(r.ID))+`.cast"`)
				ctx.SetStatus(http.StatusOK)

				_, err := io.Copy(ctx.BodyWriter(), file)
				if err != nil {
					log.Debug().Err(err).Uint64("recording", uint64(r.ID)).Msg("sending SSH recording")
				}
			},
		}, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "deleteSSHRecording",
		Method:      http.MethodDelete,
		Path:        "/api/v1/ssh-recording/{id}",
		Summary:     "Delete SSH session recording",
		Description: "Removes the index entry and the file.",
		Tags:        []string{tagSSHRecordings},
		Security:    bearerAuth,
	}, scope.LogsConfiguration), "sshrecording.delete", "sshrecording", "id"), func(
		ctx context.Context, in *sshRecordingIDInput,
	) (*emptyOutput, error) {
		id, err := parseSSHRecordingID(in.ID)
		if err != nil {
			return nil, err
		}

		r, err := b.Recorder.Get(id)
		if err != nil {
			return nil, mapError("deleting SSH recording", err)
		}

		err = b.Recorder.Delete(id)
		if err != nil {
			return nil, mapError("deleting SSH recording", err)
		}

		audit.Target(ctx, "", "", r.SrcNode+" -> "+r.SSHUser+"@"+r.DstNode)
		audit.Detail(ctx, "srcNode", r.SrcNode)
		audit.Detail(ctx, "dstNode", r.DstNode)
		audit.Detail(ctx, "sshUser", r.SSHUser)

		return &emptyOutput{}, nil
	})
}

package apiv1

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerLogStreams, registerLogStreamActions)
}

const tagLogStreams = "Log streaming"

// LogStream is a sink the audit log is shipped to. The token is never
// returned.
type LogStream struct {
	ID   string `format:"uint64" json:"id"`
	Name string `json:"name"`
	// Destination decides the payload shape and the credential header;
	// see docs/ref/log-streaming.md.
	Destination string `json:"destination"`
	URL         string `json:"url"`
	// HasToken reports whether a credential is stored.
	HasToken        bool       `json:"hasToken"`
	Enabled         bool       `json:"enabled"`
	CreatedByUserID string     `format:"uint64"       json:"createdByUserId"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	LastDeliveryAt  *time.Time `json:"lastDeliveryAt"`
	// LastDeliveryStatus is the HTTP status of the newest batch, or the
	// error when none came back; empty before the first.
	LastDeliveryStatus string `json:"lastDeliveryStatus"`
	// Delivered and Dropped count entries over the stream's life.
	Delivered uint64 `json:"delivered"`
	Dropped   uint64 `json:"dropped"`
}

// LogStreamRequestBody creates or replaces a log stream.
type LogStreamRequestBody struct {
	Name        string `json:"name"                                   maxLength:"100"    minLength:"1"`
	Destination string `enum:"http,splunk,elastic,datadog,axiom,loki" json:"destination"`
	URL         string `json:"url"`
	// Token is the sink's credential: a bearer token, a Splunk HEC token,
	// an Elastic API key, a Datadog API key. On update, empty keeps the
	// stored one.
	Token string `json:"token,omitempty" maxLength:"4096"`
	// Enabled defaults to true.
	Enabled *bool `json:"enabled,omitempty"`
}

type (
	logStreamIDInput struct {
		ID string `format:"uint64" path:"id"`
	}
	logStreamBodyInput struct {
		Body LogStreamRequestBody
	}
	logStreamUpdateInput struct {
		ID   string `format:"uint64" path:"id"`
		Body LogStreamRequestBody
	}
	logStreamOutput struct {
		Body struct {
			LogStream LogStream `json:"logStream"`
		}
	}
	listLogStreamsOutput struct {
		Body struct {
			LogStreams []LogStream `json:"logStreams" nullable:"false"`
		}
	}
	logStreamTestOutput struct {
		Body struct {
			// Delivered reports whether the sink answered 2xx.
			Delivered bool   `json:"delivered"`
			Status    string `doc:"HTTP status or the error text." json:"status"`
		}
	}
)

func parseLogStreamID(s string) (types.LogStreamID, error) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, huma.Error400BadRequest("invalid log stream id", err)
	}

	return types.LogStreamID(id), nil
}

func logStreamFromBody(body LogStreamRequestBody) types.LogStream {
	l := types.LogStream{
		Name:        body.Name,
		Destination: types.LogStreamDestination(body.Destination),
		URL:         body.URL,
		Token:       body.Token,
		Enabled:     true,
	}

	if body.Enabled != nil {
		l.Enabled = *body.Enabled
	}

	return l
}

func logStreamFrom(l types.LogStream) LogStream {
	return LogStream{
		ID:                 formatID(uint64(l.ID)),
		Name:               l.Name,
		Destination:        string(l.Destination),
		URL:                l.URL,
		HasToken:           l.Token != "",
		Enabled:            l.Enabled,
		CreatedByUserID:    formatID(uint64(l.CreatedBy)),
		CreatedAt:          l.CreatedAt,
		UpdatedAt:          l.UpdatedAt,
		LastDeliveryAt:     l.LastDeliveryAt,
		LastDeliveryStatus: l.LastDeliveryStatus,
		Delivered:          l.Delivered,
		Dropped:            l.Dropped,
	}
}

func registerLogStreams(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "listLogStreams",
		Method:      http.MethodGet,
		Path:        "/api/v1/log-stream",
		Summary:     "List log streams",
		Description: "Sinks the audit log is shipped to, in batches shaped for each destination. " +
			"Tokens are not listed.",
		Tags:     []string{tagLogStreams},
		Security: bearerAuth,
	}, scope.LogsConfigurationRead), func(_ context.Context, _ *struct{}) (*listLogStreamsOutput, error) {
		streams, err := b.State.ListLogStreams()
		if err != nil {
			return nil, mapError("listing log streams", err)
		}

		out := &listLogStreamsOutput{}
		out.Body.LogStreams = make([]LogStream, 0, len(streams))

		for _, l := range streams {
			out.Body.LogStreams = append(out.Body.LogStreams, logStreamFrom(l))
		}

		return out, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "getLogStream",
		Method:      http.MethodGet,
		Path:        "/api/v1/log-stream/{id}",
		Summary:     "Get log stream",
		Tags:        []string{tagLogStreams},
		Security:    bearerAuth,
	}, scope.LogsConfigurationRead), func(_ context.Context, in *logStreamIDInput) (*logStreamOutput, error) {
		id, err := parseLogStreamID(in.ID)
		if err != nil {
			return nil, err
		}

		l, err := b.State.GetLogStream(id)
		if err != nil {
			return nil, mapError("getting log stream", err)
		}

		out := &logStreamOutput{}
		out.Body.LogStream = logStreamFrom(l)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "createLogStream",
		Method:      http.MethodPost,
		Path:        "/api/v1/log-stream",
		Summary:     "Create log stream",
		Tags:        []string{tagLogStreams},
		Security:    bearerAuth,
	}, scope.LogsConfiguration), "logstream.create", "logstream", ""), func(
		ctx context.Context, in *logStreamBodyInput,
	) (*logStreamOutput, error) {
		l := logStreamFromBody(in.Body)
		l.CreatedBy = caller(ctx).UserID

		created, err := b.State.CreateLogStream(l)
		if err != nil {
			return nil, mapError("creating log stream", err)
		}

		audit.Target(ctx, "", formatID(uint64(created.ID)), created.Name)
		audit.Detail(ctx, "destination", string(created.Destination))
		audit.Detail(ctx, "host", created.Host())

		out := &logStreamOutput{}
		out.Body.LogStream = logStreamFrom(created)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "updateLogStream",
		Method:      http.MethodPut,
		Path:        "/api/v1/log-stream/{id}",
		Summary:     "Replace log stream",
		Description: "An empty token keeps the stored one.",
		Tags:        []string{tagLogStreams},
		Security:    bearerAuth,
	}, scope.LogsConfiguration), "logstream.update", "logstream", "id"), func(
		ctx context.Context, in *logStreamUpdateInput,
	) (*logStreamOutput, error) {
		id, err := parseLogStreamID(in.ID)
		if err != nil {
			return nil, err
		}

		l := logStreamFromBody(in.Body)
		l.ID = id

		updated, err := b.State.UpdateLogStream(l)
		if err != nil {
			return nil, mapError("updating log stream", err)
		}

		audit.Target(ctx, "", "", updated.Name)
		audit.Detail(ctx, "destination", string(updated.Destination))
		audit.Detail(ctx, "host", updated.Host())
		audit.Detail(ctx, "enabled", updated.Enabled)

		out := &logStreamOutput{}
		out.Body.LogStream = logStreamFrom(updated)

		return out, nil
	})
}

// registerLogStreamActions adds delete and test.
func registerLogStreamActions(api huma.API, b Backend) {
	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "deleteLogStream",
		Method:      http.MethodDelete,
		Path:        "/api/v1/log-stream/{id}",
		Summary:     "Delete log stream",
		Tags:        []string{tagLogStreams},
		Security:    bearerAuth,
	}, scope.LogsConfiguration), "logstream.delete", "logstream", "id"), func(
		ctx context.Context, in *logStreamIDInput,
	) (*emptyOutput, error) {
		id, err := parseLogStreamID(in.ID)
		if err != nil {
			return nil, err
		}

		l, err := b.State.GetLogStream(id)
		if err != nil {
			return nil, mapError("deleting log stream", err)
		}

		err = b.State.DeleteLogStream(id)
		if err != nil {
			return nil, mapError("deleting log stream", err)
		}

		audit.Target(ctx, "", "", l.Name)

		return &emptyOutput{}, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "testLogStream",
		Method:      http.MethodPost,
		Path:        "/api/v1/log-stream/{id}/test",
		Summary:     "Test log stream",
		Description: "Ships a test entry now and reports the sink's answer.",
		Tags:        []string{tagLogStreams},
		Security:    bearerAuth,
	}, scope.LogsConfiguration), "logstream.test", "logstream", "id"), func(
		ctx context.Context, in *logStreamIDInput,
	) (*logStreamTestOutput, error) {
		id, err := parseLogStreamID(in.ID)
		if err != nil {
			return nil, err
		}

		err = b.State.TestLogStream(ctx, id)

		l, getErr := b.State.GetLogStream(id)
		if getErr != nil {
			return nil, mapError("testing log stream", getErr)
		}

		audit.Target(ctx, "", "", l.Name)
		audit.Detail(ctx, "status", l.LastDeliveryStatus)

		out := &logStreamTestOutput{}
		out.Body.Delivered = err == nil
		out.Body.Status = l.LastDeliveryStatus

		return out, nil
	})
}

package apiv2

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/api/principal"
	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerLogging)
}

const (
	// logTypeConfiguration is Tailscale's name for the audit log, the one
	// slopscale streams. logTypeNetwork is the flow log, which slopscale
	// does not collect.
	logTypeConfiguration = "configuration"
	logTypeNetwork       = "network"

	// v2LogStreamName marks the one stream this API owns. Tailscale has a
	// single stream per log type and slopscale has many, so the name is
	// what tells them apart: GET, PUT and DELETE all address this one, and
	// streams made in the console are left alone.
	v2LogStreamName = "tailscale-api:configuration"
)

// LogstreamConfiguration is Tailscale's log stream shape. The token is
// write-only, as it is in slopscale's own API.
type LogstreamConfiguration struct {
	LogType         string `json:"logType"`
	DestinationType string `json:"destinationType"`
	URL             string `json:"url"`
}

// SetLogstreamConfigurationRequest is the PUT body. Only the fields
// slopscale acts on are declared; the rest of Tailscale's shape (user,
// uploadPeriodMinutes, compressionFormat and the s3/gcs group) is declared
// so a request that sets one is refused by name rather than silently
// dropped, which would leave an operator believing logs were compressed,
// batched or archived when they were not.
type SetLogstreamConfigurationRequest struct {
	DestinationType     string `json:"destinationType"`
	URL                 string `json:"url"`
	Token               string `json:"token,omitempty"               maxLength:"4096"`
	User                string `json:"user,omitempty"`
	UploadPeriodMinutes int    `json:"uploadPeriodMinutes,omitempty"`
	CompressionFormat   string `json:"compressionFormat,omitempty"`
	S3Bucket            string `json:"s3Bucket,omitempty"`
	GCSBucket           string `json:"gcsBucket,omitempty"`
}

type (
	logstreamInput struct {
		Tailnet string `path:"tailnet"`
		LogType string `doc:"\"configuration\"; slopscale collects no network flow logs." path:"logType"`
	}
	setLogstreamInput struct {
		Tailnet string `path:"tailnet"`
		LogType string `path:"logType"`
		Body    SetLogstreamConfigurationRequest
	}
	logstreamOutput struct {
		Body LogstreamConfiguration
	}
)

func registerLogging(api huma.API, b Backend) {
	loggingTags := []string{"Logging", tagTailscaleCompat}

	huma.Register(api, principal.RequireScope(huma.Operation{
		OperationID: "getLogstreamConfiguration",
		Method:      http.MethodGet,
		Path:        "/api/v2/tailnet/{tailnet}/logging/{logType}/stream",
		Summary:     "Get the log stream configuration",
		Description: "The audit log stream this API owns. Streams created in the console are separate " +
			"and are not returned here.",
		Tags:     loggingTags,
		Security: security,
		Errors:   []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.LogsConfigurationRead), func(_ context.Context, in *logstreamInput) (*logstreamOutput, error) {
		err := requireLogstreamTarget(in.Tailnet, in.LogType)
		if err != nil {
			return nil, err
		}

		stream, err := v2LogStream(b)
		if err != nil {
			return nil, err
		}

		return &logstreamOutput{Body: logstreamFrom(stream)}, nil
	})

	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID:   "setLogstreamConfiguration",
		Method:        http.MethodPut,
		Path:          "/api/v2/tailnet/{tailnet}/logging/{logType}/stream",
		Summary:       "Set the log stream configuration",
		Description:   "Creates or replaces the audit log stream this API owns.",
		Tags:          loggingTags,
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors: []int{
			http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
			http.StatusNotFound, http.StatusInternalServerError,
		},
	}, scope.LogsConfiguration), "logstream.update", "logstream", ""), func(
		ctx context.Context, in *setLogstreamInput,
	) (*logstreamOutput, error) {
		return handleSetLogstream(ctx, b, in)
	})

	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID:   "deleteLogstreamConfiguration",
		Method:        http.MethodDelete,
		Path:          "/api/v2/tailnet/{tailnet}/logging/{logType}/stream",
		Summary:       "Delete the log stream configuration",
		Tags:          loggingTags,
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden,
			http.StatusNotFound, http.StatusInternalServerError,
		},
	}, scope.LogsConfiguration), "logstream.delete", "logstream", ""), func(
		ctx context.Context, in *logstreamInput,
	) (*emptyOutput, error) {
		err := requireLogstreamTarget(in.Tailnet, in.LogType)
		if err != nil {
			return nil, err
		}

		stream, err := v2LogStream(b)
		if err != nil {
			return nil, err
		}

		audit.Target(ctx, "logstream", stream.ID.String(), stream.Name)

		err = b.State.DeleteLogStream(stream.ID)
		if err != nil {
			return nil, mapLogStreamError("deleting log stream", err)
		}

		return &emptyOutput{}, nil
	})
}

func handleSetLogstream(ctx context.Context, b Backend, in *setLogstreamInput) (*logstreamOutput, error) {
	err := requireLogstreamTarget(in.Tailnet, in.LogType)
	if err != nil {
		return nil, err
	}

	err = rejectUnsupportedLogstreamFields(in.Body)
	if err != nil {
		return nil, err
	}

	destination, err := logStreamDestination(in.Body.DestinationType)
	if err != nil {
		return nil, err
	}

	stream := types.LogStream{
		Name:        v2LogStreamName,
		Destination: destination,
		URL:         in.Body.URL,
		Token:       in.Body.Token,
		Enabled:     true,
		CreatedBy:   caller(ctx).UserID,
	}

	audit.Detail(ctx, "destination", string(destination))

	existing, err := findV2LogStream(b)
	if err != nil {
		return nil, err
	}

	var written types.LogStream

	if existing == nil {
		written, err = b.State.CreateLogStream(stream)
	} else {
		stream.ID = existing.ID
		stream.CreatedBy = existing.CreatedBy
		written, err = b.State.UpdateLogStream(stream)
	}

	if err != nil {
		return nil, mapLogStreamError("saving log stream", err)
	}

	audit.Target(ctx, "logstream", written.ID.String(), written.Name)
	audit.Detail(ctx, "host", written.Host())

	return &logstreamOutput{Body: logstreamFrom(written)}, nil
}

// requireLogstreamTarget checks the tailnet and the log type. Network flow
// logs are a client-side feature slopscale does not collect, so that log
// type is a 404 naming the reason rather than an empty configuration.
func requireLogstreamTarget(tailnet, logType string) error {
	err := requireDefaultTailnet(tailnet)
	if err != nil {
		return err
	}

	switch logType {
	case logTypeConfiguration:
		return nil
	case logTypeNetwork:
		return huma.Error404NotFound("network flow logs are not available on slopscale")
	}

	return huma.Error404NotFound("unknown log type " + logType)
}

// v2LogStream returns the stream this API owns, or a 404.
func v2LogStream(b Backend) (types.LogStream, error) {
	stream, err := findV2LogStream(b)
	if err != nil {
		return types.LogStream{}, err
	}

	if stream == nil {
		return types.LogStream{}, huma.Error404NotFound("no log stream configuration is set")
	}

	return *stream, nil
}

// findV2LogStream returns the first enabled stream this API owns, or nil.
func findV2LogStream(b Backend) (*types.LogStream, error) {
	streams, err := b.State.ListLogStreams()
	if err != nil {
		return nil, internalError("listing log streams", err)
	}

	for i := range streams {
		if streams[i].Name == v2LogStreamName && streams[i].Enabled {
			return &streams[i], nil
		}
	}

	return nil, nil //nolint:nilnil // no stream is not an error; the callers decide what to answer
}

// logStreamDestination translates Tailscale's destinationType. Slopscale
// encodes for the destinations in [types.LogStreamDestinations]; Panther
// and Cribl take the plain "http" JSON post, and the archive destinations
// (s3, gcs) have no equivalent.
func logStreamDestination(name string) (types.LogStreamDestination, error) {
	destination := types.LogStreamDestination(strings.ToLower(strings.TrimSpace(name)))
	if slices.Contains(types.LogStreamDestinations, destination) {
		return destination, nil
	}

	supported := make([]string, 0, len(types.LogStreamDestinations))
	for _, d := range types.LogStreamDestinations {
		supported = append(supported, string(d))
	}

	return "", huma.Error400BadRequest(
		"unsupported destinationType " + name + "; slopscale streams to " + strings.Join(supported, ", "),
	)
}

// rejectUnsupportedLogstreamFields refuses a request that asks for
// something slopscale does not do. Accepting these silently would leave an
// operator believing the log was batched on their period, compressed, or
// archived to object storage.
func rejectUnsupportedLogstreamFields(body SetLogstreamConfigurationRequest) error {
	unsupported := ""

	switch {
	case body.User != "":
		unsupported = "user; slopscale authenticates to the sink with the token alone"
	case body.UploadPeriodMinutes != 0:
		unsupported = "uploadPeriodMinutes; slopscale ships each batch as it fills"
	case body.CompressionFormat != "" && body.CompressionFormat != "none":
		unsupported = "compressionFormat; slopscale posts uncompressed"
	case body.S3Bucket != "" || body.GCSBucket != "":
		unsupported = "the object storage destinations; slopscale streams over HTTP only"
	}

	if unsupported == "" {
		return nil
	}

	return huma.Error400BadRequest("unsupported log stream field " + unsupported)
}

func logstreamFrom(l types.LogStream) LogstreamConfiguration {
	return LogstreamConfiguration{
		LogType:         logTypeConfiguration,
		DestinationType: string(l.Destination),
		URL:             l.URL,
	}
}

func mapLogStreamError(msg string, err error) error {
	switch {
	case errors.Is(err, types.ErrLogStreamNotFound):
		return huma.Error404NotFound("no log stream configuration is set")
	case errors.Is(err, types.ErrLogStreamNameInvalid),
		errors.Is(err, types.ErrLogStreamURLInvalid),
		errors.Is(err, types.ErrLogStreamDestinationUnknown),
		errors.Is(err, types.ErrLogStreamTokenRequired),
		errors.Is(err, types.ErrLogStreamTokenLong):
		return huma.Error400BadRequest(msg, err)
	}

	return mapError(msg, err)
}

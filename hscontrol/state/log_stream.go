package state

import (
	"context"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/rs/zerolog/log"
)

// loadLogStreams reads the streams into the streamer. Callers that
// change streams hold logStreamMu across the write and this reload, so
// the streamer never runs a list older than the last write.
func (s *State) loadLogStreams() error {
	streams, err := s.db.ListLogStreams()
	if err != nil {
		return err
	}

	s.logStreams.Reload(streams)

	return nil
}

// ListLogStreams returns every log stream, tokens included; the API
// layer decides what to show.
func (s *State) ListLogStreams() ([]types.LogStream, error) {
	return s.db.ListLogStreams()
}

// GetLogStream returns one stream.
func (s *State) GetLogStream(id types.LogStreamID) (types.LogStream, error) {
	return s.db.GetLogStream(id)
}

// CreateLogStream stores a stream and starts shipping to it.
func (s *State) CreateLogStream(l types.LogStream) (types.LogStream, error) {
	l = trimLogStream(l)

	err := types.ValidateLogStream(l)
	if err != nil {
		return types.LogStream{}, err
	}

	s.logStreamMu.Lock()
	defer s.logStreamMu.Unlock()

	created, err := s.db.CreateLogStream(l)
	if err != nil {
		return types.LogStream{}, err
	}

	return created, s.loadLogStreams()
}

// UpdateLogStream replaces the name, destination, URL and enabled flag
// of a stream, and its token when one is given; an empty token keeps
// the stored one.
func (s *State) UpdateLogStream(l types.LogStream) (types.LogStream, error) {
	l = trimLogStream(l)

	s.logStreamMu.Lock()
	defer s.logStreamMu.Unlock()

	if l.Token == "" {
		// Validation checks the token the stream will run with.
		current, err := s.db.GetLogStream(l.ID)
		if err != nil {
			return types.LogStream{}, err
		}

		l.Token = current.Token
	}

	err := types.ValidateLogStream(l)
	if err != nil {
		return types.LogStream{}, err
	}

	updated, err := s.db.UpdateLogStream(l)
	if err != nil {
		return types.LogStream{}, err
	}

	return updated, s.loadLogStreams()
}

// DeleteLogStream removes a stream and stops shipping to it.
func (s *State) DeleteLogStream(id types.LogStreamID) error {
	s.logStreamMu.Lock()
	defer s.logStreamMu.Unlock()

	err := s.db.DeleteLogStream(id)
	if err != nil {
		return err
	}

	return s.loadLogStreams()
}

// TestLogStream ships a test entry to the stream now and reports how it
// went; the outcome is recorded like any batch.
func (s *State) TestLogStream(ctx context.Context, id types.LogStreamID) error {
	l, err := s.db.GetLogStream(id)
	if err != nil {
		return err
	}

	err = s.logStreams.Test(ctx, l)

	s.logStreamMu.Lock()
	defer s.logStreamMu.Unlock()

	reloadErr := s.loadLogStreams()
	if reloadErr != nil {
		log.Error().Err(reloadErr).Msg("reloading log streams after test")
	}

	return err
}

func trimLogStream(l types.LogStream) types.LogStream {
	l.Name = strings.TrimSpace(l.Name)
	l.URL = strings.TrimSpace(l.URL)
	l.Token = strings.TrimSpace(l.Token)

	return l
}
